package scheduling_manleai_calendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

type lifecycleTarget struct {
	ID                          string
	Status                      string
	AuthorityAppointmentVersion int
	SchedulingAuthorityVersion  int64
	AuthorityConfigVersion      int64
	PartySize                   int
	CustomerName                string
	CustomerPhone               string
	CustomerEmail               string
	StartTime                   time.Time
	EndTime                     time.Time
	Notes                       string
	Segments                    []quotedAggregateSegment
}

func lockLifecycleTarget(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, appointmentID string) (lifecycleTarget, error) {
	var target lifecycleTarget
	err := tx.QueryRowContext(ctx, `
		SELECT appointment.id::text, appointment.status, appointment.authority_appointment_version,
		       appointment.scheduling_authority_version, appointment.authority_config_version,
		       appointment.party_size, appointment.customer_name, appointment.customer_phone,
		       COALESCE(appointment.customer_email,''), appointment.start_time, appointment.end_time,
		       COALESCE(appointment.notes,'')
		FROM appointments appointment
		JOIN salons salon ON salon.id = appointment.salon_id
		WHERE appointment.salon_id = $1 AND salon.owner_user_id = $2
		  AND appointment.id::text = $3
		  AND appointment.scheduling_authority = 'manleai_calendar'
		FOR UPDATE OF appointment
	`, salonID, ownerUserID, strings.TrimSpace(appointmentID)).Scan(
		&target.ID, &target.Status, &target.AuthorityAppointmentVersion,
		&target.SchedulingAuthorityVersion, &target.AuthorityConfigVersion,
		&target.PartySize, &target.CustomerName, &target.CustomerPhone, &target.CustomerEmail,
		&target.StartTime, &target.EndTime, &target.Notes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return lifecycleTarget{}, ErrNotFound
	}
	if err != nil {
		return lifecycleTarget{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT segment.id::text, segment.name, segment.service_id::text, segment.staff_id::text,
		       segment.staff_selection_mode, COALESCE(segment.guest_reference,''),
		       segment.duration_minutes, segment.sort_order, segment.scheduled_start_time,
		       segment.scheduled_end_time, segment.buffer_before_minutes,
		       segment.buffer_after_minutes, segment.occupied_start_time, segment.occupied_end_time
		FROM appointment_services segment
		WHERE segment.salon_id = $1 AND segment.appointment_id = $2
		  AND segment.scheduling_authority = 'manleai_calendar'
		  AND segment.plan_version = $3 AND segment.released_at IS NULL
		ORDER BY segment.sort_order
		FOR UPDATE
	`, salonID, target.ID, target.AuthorityAppointmentVersion)
	if err != nil {
		return lifecycleTarget{}, err
	}
	for rows.Next() {
		var segment quotedAggregateSegment
		if err := rows.Scan(
			&segment.ID, &segment.Name, &segment.ServiceID, &segment.StaffID, &segment.StaffSelectionMode,
			&segment.GuestReference, &segment.DurationMinutes, &segment.SortOrder,
			&segment.StartTime, &segment.EndTime, &segment.BufferBeforeMinutes,
			&segment.BufferAfterMinutes, &segment.OccupiedStartTime, &segment.OccupiedEndTime,
		); err != nil {
			rows.Close()
			return lifecycleTarget{}, err
		}
		target.Segments = append(target.Segments, segment)
	}
	if err := rows.Close(); err != nil {
		return lifecycleTarget{}, err
	}
	if err := rows.Err(); err != nil {
		return lifecycleTarget{}, err
	}
	if len(target.Segments) == 0 {
		return lifecycleTarget{}, booking.ErrOperationConflict
	}
	for index := range target.Segments {
		allocationRows, err := tx.QueryContext(ctx, `
			SELECT allocation.resource_pool_id::text, pool.name, allocation.units_allocated
			FROM manleai_calendar_appointment_resource_allocations allocation
			JOIN manleai_calendar_resource_pools pool
			  ON pool.salon_id = allocation.salon_id AND pool.id = allocation.resource_pool_id
			WHERE allocation.salon_id = $1 AND allocation.appointment_service_id = $2
			  AND allocation.plan_version = $3 AND allocation.released_at IS NULL
			ORDER BY allocation.resource_pool_id
			FOR UPDATE OF allocation, pool
		`, salonID, target.Segments[index].ID, target.AuthorityAppointmentVersion)
		if err != nil {
			return lifecycleTarget{}, err
		}
		for allocationRows.Next() {
			var allocation InternalResourceAllocation
			if err := allocationRows.Scan(&allocation.ResourcePoolID, &allocation.ResourceName, &allocation.UnitsAllocated); err != nil {
				allocationRows.Close()
				return lifecycleTarget{}, err
			}
			target.Segments[index].ResourceAllocations = append(target.Segments[index].ResourceAllocations, allocation)
		}
		if err := allocationRows.Close(); err != nil {
			return lifecycleTarget{}, err
		}
	}
	return target, nil
}

func lifecycleCutoffOpen(now time.Time, appointmentStart time.Time, cutoffMinutes *int) bool {
	if cutoffMinutes == nil {
		return true
	}
	return now.UTC().Before(appointmentStart.UTC().Add(-time.Duration(*cutoffMinutes) * time.Minute))
}

func replacementPreservesTargetShape(request normalizedAvailabilityRequest, target lifecycleTarget) bool {
	if request.PartySize != target.PartySize || len(request.Segments) != len(target.Segments) {
		return false
	}
	for index, segment := range request.Segments {
		original := target.Segments[index]
		if original.SortOrder != index+1 || segment.Quantity != 1 ||
			segment.ServiceID != original.ServiceID ||
			strings.TrimSpace(segment.GuestReference) != strings.TrimSpace(original.GuestReference) {
			return false
		}
	}
	return true
}

func lifecycleAvailabilityRequestFingerprint(authorityVersion int64, configVersion int64, targetID string, targetVersion int, request normalizedAvailabilityRequest) string {
	return hashCalendarValue(struct {
		OperationType    string                          `json:"operation_type"`
		AuthorityVersion int64                           `json:"authority_version"`
		ConfigVersion    int64                           `json:"config_version"`
		TargetID         string                          `json:"target_appointment_id"`
		TargetVersion    int                             `json:"target_authority_appointment_version"`
		PartySize        int                             `json:"party_size"`
		Segments         []normalizedAvailabilitySegment `json:"segments"`
		PreferredDate    string                          `json:"preferred_date"`
	}{
		OperationType: booking.BookingActionReschedule, AuthorityVersion: authorityVersion,
		ConfigVersion: configVersion, TargetID: targetID, TargetVersion: targetVersion,
		PartySize: request.PartySize, Segments: request.Segments, PreferredDate: request.PreferredDate,
	})
}

func (r *Repository) RescheduleAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalLifecycleRequest, now time.Time) (*InternalCreateResult, bool, error) {
	if req.OperationType != scheduling.OperationKindReschedule {
		return nil, false, scheduling.ErrInvalidSchedulingAction
	}
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := lockCalendarExecutionTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if replay, found, err := replayLifecycleTx(ctx, tx, salonID, ownerUserID, req); err != nil {
		return nil, false, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return replay, true, nil
	}
	target, err := lockLifecycleTarget(ctx, tx, salonID, ownerUserID, req.TargetAppointmentID)
	if err != nil {
		return nil, false, err
	}
	if target.Status != booking.StatusConfirmed && target.Status != booking.StatusRescheduled ||
		target.AuthorityAppointmentVersion != req.ExpectedTargetAuthorityAppointmentVersion {
		return nil, false, booking.ErrOperationConflict
	}
	cutoff, err := loadLifecycleCutoff(ctx, tx, salonID, true)
	if err != nil {
		return nil, false, err
	}
	if !lifecycleCutoffOpen(now, target.StartTime, cutoff) {
		return nil, false, booking.ErrSchedulingAuthorityNotReady
	}
	fence, err := lockExecutionFence(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, false, err
	}
	quote, err := lockQuotedAggregateSlot(ctx, tx, salonID, req.AvailabilityQuoteID, req.SlotFingerprint)
	if err != nil {
		return nil, false, err
	}
	if quote.OperationType != booking.BookingActionReschedule ||
		quote.TargetAppointmentID != target.ID ||
		quote.TargetAuthorityAppointmentVersion != target.AuthorityAppointmentVersion ||
		quote.AuthorityVersion != target.SchedulingAuthorityVersion ||
		quote.ConfigVersion != fence.ConfigVersion || fence.ActivatedVersion != fence.ConfigVersion ||
		!quote.ExpiresAt.After(now) || quote.ConsumedAt.Valid || quote.ConsumedAttemptID.Valid ||
		req.ExpectedTargetAuthorityAppointmentVersion != quote.TargetAuthorityAppointmentVersion ||
		req.RequestedTimezone != fence.Timezone || !quoteMatchesLifecycleAction(quote, req) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	requested := quotedRequest(quote, fence.Timezone)
	if !replacementPreservesTargetShape(requested, target) ||
		quote.RequestFingerprint != lifecycleAvailabilityRequestFingerprint(
			quote.AuthorityVersion, quote.ConfigVersion, target.ID, target.AuthorityAppointmentVersion, requested,
		) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	poolIDs := distinctPoolIDs(quote.Segments)
	if _, err := tx.ExecContext(ctx, `SELECT lock_manleai_calendar_resource_pools($1::uuid,$2::uuid[])`, salonID, pq.Array(poolIDs)); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if err := lockAggregateCanonicalRows(ctx, tx, salonID, quote); err != nil {
		return nil, false, err
	}
	aggregate, err := loadAggregate(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, false, err
	}
	location, err := time.LoadLocation(fence.Timezone)
	if err != nil {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	preferredDate := quote.StartTime.In(location).Format("2006-01-02")
	windowStart, windowEnd, err := localDateWindow(preferredDate, fence.Timezone)
	if err != nil {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	conflicts, unresolved, err := loadStaffConflictsExcluding(ctx, tx, salonID, windowStart, windowEnd, target.ID)
	if err != nil {
		return nil, false, err
	}
	resourceConflicts, err := loadResourceConflictsExcluding(ctx, tx, salonID, windowStart, windowEnd, target.ID)
	if err != nil {
		return nil, false, err
	}
	planned, err := planAggregateAvailability(AvailabilitySnapshot{
		Aggregate: aggregate, Conflicts: conflicts, ResourceConflicts: resourceConflicts,
		UnresolvedExternalConflict: unresolved, TargetOriginAuthorized: true,
	}, requested, now)
	if err != nil {
		if errors.Is(err, booking.ErrValidation) || errors.Is(err, booking.ErrSchedulingAuthorityNotReady) || errors.Is(err, ErrAvailabilityConflictState) {
			return nil, false, booking.ErrAvailabilityQuoteStale
		}
		return nil, false, err
	}
	quotedPlan := quotedSlotAsPlan(quote, aggregate)
	if quote.SlotFingerprint != aggregateAvailabilitySlotFingerprint(quote.RequestFingerprint, quotedPlan) ||
		!exactAggregatePlan(planned, quotedPlan) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	return executeLifecycleTx(ctx, tx, salonID, ownerUserID, req, target, quote, now, aggregate)
}

func (r *Repository) CancelAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalLifecycleRequest, now time.Time) (*InternalCreateResult, bool, error) {
	if req.OperationType != scheduling.OperationKindCancel {
		return nil, false, scheduling.ErrInvalidSchedulingAction
	}
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := lockCalendarExecutionTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if replay, found, err := replayLifecycleTx(ctx, tx, salonID, ownerUserID, req); err != nil {
		return nil, false, err
	} else if found {
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return replay, true, nil
	}
	target, err := lockLifecycleTarget(ctx, tx, salonID, ownerUserID, req.TargetAppointmentID)
	if err != nil {
		return nil, false, err
	}
	if target.Status != booking.StatusConfirmed && target.Status != booking.StatusRescheduled ||
		target.AuthorityAppointmentVersion != req.ExpectedTargetAuthorityAppointmentVersion {
		return nil, false, booking.ErrOperationConflict
	}
	cutoff, err := loadLifecycleCutoff(ctx, tx, salonID, false)
	if err != nil {
		return nil, false, err
	}
	if !lifecycleCutoffOpen(now, target.StartTime, cutoff) {
		return nil, false, booking.ErrSchedulingAuthorityNotReady
	}
	return executeLifecycleTx(ctx, tx, salonID, ownerUserID, req, target, quotedAggregateSlot{}, now, nil)
}

func loadLifecycleCutoff(ctx context.Context, tx *sql.Tx, salonID string, reschedule bool) (*int, error) {
	column := "cancellation_cutoff_minutes"
	if reschedule {
		column = "reschedule_cutoff_minutes"
	}
	var value sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT `+column+` FROM manleai_calendar_configs WHERE salon_id = $1 FOR UPDATE`, salonID).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !value.Valid {
		return nil, nil
	}
	result := int(value.Int64)
	return &result, nil
}

func quoteMatchesLifecycleAction(quote quotedAggregateSlot, req InternalLifecycleRequest) bool {
	if quote.PartySize != req.PartySize || len(quote.Segments) != len(req.Segments) ||
		!quote.StartTime.Equal(req.RequestedStartTime) || !quote.EndTime.Equal(req.RequestedEndTime) {
		return false
	}
	for index, quoted := range quote.Segments {
		requested := req.Segments[index]
		if quoted.SortOrder != index+1 || quoted.ServiceID != requested.ServiceID ||
			quoted.StaffID != requested.StaffID || quoted.StaffSelectionMode != requested.StaffSelectionMode ||
			quoted.GuestReference != requested.GuestReference || requested.Quantity != 1 ||
			!quoted.StartTime.Equal(requested.RequestedStartTime) ||
			!quoted.EndTime.Equal(requested.RequestedEndTime) {
			return false
		}
	}
	return true
}

func executeLifecycleTx(
	ctx context.Context,
	tx *sql.Tx,
	salonID string,
	ownerUserID string,
	req InternalLifecycleRequest,
	target lifecycleTarget,
	quote quotedAggregateSlot,
	now time.Time,
	aggregate *Aggregate,
) (*InternalCreateResult, bool, error) {
	newVersion := target.AuthorityAppointmentVersion + 1
	status := booking.StatusCancelled
	eventType := "appointment_cancelled"
	snapshot := target.Segments
	configVersion := target.AuthorityConfigVersion
	rootStart, rootEnd := target.StartTime, target.EndTime
	rootNotes := req.Notes
	if req.OperationType == scheduling.OperationKindReschedule {
		status = booking.StatusRescheduled
		eventType = "appointment_rescheduled"
		snapshot = quote.Segments
		configVersion = quote.ConfigVersion
		rootStart, rootEnd = quote.StartTime, quote.EndTime
	}
	attemptID := uuid.NewString()
	primary := snapshot[0]
	var quoteID any
	var slotFingerprint any
	if req.OperationType == scheduling.OperationKindReschedule {
		quoteID = quote.QuoteID
		slotFingerprint = quote.SlotFingerprint
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO booking_attempts (
			id, salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			target_pos_booking_version, pos_idempotency_key, operation_key, request_fingerprint,
			availability_quote_id, availability_slot_fingerprint, operation_type, target_appointment_id,
			processing_token, processing_lease_expires_at, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, customer_email, service_id, staff_id, staff_selection_mode,
			requested_start_time, requested_end_time, notes, provider_location_id, provider_snapshot_generation,
			scheduling_authority, authority_provider, authority_appointment_id, authority_appointment_version,
			target_authority_appointment_version, authority_idempotency_key, authority_location_id, authority_snapshot_generation,
			scheduling_authority_version, authority_config_version, party_size, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,NULL,NULL,NULL,NULL,NULL,$5,$6,$7,$8,$9,$10,NULL,NULL,'not_started','none','not_required',
			$11,$12,NULLIF($13,''),$14,$15,$16,$17,$18,NULLIF($19,''),NULL,NULL,
			'manleai_calendar',NULL,$10::uuid::text,$20,$21,$5,NULL,NULL,$22,$23,$24,$25,$25
		)
	`, attemptID, salonID, req.Source, status, req.OperationKey, req.RequestFingerprint,
		quoteID, slotFingerprint, string(req.OperationType), target.ID,
		target.CustomerName, target.CustomerPhone, target.CustomerEmail,
		primary.ServiceID, primary.StaffID, primary.StaffSelectionMode,
		rootStart, rootEnd, rootNotes, newVersion, target.AuthorityAppointmentVersion,
		target.SchedulingAuthorityVersion, configVersion, target.PartySize, now); err != nil {
		return nil, false, fmt.Errorf("insert lifecycle booking attempt: %w", classifyExecutionWriteError(err))
	}
	children, err := insertAttemptSnapshot(ctx, tx, salonID, attemptID, snapshot, aggregate, now)
	if err != nil {
		return nil, false, err
	}
	expectedAllocations := 0
	for _, segment := range target.Segments {
		expectedAllocations += len(segment.ResourceAllocations)
	}
	releasedAllocations, err := tx.ExecContext(ctx, `
		UPDATE manleai_calendar_appointment_resource_allocations allocation
		SET released_at = $1
		FROM appointment_services segment
		WHERE allocation.salon_id = $2
		  AND allocation.appointment_service_id = segment.id
		  AND segment.appointment_id = $3
		  AND segment.plan_version = $4
		  AND segment.released_at IS NULL
		  AND allocation.released_at IS NULL
	`, now, salonID, target.ID, target.AuthorityAppointmentVersion)
	if err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if affected, err := releasedAllocations.RowsAffected(); err != nil || int(affected) != expectedAllocations {
		if err != nil {
			return nil, false, err
		}
		return nil, false, booking.ErrOperationConflict
	}
	releasedSegments, err := tx.ExecContext(ctx, `
		UPDATE appointment_services
		SET released_at = $1, released_by_attempt_id = $2
		WHERE salon_id = $3 AND appointment_id = $4
		  AND plan_version = $5 AND released_at IS NULL
	`, now, attemptID, salonID, target.ID, target.AuthorityAppointmentVersion)
	if err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if affected, err := releasedSegments.RowsAffected(); err != nil || int(affected) != len(target.Segments) {
		if err != nil {
			return nil, false, err
		}
		return nil, false, booking.ErrOperationConflict
	}
	if req.OperationType == scheduling.OperationKindReschedule {
		children, err = insertAppointmentPlan(ctx, tx, salonID, target.ID, newVersion, quote.Segments, aggregate, now)
		if err != nil {
			return nil, false, err
		}
		consumed, err := tx.ExecContext(ctx, `
			UPDATE availability_quotes SET consumed_at = $1, consumed_by_attempt_id = $2
			WHERE id = $3 AND salon_id = $4 AND scheduling_authority = 'manleai_calendar'
			  AND consumed_at IS NULL AND consumed_by_attempt_id IS NULL AND expires_at > $1
		`, now, attemptID, quote.QuoteID, salonID)
		if err != nil {
			return nil, false, classifyExecutionWriteError(err)
		}
		if affected, err := consumed.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return nil, false, err
			}
			return nil, false, booking.ErrAvailabilityQuoteStale
		}
	}
	updatedRoot, err := tx.ExecContext(ctx, `
		UPDATE appointments
		SET booking_attempt_id = $1, status = $2, service_id = $3, staff_id = $4,
		    staff_selection_mode = $5, start_time = $6, end_time = $7,
		    notes = NULLIF($8,''), authority_appointment_version = $9,
		    authority_config_version = $10, updated_at = $11
		WHERE salon_id = $12 AND id = $13
		  AND scheduling_authority = 'manleai_calendar'
		  AND authority_appointment_version = $14
		  AND status IN ('confirmed','rescheduled')
	`, attemptID, status, primary.ServiceID, primary.StaffID, primary.StaffSelectionMode,
		rootStart, rootEnd, rootNotes, newVersion, configVersion, now,
		salonID, target.ID, target.AuthorityAppointmentVersion)
	if err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if affected, err := updatedRoot.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return nil, false, err
		}
		return nil, false, booking.ErrOperationConflict
	}
	eventPayload := hashCalendarValue(children)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_execution_events (
			id, salon_id, booking_attempt_id, appointment_id, event_type,
			scheduling_authority_version, authority_config_version,
			authority_appointment_version, payload, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,jsonb_build_object('children_hash',$9::text),$10)
	`, uuid.NewString(), salonID, attemptID, target.ID, eventType,
		target.SchedulingAuthorityVersion, configVersion, newVersion, eventPayload, now); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if req.OperationType == scheduling.OperationKindReschedule {
		if _, err := tx.ExecContext(ctx, `SELECT validate_manleai_calendar_resource_capacity($1::uuid,$2::uuid)`, salonID, target.ID); err != nil {
			return nil, false, classifyExecutionWriteError(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `SELECT validate_manleai_calendar_execution_graph($1::uuid)`, attemptID); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT validate_manleai_calendar_lifecycle_graph($1::uuid)`, target.ID); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	authoritative, err := loadAuthoritativeInternalResultTx(
		ctx, tx, salonID, ownerUserID, target.ID, attemptID,
	)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	return authoritative, false, nil
}

func insertAttemptSnapshot(
	ctx context.Context,
	tx *sql.Tx,
	salonID string,
	attemptID string,
	segments []quotedAggregateSegment,
	aggregate *Aggregate,
	now time.Time,
) ([]InternalCreateSegmentResult, error) {
	children := make([]InternalCreateSegmentResult, 0, len(segments))
	for _, segment := range segments {
		attemptSegmentID := uuid.NewString()
		name := ""
		if aggregate != nil {
			name = serviceName(aggregate, segment.ServiceID)
		}
		if name == "" {
			name = strings.TrimSpace(segment.Name)
		}
		if name == "" {
			name = segment.ServiceID
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segments (
				id, salon_id, booking_attempt_id, service_id, staff_id, staff_selection_mode,
				guest_reference, pos_service_id, pos_service_version, pos_staff_id, name,
				duration_minutes, sort_order, scheduling_authority, authority_provider,
				authority_service_id, authority_service_version, authority_staff_id,
				scheduled_start_time, scheduled_end_time, buffer_before_minutes,
				buffer_after_minutes, occupied_start_time, occupied_end_time, created_at
			) VALUES (
				$1,$2,$3,$4::uuid,$5::uuid,$6,NULLIF($7,''),NULL,NULL,NULL,$8,$9,$10,
				'manleai_calendar',NULL,$4::uuid::text,NULL,$5::uuid::text,
				$11,$12,$13,$14,$15,$16,$17
			)
		`, attemptSegmentID, salonID, attemptID, segment.ServiceID, segment.StaffID,
			segment.StaffSelectionMode, segment.GuestReference, name, segment.DurationMinutes,
			segment.SortOrder, segment.StartTime, segment.EndTime, segment.BufferBeforeMinutes,
			segment.BufferAfterMinutes, segment.OccupiedStartTime, segment.OccupiedEndTime, now); err != nil {
			return nil, fmt.Errorf("insert lifecycle attempt segment: %w", classifyExecutionWriteError(err))
		}
		for _, allocation := range segment.ResourceAllocations {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO booking_attempt_segment_resource_allocations (
					id, salon_id, booking_attempt_segment_id, resource_pool_id,
					units_allocated, created_at
				) VALUES ($1,$2,$3,$4,$5,$6)
			`, uuid.NewString(), salonID, attemptSegmentID, allocation.ResourcePoolID,
				allocation.UnitsAllocated, now); err != nil {
				return nil, classifyExecutionWriteError(err)
			}
		}
		children = append(children, lifecycleChild(segment, ""))
	}
	return children, nil
}

func insertAppointmentPlan(
	ctx context.Context,
	tx *sql.Tx,
	salonID string,
	appointmentID string,
	planVersion int,
	segments []quotedAggregateSegment,
	aggregate *Aggregate,
	now time.Time,
) ([]InternalCreateSegmentResult, error) {
	children := make([]InternalCreateSegmentResult, 0, len(segments))
	for _, segment := range segments {
		appointmentServiceID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO appointment_services (
				id, salon_id, appointment_id, service_id, staff_id, staff_selection_mode,
				guest_reference, pos_service_id, pos_service_version, pos_staff_id, name,
				duration_minutes, sort_order, plan_version, scheduling_authority,
				authority_provider, authority_service_id, authority_service_version,
				authority_staff_id, scheduled_start_time, scheduled_end_time,
				buffer_before_minutes, buffer_after_minutes, occupied_start_time,
				occupied_end_time, created_at
			) VALUES (
				$1,$2,$3,$4::uuid,$5::uuid,$6,NULLIF($7,''),NULL,NULL,NULL,$8,$9,$10,$11,
				'manleai_calendar',NULL,$4::uuid::text,NULL,$5::uuid::text,
				$12,$13,$14,$15,$16,$17,$18
			)
		`, appointmentServiceID, salonID, appointmentID, segment.ServiceID, segment.StaffID,
			segment.StaffSelectionMode, segment.GuestReference, serviceName(aggregate, segment.ServiceID),
			segment.DurationMinutes, segment.SortOrder, planVersion, segment.StartTime,
			segment.EndTime, segment.BufferBeforeMinutes, segment.BufferAfterMinutes,
			segment.OccupiedStartTime, segment.OccupiedEndTime, now); err != nil {
			return nil, fmt.Errorf("insert lifecycle appointment segment: %w", classifyExecutionWriteError(err))
		}
		for _, allocation := range segment.ResourceAllocations {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO manleai_calendar_appointment_resource_allocations (
					id, salon_id, appointment_service_id, resource_pool_id,
					units_allocated, plan_version, occupied_start_time,
					occupied_end_time, created_at
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			`, uuid.NewString(), salonID, appointmentServiceID, allocation.ResourcePoolID,
				allocation.UnitsAllocated, planVersion, segment.OccupiedStartTime,
				segment.OccupiedEndTime, now); err != nil {
				return nil, classifyExecutionWriteError(err)
			}
		}
		children = append(children, lifecycleChild(segment, appointmentServiceID))
	}
	return children, nil
}

func lifecycleChild(segment quotedAggregateSegment, appointmentServiceID string) InternalCreateSegmentResult {
	return InternalCreateSegmentResult{
		AppointmentServiceID: appointmentServiceID, GuestReference: segment.GuestReference,
		ServiceID: segment.ServiceID, StaffID: segment.StaffID,
		StaffSelectionMode: segment.StaffSelectionMode, Quantity: 1,
		ScheduledStartTime: segment.StartTime, ScheduledEndTime: segment.EndTime,
		OccupiedStartTime: segment.OccupiedStartTime, OccupiedEndTime: segment.OccupiedEndTime,
		BufferBeforeMinutes: segment.BufferBeforeMinutes,
		BufferAfterMinutes:  segment.BufferAfterMinutes,
		ResourceAllocations: append([]InternalResourceAllocation{}, segment.ResourceAllocations...),
	}
}

func replayLifecycleTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, req InternalLifecycleRequest) (*InternalCreateResult, bool, error) {
	var authority, fingerprint, operationType, status, attemptID, appointmentID string
	var targetVersion, authorityVersion int
	err := tx.QueryRowContext(ctx, `
		SELECT attempt.scheduling_authority, attempt.request_fingerprint,
		       attempt.operation_type, attempt.status, attempt.id::text,
		       event.appointment_id::text, attempt.target_authority_appointment_version,
		       event.authority_appointment_version
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id = attempt.salon_id
		JOIN manleai_calendar_execution_events event
		  ON event.salon_id = attempt.salon_id AND event.booking_attempt_id = attempt.id
		WHERE attempt.salon_id = $1 AND salon.owner_user_id = $2
		  AND attempt.operation_key = $3
		FOR UPDATE OF attempt
	`, salonID, ownerUserID, req.OperationKey).Scan(
		&authority, &fingerprint, &operationType, &status, &attemptID,
		&appointmentID, &targetVersion, &authorityVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	wantStatus := booking.StatusRescheduled
	if req.OperationType == scheduling.OperationKindCancel {
		wantStatus = booking.StatusCancelled
	}
	if authority != booking.SchedulingAuthorityManleAICalendar ||
		fingerprint != req.RequestFingerprint || operationType != string(req.OperationType) ||
		status != wantStatus || appointmentID != req.TargetAppointmentID ||
		targetVersion != req.ExpectedTargetAuthorityAppointmentVersion {
		return nil, false, booking.ErrOperationConflict
	}
	result, err := loadAuthoritativeInternalResultTx(
		ctx, tx, salonID, ownerUserID, appointmentID, attemptID,
	)
	if err != nil {
		return nil, false, err
	}
	if result.TargetAuthorityAppointmentVersion != targetVersion ||
		result.AuthorityAppointmentVersion != authorityVersion {
		return nil, false, scheduling.ErrInvalidSchedulingResult
	}
	return result, true, nil
}

func loadAttemptSnapshot(ctx context.Context, tx *sql.Tx, salonID string, attemptID string) ([]InternalCreateSegmentResult, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT segment.id::text, COALESCE(segment.guest_reference,''),
		       segment.service_id::text, segment.staff_id::text,
		       segment.staff_selection_mode, segment.scheduled_start_time,
		       segment.scheduled_end_time, segment.occupied_start_time,
		       segment.occupied_end_time, segment.buffer_before_minutes,
		       segment.buffer_after_minutes
		FROM booking_attempt_segments segment
		WHERE segment.salon_id = $1 AND segment.booking_attempt_id = $2
		  AND segment.scheduling_authority = 'manleai_calendar'
		ORDER BY segment.sort_order
	`, salonID, attemptID)
	if err != nil {
		return nil, err
	}
	var children []InternalCreateSegmentResult
	var attemptSegmentIDs []string
	for rows.Next() {
		var child InternalCreateSegmentResult
		var attemptSegmentID string
		if err := rows.Scan(
			&attemptSegmentID, &child.GuestReference, &child.ServiceID, &child.StaffID,
			&child.StaffSelectionMode, &child.ScheduledStartTime, &child.ScheduledEndTime,
			&child.OccupiedStartTime, &child.OccupiedEndTime,
			&child.BufferBeforeMinutes, &child.BufferAfterMinutes,
		); err != nil {
			rows.Close()
			return nil, err
		}
		child.Quantity = 1
		attemptSegmentIDs = append(attemptSegmentIDs, attemptSegmentID)
		children = append(children, child)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index, attemptSegmentID := range attemptSegmentIDs {
		allocationRows, err := tx.QueryContext(ctx, `
			SELECT allocation.resource_pool_id::text, pool.name, allocation.units_allocated
			FROM booking_attempt_segment_resource_allocations allocation
			JOIN manleai_calendar_resource_pools pool
			  ON pool.salon_id = allocation.salon_id AND pool.id = allocation.resource_pool_id
			WHERE allocation.salon_id = $1 AND allocation.booking_attempt_segment_id = $2
			ORDER BY allocation.resource_pool_id
		`, salonID, attemptSegmentID)
		if err != nil {
			return nil, err
		}
		for allocationRows.Next() {
			var allocation InternalResourceAllocation
			if err := allocationRows.Scan(&allocation.ResourcePoolID, &allocation.ResourceName, &allocation.UnitsAllocated); err != nil {
				allocationRows.Close()
				return nil, err
			}
			children[index].ResourceAllocations = append(children[index].ResourceAllocations, allocation)
		}
		if err := allocationRows.Close(); err != nil {
			return nil, err
		}
	}
	return children, nil
}
