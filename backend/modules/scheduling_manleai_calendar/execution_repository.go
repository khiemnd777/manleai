package scheduling_manleai_calendar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
)

const internalAvailabilityQuoteTTL = 2 * time.Minute

type executionFence struct {
	Timezone         string
	Authority        string
	AuthorityVersion int64
	ConfigVersion    int64
	ActivatedVersion int64
}

type quotedStaffOnlySlot struct {
	QuoteID             string
	RequestFingerprint  string
	ExpiresAt           time.Time
	ConsumedAt          sql.NullTime
	ConsumedAttemptID   sql.NullString
	AuthorityVersion    int64
	ConfigVersion       int64
	OperationType       string
	PartySize           int
	SlotID              string
	SlotFingerprint     string
	ServiceID           string
	StaffID             string
	StaffSelectionMode  string
	DurationMinutes     int
	StartTime           time.Time
	EndTime             time.Time
	BufferBeforeMinutes int
	BufferAfterMinutes  int
	OccupiedStartTime   time.Time
	OccupiedEndTime     time.Time
}

func (r *Repository) CheckStaffOnlyAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest, now time.Time) (*booking.AvailabilityResult, error) {
	normalized, err := normalizeStaffOnlyAvailabilityRequest(req)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockCalendarExecutionTx(ctx, tx, salonID); err != nil {
		return nil, err
	}
	fence, err := lockExecutionFence(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if fence.Authority != booking.SchedulingAuthorityManleAICalendar || fence.ConfigVersion < 1 || fence.ActivatedVersion != fence.ConfigVersion {
		return nil, booking.ErrSchedulingAuthorityNotReady
	}
	aggregate, err := loadAggregate(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	windowStart, windowEnd, err := localDateWindow(normalized.PreferredDate, fence.Timezone)
	if err != nil {
		return nil, err
	}
	conflicts, unresolved, err := loadStaffConflicts(ctx, tx, salonID, windowStart, windowEnd)
	if err != nil {
		return nil, err
	}
	planned, err := planStaffOnlyAvailability(AvailabilitySnapshot{
		Aggregate: aggregate, Conflicts: conflicts, UnresolvedExternalConflict: unresolved,
	}, normalized, now)
	if err != nil {
		return nil, err
	}
	result := availabilityResultFromPlan(aggregate, normalized, planned)
	if len(planned) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}

	requestFingerprint := internalAvailabilityRequestFingerprint(fence.AuthorityVersion, fence.ConfigVersion, normalized)
	quoteID := uuid.NewString()
	expiresAt := now.Add(internalAvailabilityQuoteTTL)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id, salon_id, provider, provider_location_id, provider_snapshot_generation,
			request_fingerprint, expires_at, created_at,
			scheduling_authority, authority_provider, authority_location_id, authority_snapshot_generation,
			scheduling_authority_version, authority_config_version, operation_type, party_size
		) VALUES (
			$1,$2,NULL,NULL,NULL,$3,$4,$5,
			'manleai_calendar',NULL,NULL,NULL,$6,$7,'book',1
		)
	`, quoteID, salonID, requestFingerprint, expiresAt, now, fence.AuthorityVersion, fence.ConfigVersion); err != nil {
		return nil, classifyExecutionWriteError(err)
	}
	for index := range planned {
		slot := &planned[index]
		slot.Fingerprint = internalAvailabilitySlotFingerprint(requestFingerprint, normalized.StaffSelectionMode, *slot)
		slotID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO availability_quote_slots (id, salon_id, quote_id, slot_fingerprint, start_time, end_time, segments, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,'[]'::jsonb,$7)
		`, slotID, salonID, quoteID, slot.Fingerprint, slot.StartTime, slot.EndTime, now); err != nil {
			return nil, classifyExecutionWriteError(err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO availability_quote_slot_segments (
				id, salon_id, quote_slot_id, service_id, staff_id, staff_selection_mode,
				duration_minutes, sort_order, scheduled_start_time, scheduled_end_time,
				buffer_before_minutes, buffer_after_minutes, occupied_start_time, occupied_end_time, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,$11,$12,$13,$14)
		`, uuid.NewString(), salonID, slotID, slot.ServiceID, slot.StaffID, normalized.StaffSelectionMode,
			int(slot.EndTime.Sub(slot.StartTime)/time.Minute), slot.StartTime, slot.EndTime,
			slot.BufferBeforeMinutes, slot.BufferAfterMinutes, slot.OccupiedStartTime, slot.OccupiedEndTime, now); err != nil {
			return nil, classifyExecutionWriteError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, classifyExecutionWriteError(err)
	}
	result.QuoteID = quoteID
	result.RequestFingerprint = requestFingerprint
	result.ExpiresAt = &expiresAt
	result.Slots = availabilitySlotsFromPlan(normalized, planned)
	return result, nil
}

func (r *Repository) CreateStaffOnlyAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalCreateRequest, now time.Time) (*InternalCreateResult, bool, error) {
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := lockCalendarExecutionTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if replay, found, err := replayInternalCreateTx(ctx, tx, salonID, ownerUserID, req); err != nil {
		return nil, false, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return replay, true, nil
	}

	fence, err := lockExecutionFence(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, false, err
	}
	quote, err := lockQuotedStaffOnlySlot(ctx, tx, salonID, req.AvailabilityQuoteID, req.SlotFingerprint)
	if err != nil {
		return nil, false, err
	}
	if len(req.Segments) != 1 {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	requestedSegment := req.Segments[0]
	if fence.Authority != booking.SchedulingAuthorityManleAICalendar || fence.AuthorityVersion != quote.AuthorityVersion ||
		fence.ConfigVersion != quote.ConfigVersion || fence.ActivatedVersion != fence.ConfigVersion ||
		quote.OperationType != booking.BookingActionBook || quote.PartySize != 1 || !quote.ExpiresAt.After(now) ||
		quote.ConsumedAt.Valid || quote.ConsumedAttemptID.Valid || req.RequestedTimezone != fence.Timezone ||
		quote.ServiceID != requestedSegment.ServiceID || quote.StaffID != requestedSegment.StaffID || quote.StaffSelectionMode != requestedSegment.StaffSelectionMode ||
		!quote.StartTime.Equal(req.RequestedStartTime) || !quote.EndTime.Equal(req.RequestedEndTime) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	location, locationErr := time.LoadLocation(fence.Timezone)
	if locationErr != nil {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	quotedRequestStaffID := quote.StaffID
	if quote.StaffSelectionMode == booking.StaffSelectionAnyone {
		quotedRequestStaffID = ""
	}
	quotedDate := quote.StartTime.In(location).Format("2006-01-02")
	expectedRequestFingerprint := internalAvailabilityRequestFingerprint(quote.AuthorityVersion, quote.ConfigVersion, normalizedAvailabilityRequest{
		ServiceID: quote.ServiceID, StaffID: quotedRequestStaffID, StaffSelectionMode: quote.StaffSelectionMode, PreferredDate: quotedDate,
	})
	quotedPlan := InternalAvailabilitySlot{
		ServiceID: quote.ServiceID, StaffID: quote.StaffID, StartTime: quote.StartTime, EndTime: quote.EndTime,
		OccupiedStartTime: quote.OccupiedStartTime, OccupiedEndTime: quote.OccupiedEndTime,
	}
	if quote.RequestFingerprint != expectedRequestFingerprint ||
		quote.SlotFingerprint != internalAvailabilitySlotFingerprint(expectedRequestFingerprint, quote.StaffSelectionMode, quotedPlan) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	var resourceEvidence int
	if err := tx.QueryRowContext(ctx, `
		SELECT count(*)
		FROM availability_quote_slot_resource_allocations allocation
		JOIN availability_quote_slot_segments segment ON segment.id = allocation.quote_slot_segment_id
		WHERE segment.quote_slot_id = $1
	`, quote.SlotID).Scan(&resourceEvidence); err != nil {
		return nil, false, err
	}
	if resourceEvidence != 0 {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	if err := lockCanonicalStaffOnlyRows(ctx, tx, salonID, quote.ServiceID, quote.StaffID); err != nil {
		return nil, false, err
	}
	aggregate, err := loadAggregate(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, false, err
	}
	localStart := req.RequestedStartTime.In(location)
	preferredDate := localStart.Format("2006-01-02")
	windowStart, windowEnd, err := localDateWindow(preferredDate, fence.Timezone)
	if err != nil {
		return nil, false, err
	}
	conflicts, unresolved, err := loadStaffConflicts(ctx, tx, salonID, windowStart, windowEnd)
	if err != nil {
		return nil, false, err
	}
	planned, err := planStaffOnlyAvailability(AvailabilitySnapshot{
		Aggregate: aggregate, Conflicts: conflicts, UnresolvedExternalConflict: unresolved,
	}, normalizedAvailabilityRequest{
		ServiceID: quote.ServiceID, StaffID: quote.StaffID, StaffSelectionMode: booking.StaffSelectionSpecific,
		PreferredDate: preferredDate, Limit: -1,
	}, now)
	if err != nil {
		if errors.Is(err, booking.ErrValidation) || errors.Is(err, booking.ErrSchedulingAuthorityNotReady) || errors.Is(err, ErrAvailabilityConflictState) {
			return nil, false, booking.ErrAvailabilityQuoteStale
		}
		return nil, false, err
	}
	if !containsExactQuotedSlot(planned, quote) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}

	attemptID := uuid.NewString()
	appointmentID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			id, salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			target_pos_booking_version, pos_idempotency_key, operation_key, request_fingerprint,
			availability_quote_id, availability_slot_fingerprint, operation_type, target_appointment_id,
			processing_token, processing_lease_expires_at, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, customer_email, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time, notes,
			provider_location_id, provider_snapshot_generation,
			scheduling_authority, authority_provider, authority_appointment_id, authority_appointment_version,
			target_authority_appointment_version, authority_idempotency_key, authority_location_id, authority_snapshot_generation,
			scheduling_authority_version, authority_config_version, party_size, created_at, updated_at
		) VALUES (
			$1,$2,$3,'confirmed',NULL,NULL,NULL,NULL,NULL,$4,$5,
			$6,$7,'book',NULL,NULL,NULL,'not_started','none','not_required',
			$8,$9,NULLIF($10,''),$11,$12,$13,$14,$15,NULLIF($16,''),
			NULL,NULL,'manleai_calendar',NULL,$17,1,NULL,$4,NULL,NULL,
			$18,$19,1,$20,$20
		)
	`, attemptID, salonID, req.Source, req.OperationKey, req.RequestFingerprint,
		req.AvailabilityQuoteID, req.SlotFingerprint, req.CustomerName, req.CustomerPhone, req.CustomerEmail,
		quote.ServiceID, quote.StaffID, quote.StaffSelectionMode, quote.StartTime, quote.EndTime, req.Notes,
		appointmentID, quote.AuthorityVersion, quote.ConfigVersion, now); err != nil {
		return nil, false, fmt.Errorf("insert internal booking attempt: %w", classifyExecutionWriteError(err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempt_segments (
			id, salon_id, booking_attempt_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, pos_service_version, pos_staff_id, name, duration_minutes, sort_order,
			scheduling_authority, authority_provider, authority_service_id, authority_service_version, authority_staff_id,
			scheduled_start_time, scheduled_end_time, buffer_before_minutes, buffer_after_minutes,
			occupied_start_time, occupied_end_time, created_at
		) VALUES (
			$1,$2,$3,$4::uuid,$5::uuid,$6,NULL,NULL,NULL,$7,$8,1,
			'manleai_calendar',NULL,$4::uuid::text,NULL,$5::uuid::text,$9,$10,$11,$12,$13,$14,$15
		)
	`, uuid.NewString(), salonID, attemptID, quote.ServiceID, quote.StaffID, quote.StaffSelectionMode,
		serviceName(aggregate, quote.ServiceID), quote.DurationMinutes, quote.StartTime, quote.EndTime,
		quote.BufferBeforeMinutes, quote.BufferAfterMinutes, quote.OccupiedStartTime, quote.OccupiedEndTime, now); err != nil {
		return nil, false, fmt.Errorf("insert internal attempt segment: %w", classifyExecutionWriteError(err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id, salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			pos_customer_id, pos_sync_status, last_pos_synced_at, pos_sync_error,
			status, customer_name, customer_phone, customer_email, service_id, staff_id, staff_selection_mode,
			start_time, end_time, notes, scheduling_authority, authority_provider, authority_appointment_id,
			authority_appointment_version, authority_customer_id, confirmed_at, confirmed_by_user_id, confirmation_source,
			scheduling_authority_version, authority_config_version, party_size, created_at, updated_at
		) VALUES (
			$1::uuid,$2,$3,NULL,NULL,NULL,NULL,NULL,NULL,NULL,
			'confirmed',$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,NULLIF($12,''),
			'manleai_calendar',NULL,$1::uuid::text,1,NULL,$13,NULL,'manleai_calendar',$14,$15,1,$13,$13
		)
	`, appointmentID, salonID, attemptID, req.CustomerName, req.CustomerPhone, req.CustomerEmail,
		quote.ServiceID, quote.StaffID, quote.StaffSelectionMode, quote.StartTime, quote.EndTime, req.Notes,
		now, quote.AuthorityVersion, quote.ConfigVersion); err != nil {
		return nil, false, fmt.Errorf("insert internal appointment: %w", classifyExecutionWriteError(err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointment_services (
			id, salon_id, appointment_id, service_id, staff_id, staff_selection_mode,
			pos_service_id, pos_service_version, pos_staff_id, name, duration_minutes, sort_order, plan_version,
			scheduling_authority, authority_provider, authority_service_id, authority_service_version, authority_staff_id,
			scheduled_start_time, scheduled_end_time, buffer_before_minutes, buffer_after_minutes,
			occupied_start_time, occupied_end_time, created_at
		) VALUES (
			$1,$2,$3,$4::uuid,$5::uuid,$6,NULL,NULL,NULL,$7,$8,1,1,
			'manleai_calendar',NULL,$4::uuid::text,NULL,$5::uuid::text,$9,$10,$11,$12,$13,$14,$15
		)
	`, uuid.NewString(), salonID, appointmentID, quote.ServiceID, quote.StaffID, quote.StaffSelectionMode,
		serviceName(aggregate, quote.ServiceID), quote.DurationMinutes, quote.StartTime, quote.EndTime,
		quote.BufferBeforeMinutes, quote.BufferAfterMinutes, quote.OccupiedStartTime, quote.OccupiedEndTime, now); err != nil {
		return nil, false, fmt.Errorf("insert internal reservation: %w", classifyExecutionWriteError(err))
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE availability_quotes
		SET consumed_at = $1, consumed_by_attempt_id = $2
		WHERE id = $3 AND salon_id = $4 AND scheduling_authority = 'manleai_calendar'
		  AND consumed_at IS NULL AND consumed_by_attempt_id IS NULL AND expires_at > $1
	`, now, attemptID, quote.QuoteID, salonID)
	if err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, false, err
		}
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	eventPayload, _ := json.Marshal(map[string]any{
		"operation_type": "book", "appointment_id": appointmentID, "booking_attempt_id": attemptID,
		"service_id": quote.ServiceID, "staff_id": quote.StaffID,
	})
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_execution_events (
			id, salon_id, booking_attempt_id, appointment_id, event_type,
			scheduling_authority_version, authority_config_version, authority_appointment_version,
			payload, created_at
		) VALUES ($1,$2,$3,$4,'appointment_confirmed',$5,$6,1,$7::jsonb,$8)
	`, uuid.NewString(), salonID, attemptID, appointmentID, quote.AuthorityVersion, quote.ConfigVersion, string(eventPayload), now); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	authoritative, err := loadAuthoritativeInternalResultTx(
		ctx, tx, salonID, ownerUserID, appointmentID, attemptID,
	)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	return authoritative, false, nil
}

func normalizeStaffOnlyAvailabilityRequest(req booking.AvailabilityRequest) (normalizedAvailabilityRequest, error) {
	if strings.TrimSpace(req.TargetAppointmentID) != "" || len(req.Segments) > 1 {
		return normalizedAvailabilityRequest{}, booking.ErrValidation
	}
	serviceID := strings.TrimSpace(req.ServiceID)
	staffID := strings.TrimSpace(req.StaffID)
	staffMode := strings.TrimSpace(req.StaffSelectionMode)
	if len(req.Segments) == 1 {
		segment := req.Segments[0]
		if serviceID != "" && serviceID != strings.TrimSpace(segment.ServiceID) ||
			staffID != "" && staffID != strings.TrimSpace(segment.StaffID) ||
			staffMode != "" && staffMode != strings.TrimSpace(segment.StaffSelectionMode) {
			return normalizedAvailabilityRequest{}, booking.ErrValidation
		}
		serviceID = strings.TrimSpace(segment.ServiceID)
		staffID = strings.TrimSpace(segment.StaffID)
		staffMode = strings.TrimSpace(segment.StaffSelectionMode)
	}
	if staffMode == "" {
		staffMode = booking.StaffSelectionSpecific
	}
	if !validUUID(serviceID) || (staffMode == booking.StaffSelectionSpecific && !validUUID(staffID)) ||
		(staffMode != booking.StaffSelectionSpecific && staffMode != booking.StaffSelectionAnyone) ||
		(staffMode == booking.StaffSelectionAnyone && staffID != "") || strings.TrimSpace(req.PreferredDate) == "" || req.Limit < 0 || req.Limit > 50 {
		return normalizedAvailabilityRequest{}, booking.ErrValidation
	}
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(req.PreferredDate)); err != nil {
		return normalizedAvailabilityRequest{}, booking.ErrValidation
	}
	return normalizedAvailabilityRequest{
		ServiceID: serviceID, StaffID: staffID, StaffSelectionMode: staffMode,
		PreferredDate: strings.TrimSpace(req.PreferredDate), Limit: req.Limit,
	}, nil
}

func lockCalendarExecutionTx(ctx context.Context, tx *sql.Tx, salonID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, reconciliationLockPrefix+salonID)
	return err
}

func lockExecutionFence(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string) (executionFence, error) {
	var fence executionFence
	err := tx.QueryRowContext(ctx, `
		SELECT salon.timezone, settings.scheduling_authority, settings.scheduling_authority_version
		FROM salons salon
		JOIN salon_settings settings ON settings.salon_id = salon.id
		WHERE salon.id = $1
		  AND (public.app_rls_system_salon_allowed(salon.id)
		       OR public.has_active_tenant_membership(salon.id, $2::uuid)
		       OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls'))
		FOR UPDATE OF salon, settings
	`, salonID, ownerUserID).Scan(&fence.Timezone, &fence.Authority, &fence.AuthorityVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return executionFence{}, ErrNotFound
	}
	if err != nil {
		return executionFence{}, err
	}
	var activated sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT version, activated_version
		FROM manleai_calendar_configs
		WHERE salon_id = $1
		FOR UPDATE
	`, salonID).Scan(&fence.ConfigVersion, &activated)
	if errors.Is(err, sql.ErrNoRows) {
		return executionFence{}, booking.ErrSchedulingAuthorityNotReady
	}
	if err != nil {
		return executionFence{}, err
	}
	if activated.Valid {
		fence.ActivatedVersion = activated.Int64
	}
	return fence, nil
}

func localDateWindow(date string, timezone string) (time.Time, time.Time, error) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, time.Time{}, booking.ErrValidation
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return time.Time{}, time.Time{}, booking.ErrValidation
	}
	start, ok := strictLocalMinute(parsed.Year(), parsed.Month(), parsed.Day(), 0, location)
	if !ok {
		return time.Time{}, time.Time{}, booking.ErrValidation
	}
	next := parsed.AddDate(0, 0, 1)
	end, ok := strictLocalMinute(next.Year(), next.Month(), next.Day(), 0, location)
	if !ok || !end.After(start) {
		return time.Time{}, time.Time{}, booking.ErrValidation
	}
	return start, end, nil
}

func loadStaffConflicts(ctx context.Context, tx *sql.Tx, salonID string, windowStart time.Time, windowEnd time.Time) ([]StaffConflict, bool, error) {
	return loadStaffConflictsExcluding(ctx, tx, salonID, windowStart, windowEnd, "")
}

func loadStaffConflictsExcluding(ctx context.Context, tx *sql.Tx, salonID string, windowStart time.Time, windowEnd time.Time, excludedAppointmentID string) ([]StaffConflict, bool, error) {
	var invalidExternalTiming bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM appointments appointment
			WHERE appointment.salon_id = $1
			  AND appointment.scheduling_authority = 'external_provider'
			  AND appointment.status IN ('provider_pending','confirmed','rescheduled','unknown')
			  AND appointment.end_time <= appointment.start_time
		)
	`, salonID).Scan(&invalidExternalTiming); err != nil {
		return nil, false, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT segment.staff_id::text, segment.occupied_start_time, segment.occupied_end_time
		FROM appointment_services segment
		JOIN appointments appointment ON appointment.salon_id = segment.salon_id AND appointment.id = segment.appointment_id
		WHERE segment.salon_id = $1
		  AND segment.scheduling_authority = 'manleai_calendar'
		  AND segment.released_at IS NULL
		  AND ($4 = '' OR appointment.id::text <> $4)
		  AND appointment.status IN ('confirmed','rescheduled')
		  AND segment.occupied_start_time < $3 AND segment.occupied_end_time > $2
		ORDER BY segment.staff_id, segment.occupied_start_time, segment.id
	`, salonID, windowStart, windowEnd, strings.TrimSpace(excludedAppointmentID))
	if err != nil {
		return nil, false, err
	}
	conflicts := []StaffConflict{}
	for rows.Next() {
		var item StaffConflict
		if err := rows.Scan(&item.StaffID, &item.StartsAt, &item.EndsAt); err != nil {
			rows.Close()
			return nil, false, err
		}
		conflicts = append(conflicts, item)
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	externalRows, err := tx.QueryContext(ctx, `
		SELECT appointment.id::text, appointment.start_time, appointment.end_time,
		       COALESCE(array_agg(DISTINCT COALESCE(segment.staff_id, appointment.staff_id)::text)
		           FILTER (WHERE COALESCE(segment.staff_id, appointment.staff_id) IS NOT NULL), ARRAY[]::text[]),
		       bool_or(COALESCE(segment.staff_id, appointment.staff_id) IS NULL)
		FROM appointments appointment
		LEFT JOIN appointment_services segment
		  ON segment.salon_id = appointment.salon_id AND segment.appointment_id = appointment.id
		WHERE appointment.salon_id = $1
		  AND appointment.scheduling_authority = 'external_provider'
		  AND appointment.status IN ('provider_pending','confirmed','rescheduled','unknown')
		  AND appointment.start_time < $3 AND appointment.end_time > $2
		GROUP BY appointment.id, appointment.start_time, appointment.end_time
		ORDER BY appointment.start_time, appointment.id
	`, salonID, windowStart, windowEnd)
	if err != nil {
		return nil, false, err
	}
	defer externalRows.Close()
	unresolved := invalidExternalTiming
	for externalRows.Next() {
		var appointmentID string
		var startsAt, endsAt time.Time
		var staffIDs []string
		var missingStaff bool
		if err := externalRows.Scan(&appointmentID, &startsAt, &endsAt, pq.Array(&staffIDs), &missingStaff); err != nil {
			return nil, false, err
		}
		if appointmentID == "" || !endsAt.After(startsAt) || missingStaff || len(staffIDs) == 0 {
			unresolved = true
			continue
		}
		for _, staffID := range staffIDs {
			if !validUUID(staffID) {
				unresolved = true
				continue
			}
			conflicts = append(conflicts, StaffConflict{StaffID: staffID, StartsAt: startsAt, EndsAt: endsAt})
		}
	}
	return conflicts, unresolved, externalRows.Err()
}

func availabilityResultFromPlan(aggregate *Aggregate, req normalizedAvailabilityRequest, slots []InternalAvailabilitySlot) *booking.AvailabilityResult {
	result := &booking.AvailabilityResult{
		ServiceID: req.ServiceID, StaffID: req.StaffID, StaffSelectionMode: req.StaffSelectionMode,
		PreferredDate: req.PreferredDate, Timezone: aggregate.Timezone, Slots: availabilitySlotsFromPlan(req, slots),
	}
	for _, policy := range aggregate.ServicePolicies {
		if policy.Service.ID == req.ServiceID {
			result.ServiceName = policy.Service.Name
			result.DurationMinutes = policy.Service.DurationMinutes
			break
		}
	}
	if req.StaffSelectionMode == booking.StaffSelectionSpecific {
		for _, profile := range aggregate.StaffProfiles {
			if profile.Staff.ID == req.StaffID {
				result.StaffName = profile.Staff.Name
				break
			}
		}
	}
	return result
}

func availabilitySlotsFromPlan(req normalizedAvailabilityRequest, planned []InternalAvailabilitySlot) []booking.AvailabilitySlot {
	result := make([]booking.AvailabilitySlot, 0, len(planned))
	for _, slot := range planned {
		result = append(result, booking.AvailabilitySlot{
			Fingerprint: slot.Fingerprint, StartTime: slot.StartTime, EndTime: slot.EndTime,
			StaffID: slot.StaffID, StaffName: slot.StaffName, StaffSelectionMode: req.StaffSelectionMode,
			Segments: []booking.AvailabilitySegment{{
				ServiceID: slot.ServiceID, ServiceName: slot.ServiceName, StaffID: slot.StaffID, StaffName: slot.StaffName,
				StaffSelectionMode: req.StaffSelectionMode, DurationMinutes: int(slot.EndTime.Sub(slot.StartTime) / time.Minute),
			}},
		})
	}
	return result
}

func replayInternalCreateTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, req InternalCreateRequest) (*InternalCreateResult, bool, error) {
	var authority, fingerprint, status, attemptID string
	var appointmentID sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT attempt.scheduling_authority, attempt.request_fingerprint, attempt.status, attempt.id::text,
		       appointment.id::text
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id = attempt.salon_id
		LEFT JOIN manleai_calendar_execution_events event
		  ON event.salon_id = attempt.salon_id AND event.booking_attempt_id = attempt.id
		 AND event.event_type = 'appointment_confirmed'
		LEFT JOIN appointments appointment
		  ON appointment.salon_id = event.salon_id AND appointment.id = event.appointment_id
		WHERE attempt.salon_id = $1
		  AND (public.app_rls_system_salon_allowed(salon.id)
		       OR public.has_active_tenant_membership(salon.id, $2::uuid)
		       OR public.app_actor_feature_access($2::uuid, salon.id, 'calls.simulate', 'calls'))
		  AND attempt.operation_key = $3
	`, salonID, ownerUserID, req.OperationKey).Scan(&authority, &fingerprint, &status, &attemptID, &appointmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if authority != booking.SchedulingAuthorityManleAICalendar || fingerprint != req.RequestFingerprint ||
		status != booking.StatusConfirmed || !appointmentID.Valid || appointmentID.String == "" {
		return nil, false, booking.ErrOperationConflict
	}
	result, err := loadAuthoritativeInternalResultTx(
		ctx, tx, salonID, ownerUserID, appointmentID.String, attemptID,
	)
	if err != nil {
		return nil, false, err
	}
	return result, true, nil
}

func lockQuotedStaffOnlySlot(ctx context.Context, tx *sql.Tx, salonID string, quoteID string, slotFingerprint string) (quotedStaffOnlySlot, error) {
	var item quotedStaffOnlySlot
	err := tx.QueryRowContext(ctx, `
		SELECT quote.id::text, quote.request_fingerprint, quote.expires_at, quote.consumed_at,
		       quote.consumed_by_attempt_id::text, quote.scheduling_authority_version, quote.authority_config_version,
		       quote.operation_type, quote.party_size, slot.id::text, slot.slot_fingerprint,
		       segment.service_id::text, segment.staff_id::text, segment.staff_selection_mode,
		       segment.duration_minutes, segment.scheduled_start_time, segment.scheduled_end_time,
		       segment.buffer_before_minutes, segment.buffer_after_minutes,
		       segment.occupied_start_time, segment.occupied_end_time
		FROM availability_quotes quote
		JOIN availability_quote_slots slot
		  ON slot.salon_id = quote.salon_id AND slot.quote_id = quote.id
		JOIN availability_quote_slot_segments segment
		  ON segment.salon_id = slot.salon_id AND segment.quote_slot_id = slot.id AND segment.sort_order = 1
		WHERE quote.id = $1 AND quote.salon_id = $2
		  AND quote.scheduling_authority = 'manleai_calendar'
		  AND slot.slot_fingerprint = $3
		  AND NOT EXISTS (
		      SELECT 1 FROM availability_quote_slot_segments extra
		      WHERE extra.quote_slot_id = slot.id AND extra.id <> segment.id
		  )
		FOR UPDATE OF quote, slot, segment
	`, quoteID, salonID, slotFingerprint).Scan(
		&item.QuoteID, &item.RequestFingerprint, &item.ExpiresAt, &item.ConsumedAt,
		&item.ConsumedAttemptID, &item.AuthorityVersion, &item.ConfigVersion, &item.OperationType, &item.PartySize,
		&item.SlotID, &item.SlotFingerprint, &item.ServiceID, &item.StaffID, &item.StaffSelectionMode,
		&item.DurationMinutes, &item.StartTime, &item.EndTime, &item.BufferBeforeMinutes, &item.BufferAfterMinutes,
		&item.OccupiedStartTime, &item.OccupiedEndTime,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return quotedStaffOnlySlot{}, booking.ErrAvailabilityQuoteStale
	}
	return item, err
}

func lockCanonicalStaffOnlyRows(ctx context.Context, tx *sql.Tx, salonID string, serviceID string, staffID string) error {
	var matched bool
	err := tx.QueryRowContext(ctx, `
		SELECT true
		FROM services service
		JOIN manleai_calendar_service_policies policy
		  ON policy.salon_id = service.salon_id AND policy.service_id = service.id
		JOIN manleai_calendar_service_staff link
		  ON link.salon_id = service.salon_id AND link.service_id = service.id
		JOIN staff ON staff.salon_id = link.salon_id AND staff.id = link.staff_id
		WHERE service.salon_id = $1 AND service.id = $2 AND staff.id = $3
		  AND service.active AND service.ai_bookable AND service.archived_at IS NULL
		  AND staff.active AND staff.ai_bookable AND staff.archived_at IS NULL
		  AND policy.enabled AND policy.capacity_mode = 'staff_only'
		  AND NOT EXISTS (
		      SELECT 1 FROM manleai_calendar_service_resources resource
		      WHERE resource.salon_id = service.salon_id AND resource.service_id = service.id
		  )
		FOR UPDATE OF service, policy, link, staff
	`, salonID, serviceID, staffID).Scan(&matched)
	if errors.Is(err, sql.ErrNoRows) || !matched {
		return booking.ErrAvailabilityQuoteStale
	}
	return err
}

func containsExactQuotedSlot(planned []InternalAvailabilitySlot, quote quotedStaffOnlySlot) bool {
	for _, slot := range planned {
		if slot.ServiceID == quote.ServiceID && slot.StaffID == quote.StaffID &&
			slot.StartTime.Equal(quote.StartTime) && slot.EndTime.Equal(quote.EndTime) &&
			slot.OccupiedStartTime.Equal(quote.OccupiedStartTime) && slot.OccupiedEndTime.Equal(quote.OccupiedEndTime) &&
			slot.BufferBeforeMinutes == quote.BufferBeforeMinutes && slot.BufferAfterMinutes == quote.BufferAfterMinutes {
			return true
		}
	}
	return false
}

func serviceName(aggregate *Aggregate, serviceID string) string {
	for _, policy := range aggregate.ServicePolicies {
		if policy.Service.ID == serviceID {
			return policy.Service.Name
		}
	}
	return ""
}

func hashCalendarValue(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func internalAvailabilityRequestFingerprint(authorityVersion int64, configVersion int64, req normalizedAvailabilityRequest) string {
	return hashCalendarValue(struct {
		AuthorityVersion int64  `json:"authority_version"`
		ConfigVersion    int64  `json:"config_version"`
		ServiceID        string `json:"service_id"`
		StaffID          string `json:"staff_id"`
		StaffMode        string `json:"staff_selection_mode"`
		PreferredDate    string `json:"preferred_date"`
	}{authorityVersion, configVersion, req.ServiceID, req.StaffID, req.StaffSelectionMode, req.PreferredDate})
}

func internalAvailabilitySlotFingerprint(requestFingerprint string, staffMode string, slot InternalAvailabilitySlot) string {
	return hashCalendarValue(struct {
		QuoteRequestFingerprint string `json:"quote_request_fingerprint"`
		ServiceID               string `json:"service_id"`
		StaffID                 string `json:"staff_id"`
		StaffMode               string `json:"staff_selection_mode"`
		StartTime               string `json:"start_time"`
		EndTime                 string `json:"end_time"`
		OccupiedStartTime       string `json:"occupied_start_time"`
		OccupiedEndTime         string `json:"occupied_end_time"`
	}{requestFingerprint, slot.ServiceID, slot.StaffID, staffMode,
		slot.StartTime.Format(time.RFC3339Nano), slot.EndTime.Format(time.RFC3339Nano),
		slot.OccupiedStartTime.Format(time.RFC3339Nano), slot.OccupiedEndTime.Format(time.RFC3339Nano)})
}

func classifyExecutionWriteError(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch string(pqErr.Code) {
		case "23P01":
			return fmt.Errorf("%w: staff reservation conflict", booking.ErrAvailabilityQuoteStale)
		case "23502", "23503", "23514":
			return fmt.Errorf("%w: internal calendar evidence conflict", booking.ErrAvailabilityQuoteStale)
		case "23505":
			if strings.Contains(pqErr.Constraint, "operation_key") {
				return booking.ErrOperationConflict
			}
			return fmt.Errorf("%w: internal calendar uniqueness conflict", booking.ErrAvailabilityQuoteStale)
		}
	}
	return err
}
