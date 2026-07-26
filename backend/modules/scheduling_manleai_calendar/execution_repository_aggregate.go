package scheduling_manleai_calendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/booking"
)

type quotedAggregateSlot struct {
	QuoteID                           string
	RequestFingerprint                string
	ExpiresAt                         time.Time
	ConsumedAt                        sql.NullTime
	ConsumedAttemptID                 sql.NullString
	AuthorityVersion                  int64
	ConfigVersion                     int64
	OperationType                     string
	TargetAppointmentID               string
	TargetAuthorityAppointmentVersion int
	PartySize                         int
	SlotID                            string
	SlotFingerprint                   string
	StartTime                         time.Time
	EndTime                           time.Time
	Segments                          []quotedAggregateSegment
}

type quotedAggregateSegment struct {
	ID                  string
	Name                string
	ServiceID           string
	StaffID             string
	StaffSelectionMode  string
	GuestReference      string
	DurationMinutes     int
	SortOrder           int
	StartTime           time.Time
	EndTime             time.Time
	BufferBeforeMinutes int
	BufferAfterMinutes  int
	OccupiedStartTime   time.Time
	OccupiedEndTime     time.Time
	ResourceAllocations []InternalResourceAllocation
}

// CheckAvailability creates one aggregate quote. Quantity is normalized into
// ordered quantity-one service units before planning or persistence.
func (r *Repository) CheckAvailability(ctx context.Context, salonID string, ownerUserID string, req booking.AvailabilityRequest, now time.Time) (*booking.AvailabilityResult, error) {
	targetAppointmentID := strings.TrimSpace(req.TargetAppointmentID)
	normalizedRequest := req
	normalizedRequest.TargetAppointmentID = ""
	normalized, err := normalizeAggregateAvailabilityRequest(normalizedRequest)
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
	if fence.ConfigVersion < 1 || fence.ActivatedVersion != fence.ConfigVersion ||
		(targetAppointmentID == "" && fence.Authority != booking.SchedulingAuthorityManleAICalendar) {
		return nil, booking.ErrSchedulingAuthorityNotReady
	}
	aggregate, err := loadAggregate(ctx, tx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	operationType := booking.BookingActionBook
	quoteAuthorityVersion := fence.AuthorityVersion
	targetVersion := 0
	if targetAppointmentID != "" {
		target, targetErr := lockLifecycleTarget(ctx, tx, salonID, ownerUserID, targetAppointmentID)
		if targetErr != nil {
			return nil, targetErr
		}
		if target.Status != booking.StatusConfirmed && target.Status != booking.StatusRescheduled {
			return nil, booking.ErrOperationConflict
		}
		if !lifecycleCutoffOpen(now, target.StartTime, aggregate.Config.RescheduleCutoffMinutes) {
			return nil, booking.ErrSchedulingAuthorityNotReady
		}
		if !replacementPreservesTargetShape(normalized, target) {
			return nil, booking.ErrValidation
		}
		operationType = booking.BookingActionReschedule
		quoteAuthorityVersion = target.SchedulingAuthorityVersion
		targetVersion = target.AuthorityAppointmentVersion
	}
	windowStart, windowEnd, err := localDateWindow(normalized.PreferredDate, fence.Timezone)
	if err != nil {
		return nil, err
	}
	conflicts, unresolved, err := loadStaffConflictsExcluding(ctx, tx, salonID, windowStart, windowEnd, targetAppointmentID)
	if err != nil {
		return nil, err
	}
	resourceConflicts, err := loadResourceConflictsExcluding(ctx, tx, salonID, windowStart, windowEnd, targetAppointmentID)
	if err != nil {
		return nil, err
	}
	planned, err := planAggregateAvailability(AvailabilitySnapshot{
		Aggregate: aggregate, Conflicts: conflicts, ResourceConflicts: resourceConflicts, UnresolvedExternalConflict: unresolved,
		TargetOriginAuthorized: targetAppointmentID != "",
	}, normalized, now)
	if err != nil {
		return nil, err
	}
	result := aggregateAvailabilityResult(aggregate, normalized, planned)
	result.TargetAuthorityAppointmentVersion = targetVersion
	if len(planned) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	requestFingerprint := aggregateAvailabilityRequestFingerprint(quoteAuthorityVersion, fence.ConfigVersion, normalized)
	if targetAppointmentID != "" {
		requestFingerprint = lifecycleAvailabilityRequestFingerprint(
			quoteAuthorityVersion, fence.ConfigVersion, targetAppointmentID, targetVersion, normalized,
		)
	}
	quoteID := uuid.NewString()
	expiresAt := now.Add(internalAvailabilityQuoteTTL)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO availability_quotes (
			id, salon_id, provider, provider_location_id, provider_snapshot_generation,
			request_fingerprint, expires_at, created_at,
			scheduling_authority, authority_provider, authority_location_id, authority_snapshot_generation,
			scheduling_authority_version, authority_config_version, operation_type,
			target_appointment_id, target_authority_appointment_version, party_size
		) VALUES ($1,$2,NULL,NULL,NULL,$3,$4,$5,'manleai_calendar',NULL,NULL,NULL,$6,$7,$8,NULLIF($9,'')::uuid,NULLIF($10,0),$11)
	`, quoteID, salonID, requestFingerprint, expiresAt, now, quoteAuthorityVersion, fence.ConfigVersion,
		operationType, targetAppointmentID, targetVersion, normalized.PartySize); err != nil {
		return nil, classifyExecutionWriteError(err)
	}
	for slotIndex := range planned {
		slot := &planned[slotIndex]
		slot.Fingerprint = aggregateAvailabilitySlotFingerprint(requestFingerprint, *slot)
		slotID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO availability_quote_slots (id, salon_id, quote_id, slot_fingerprint, start_time, end_time, segments, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,'[]'::jsonb,$7)
		`, slotID, salonID, quoteID, slot.Fingerprint, slot.StartTime, slot.EndTime, now); err != nil {
			return nil, classifyExecutionWriteError(err)
		}
		for segmentIndex, segment := range slot.Segments {
			segmentID := uuid.NewString()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO availability_quote_slot_segments (
					id, salon_id, quote_slot_id, service_id, staff_id, staff_selection_mode, guest_reference,
					duration_minutes, sort_order, scheduled_start_time, scheduled_end_time,
					buffer_before_minutes, buffer_after_minutes, occupied_start_time, occupied_end_time, created_at
				) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12,$13,$14,$15,$16)
			`, segmentID, salonID, slotID, segment.ServiceID, segment.StaffID, segment.StaffSelectionMode, segment.GuestReference,
				int(segment.EndTime.Sub(segment.StartTime)/time.Minute), segmentIndex+1, segment.StartTime, segment.EndTime,
				segment.BufferBeforeMinutes, segment.BufferAfterMinutes, segment.OccupiedStartTime, segment.OccupiedEndTime, now); err != nil {
				return nil, classifyExecutionWriteError(err)
			}
			for _, allocation := range segment.ResourceAllocations {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO availability_quote_slot_resource_allocations
						(id, salon_id, quote_slot_segment_id, resource_pool_id, units_allocated, created_at)
					VALUES ($1,$2,$3,$4,$5,$6)
				`, uuid.NewString(), salonID, segmentID, allocation.ResourcePoolID, allocation.UnitsAllocated, now); err != nil {
					return nil, classifyExecutionWriteError(err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, classifyExecutionWriteError(err)
	}
	result.QuoteID = quoteID
	result.RequestFingerprint = requestFingerprint
	result.ExpiresAt = &expiresAt
	result.Slots = aggregateAvailabilitySlots(planned)
	return result, nil
}

func normalizeAggregateAvailabilityRequest(req booking.AvailabilityRequest) (normalizedAvailabilityRequest, error) {
	if strings.TrimSpace(req.TargetAppointmentID) != "" || strings.TrimSpace(req.PreferredDate) == "" || req.Limit < 0 || req.Limit > 50 {
		return normalizedAvailabilityRequest{}, booking.ErrValidation
	}
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(req.PreferredDate)); err != nil {
		return normalizedAvailabilityRequest{}, booking.ErrValidation
	}
	rawSegments := append([]booking.BookingSegmentRequest{}, req.Segments...)
	if len(rawSegments) == 0 {
		rawSegments = []booking.BookingSegmentRequest{{ServiceID: req.ServiceID, StaffID: req.StaffID, StaffSelectionMode: req.StaffSelectionMode, Quantity: 1}}
	}
	normalized := normalizedAvailabilityRequest{PreferredDate: strings.TrimSpace(req.PreferredDate), Limit: req.Limit}
	guestReferences := make([]string, 0, len(rawSegments))
	for _, raw := range rawSegments {
		quantity := raw.Quantity
		if quantity == 0 {
			quantity = 1
		}
		serviceID := strings.TrimSpace(raw.ServiceID)
		staffID := strings.TrimSpace(raw.StaffID)
		staffMode := strings.TrimSpace(raw.StaffSelectionMode)
		guestReference := strings.TrimSpace(raw.GuestReference)
		if staffMode == "" {
			staffMode = booking.StaffSelectionSpecific
		}
		if !validUUID(serviceID) || quantity < 1 || quantity > 100 || len(guestReference) > 200 ||
			(staffMode != booking.StaffSelectionSpecific && staffMode != booking.StaffSelectionAnyone) ||
			(staffMode == booking.StaffSelectionSpecific && !validUUID(staffID)) ||
			(staffMode == booking.StaffSelectionAnyone && staffID != "") {
			return normalizedAvailabilityRequest{}, booking.ErrValidation
		}
		for unit := 0; unit < quantity; unit++ {
			unitGuest := guestReference
			if quantity > 1 {
				if unitGuest == "" {
					return normalizedAvailabilityRequest{}, booking.ErrValidation
				}
				unitGuest = fmt.Sprintf("%s-%d", unitGuest, unit+1)
			}
			guestReferences = append(guestReferences, unitGuest)
			normalized.Segments = append(normalized.Segments, normalizedAvailabilitySegment{
				ServiceID: serviceID, StaffID: staffID, StaffSelectionMode: staffMode, GuestReference: unitGuest, Quantity: 1,
			})
		}
	}
	partySize := req.PartySize
	if partySize == 0 {
		partySize = 1
		if distinctGuestReferences := distinctGuestReferenceCount(guestReferences); distinctGuestReferences > 0 {
			partySize = distinctGuestReferences
		}
	}
	if partySize < 1 || partySize > 100 || !validGuestReferenceParty(partySize, guestReferences) {
		return normalizedAvailabilityRequest{}, booking.ErrValidation
	}
	normalized.PartySize = partySize
	first := normalized.Segments[0]
	normalized.ServiceID, normalized.StaffID, normalized.StaffSelectionMode = first.ServiceID, first.StaffID, first.StaffSelectionMode
	return normalized, nil
}

// validGuestReferenceParty keeps aggregate planning aligned with the persisted
// party evidence: guest references are either absent for one guest, or every
// normalized segment names one of exactly partySize distinct guests.
func validGuestReferenceParty(partySize int, guestReferences []string) bool {
	anyReference := false
	allReferencesPresent := true
	distinctReferences := make(map[string]struct{}, len(guestReferences))
	for _, guestReference := range guestReferences {
		guestReference = strings.TrimSpace(guestReference)
		if guestReference == "" {
			allReferencesPresent = false
			continue
		}
		anyReference = true
		distinctReferences[guestReference] = struct{}{}
	}
	if !anyReference {
		return partySize == 1
	}
	return allReferencesPresent && len(distinctReferences) == partySize
}

func distinctGuestReferenceCount(guestReferences []string) int {
	distinctReferences := make(map[string]struct{}, len(guestReferences))
	for _, guestReference := range guestReferences {
		if guestReference = strings.TrimSpace(guestReference); guestReference != "" {
			distinctReferences[guestReference] = struct{}{}
		}
	}
	return len(distinctReferences)
}

func loadResourceConflicts(ctx context.Context, tx *sql.Tx, salonID string, windowStart time.Time, windowEnd time.Time) ([]ResourceConflict, error) {
	return loadResourceConflictsExcluding(ctx, tx, salonID, windowStart, windowEnd, "")
}

func loadResourceConflictsExcluding(ctx context.Context, tx *sql.Tx, salonID string, windowStart time.Time, windowEnd time.Time, excludedAppointmentID string) ([]ResourceConflict, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT allocation.resource_pool_id::text, allocation.units_allocated,
		       allocation.occupied_start_time, allocation.occupied_end_time
		FROM manleai_calendar_appointment_resource_allocations allocation
		JOIN appointment_services segment ON segment.salon_id = allocation.salon_id AND segment.id = allocation.appointment_service_id
		JOIN appointments appointment ON appointment.salon_id = segment.salon_id AND appointment.id = segment.appointment_id
		WHERE allocation.salon_id = $1 AND allocation.released_at IS NULL AND segment.released_at IS NULL
		  AND ($4 = '' OR appointment.id::text <> $4)
		  AND appointment.status IN ('confirmed','rescheduled')
		  AND allocation.occupied_start_time < $3 AND allocation.occupied_end_time > $2
		ORDER BY allocation.resource_pool_id, allocation.occupied_start_time, allocation.id
	`, salonID, windowStart, windowEnd, strings.TrimSpace(excludedAppointmentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ResourceConflict{}
	for rows.Next() {
		var item ResourceConflict
		if err := rows.Scan(&item.ResourcePoolID, &item.UnitsAllocated, &item.StartsAt, &item.EndsAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func aggregateAvailabilityResult(aggregate *Aggregate, req normalizedAvailabilityRequest, slots []InternalAvailabilitySlot) *booking.AvailabilityResult {
	result := &booking.AvailabilityResult{
		ServiceID: req.ServiceID, StaffID: req.StaffID, StaffSelectionMode: req.StaffSelectionMode,
		PreferredDate: req.PreferredDate, Timezone: aggregate.Timezone, Slots: aggregateAvailabilitySlots(slots),
	}
	if len(slots) > 0 && len(slots[0].Segments) > 0 {
		result.ServiceName = slots[0].Segments[0].ServiceName
		result.DurationMinutes = int(slots[0].EndTime.Sub(slots[0].StartTime) / time.Minute)
		result.Segments = availabilitySegmentsFromInternal(slots[0].Segments)
	}
	return result
}

func aggregateAvailabilitySlots(slots []InternalAvailabilitySlot) []booking.AvailabilitySlot {
	result := make([]booking.AvailabilitySlot, 0, len(slots))
	for _, slot := range slots {
		result = append(result, booking.AvailabilitySlot{
			Fingerprint: slot.Fingerprint, StartTime: slot.StartTime, EndTime: slot.EndTime,
			StaffID: slot.StaffID, StaffName: slot.StaffName, StaffSelectionMode: slot.Segments[0].StaffSelectionMode,
			Segments: availabilitySegmentsFromInternal(slot.Segments),
		})
	}
	return result
}

func availabilitySegmentsFromInternal(segments []InternalAvailabilitySegment) []booking.AvailabilitySegment {
	result := make([]booking.AvailabilitySegment, 0, len(segments))
	for _, segment := range segments {
		allocations := make([]booking.AvailabilityResourceAllocation, 0, len(segment.ResourceAllocations))
		for _, allocation := range segment.ResourceAllocations {
			allocations = append(allocations, booking.AvailabilityResourceAllocation{ResourcePoolID: allocation.ResourcePoolID, ResourceName: allocation.ResourceName, UnitsAllocated: allocation.UnitsAllocated})
		}
		result = append(result, booking.AvailabilitySegment{
			ServiceID: segment.ServiceID, ServiceName: segment.ServiceName, StaffID: segment.StaffID, StaffName: segment.StaffName,
			StaffSelectionMode: segment.StaffSelectionMode, GuestReference: segment.GuestReference, Quantity: 1,
			DurationMinutes:    int(segment.EndTime.Sub(segment.StartTime) / time.Minute),
			ScheduledStartTime: segment.StartTime, ScheduledEndTime: segment.EndTime,
			OccupiedStartTime: segment.OccupiedStartTime, OccupiedEndTime: segment.OccupiedEndTime,
			BufferBeforeMinutes: segment.BufferBeforeMinutes, BufferAfterMinutes: segment.BufferAfterMinutes,
			ResourceAllocations: allocations,
		})
	}
	return result
}

func aggregateAvailabilityRequestFingerprint(authorityVersion int64, configVersion int64, req normalizedAvailabilityRequest) string {
	return hashCalendarValue(struct {
		AuthorityVersion int64                           `json:"authority_version"`
		ConfigVersion    int64                           `json:"config_version"`
		PartySize        int                             `json:"party_size"`
		Segments         []normalizedAvailabilitySegment `json:"segments"`
		PreferredDate    string                          `json:"preferred_date"`
	}{authorityVersion, configVersion, req.PartySize, req.Segments, req.PreferredDate})
}

func aggregateAvailabilitySlotFingerprint(requestFingerprint string, slot InternalAvailabilitySlot) string {
	return hashCalendarValue(struct {
		RequestFingerprint string                        `json:"request_fingerprint"`
		StartTime          string                        `json:"start_time"`
		EndTime            string                        `json:"end_time"`
		Segments           []InternalAvailabilitySegment `json:"segments"`
	}{requestFingerprint, slot.StartTime.Format(time.RFC3339Nano), slot.EndTime.Format(time.RFC3339Nano), slot.Segments})
}

func lockQuotedAggregateSlot(ctx context.Context, tx *sql.Tx, salonID string, quoteID string, fingerprint string) (quotedAggregateSlot, error) {
	var item quotedAggregateSlot
	err := tx.QueryRowContext(ctx, `
		SELECT quote.id::text, quote.request_fingerprint, quote.expires_at, quote.consumed_at,
		       quote.consumed_by_attempt_id::text, quote.scheduling_authority_version, quote.authority_config_version,
		       quote.operation_type, COALESCE(quote.target_appointment_id::text,''),
		       COALESCE(quote.target_authority_appointment_version,0), quote.party_size,
		       slot.id::text, slot.slot_fingerprint, slot.start_time, slot.end_time
		FROM availability_quotes quote
		JOIN availability_quote_slots slot ON slot.salon_id = quote.salon_id AND slot.quote_id = quote.id
		WHERE quote.id = $1 AND quote.salon_id = $2 AND quote.scheduling_authority = 'manleai_calendar' AND slot.slot_fingerprint = $3
		FOR UPDATE OF quote, slot
	`, quoteID, salonID, fingerprint).Scan(
		&item.QuoteID, &item.RequestFingerprint, &item.ExpiresAt, &item.ConsumedAt, &item.ConsumedAttemptID,
		&item.AuthorityVersion, &item.ConfigVersion, &item.OperationType,
		&item.TargetAppointmentID, &item.TargetAuthorityAppointmentVersion, &item.PartySize,
		&item.SlotID, &item.SlotFingerprint, &item.StartTime, &item.EndTime,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return quotedAggregateSlot{}, booking.ErrAvailabilityQuoteStale
	}
	if err != nil {
		return quotedAggregateSlot{}, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT segment.id::text, segment.service_id::text, segment.staff_id::text, segment.staff_selection_mode,
		       COALESCE(segment.guest_reference,''), segment.duration_minutes, segment.sort_order,
		       segment.scheduled_start_time, segment.scheduled_end_time, segment.buffer_before_minutes,
		       segment.buffer_after_minutes, segment.occupied_start_time, segment.occupied_end_time
		FROM availability_quote_slot_segments segment
		WHERE segment.salon_id = $1 AND segment.quote_slot_id = $2
		ORDER BY segment.sort_order
		FOR UPDATE
	`, salonID, item.SlotID)
	if err != nil {
		return quotedAggregateSlot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var segment quotedAggregateSegment
		if err := rows.Scan(&segment.ID, &segment.ServiceID, &segment.StaffID, &segment.StaffSelectionMode, &segment.GuestReference,
			&segment.DurationMinutes, &segment.SortOrder, &segment.StartTime, &segment.EndTime, &segment.BufferBeforeMinutes,
			&segment.BufferAfterMinutes, &segment.OccupiedStartTime, &segment.OccupiedEndTime); err != nil {
			return quotedAggregateSlot{}, err
		}
		item.Segments = append(item.Segments, segment)
	}
	if err := rows.Err(); err != nil {
		return quotedAggregateSlot{}, err
	}
	for index := range item.Segments {
		allocationRows, err := tx.QueryContext(ctx, `
			SELECT allocation.resource_pool_id::text, pool.name, allocation.units_allocated
			FROM availability_quote_slot_resource_allocations allocation
			JOIN manleai_calendar_resource_pools pool ON pool.salon_id = allocation.salon_id AND pool.id = allocation.resource_pool_id
			WHERE allocation.salon_id = $1 AND allocation.quote_slot_segment_id = $2
			ORDER BY allocation.resource_pool_id
			FOR UPDATE OF allocation, pool
		`, salonID, item.Segments[index].ID)
		if err != nil {
			return quotedAggregateSlot{}, err
		}
		for allocationRows.Next() {
			var allocation InternalResourceAllocation
			if err := allocationRows.Scan(&allocation.ResourcePoolID, &allocation.ResourceName, &allocation.UnitsAllocated); err != nil {
				allocationRows.Close()
				return quotedAggregateSlot{}, err
			}
			item.Segments[index].ResourceAllocations = append(item.Segments[index].ResourceAllocations, allocation)
		}
		if err := allocationRows.Close(); err != nil {
			return quotedAggregateSlot{}, err
		}
	}
	return item, nil
}

func quotedSlotAsPlan(quote quotedAggregateSlot, aggregate *Aggregate) InternalAvailabilitySlot {
	segments := make([]InternalAvailabilitySegment, 0, len(quote.Segments))
	for _, segment := range quote.Segments {
		segments = append(segments, InternalAvailabilitySegment{
			ServiceID: segment.ServiceID, ServiceName: serviceName(aggregate, segment.ServiceID), StaffID: segment.StaffID,
			StaffName: staffName(aggregate, segment.StaffID), StaffSelectionMode: segment.StaffSelectionMode,
			GuestReference: segment.GuestReference, Quantity: 1, StartTime: segment.StartTime, EndTime: segment.EndTime,
			OccupiedStartTime: segment.OccupiedStartTime, OccupiedEndTime: segment.OccupiedEndTime,
			BufferBeforeMinutes: segment.BufferBeforeMinutes, BufferAfterMinutes: segment.BufferAfterMinutes,
			ResourceAllocations: append([]InternalResourceAllocation{}, segment.ResourceAllocations...),
		})
	}
	return InternalAvailabilitySlot{Fingerprint: quote.SlotFingerprint, StartTime: quote.StartTime, EndTime: quote.EndTime, Segments: segments}
}

func staffName(aggregate *Aggregate, staffID string) string {
	for _, profile := range aggregate.StaffProfiles {
		if profile.Staff.ID == staffID {
			return profile.Staff.Name
		}
	}
	return ""
}

func quotedRequest(quote quotedAggregateSlot, timezone string) normalizedAvailabilityRequest {
	location, _ := time.LoadLocation(timezone)
	result := normalizedAvailabilityRequest{PartySize: quote.PartySize, PreferredDate: quote.StartTime.In(location).Format("2006-01-02"), Limit: -1}
	for _, segment := range quote.Segments {
		staffID := segment.StaffID
		if segment.StaffSelectionMode == booking.StaffSelectionAnyone {
			staffID = ""
		}
		result.Segments = append(result.Segments, normalizedAvailabilitySegment{
			ServiceID: segment.ServiceID, StaffID: staffID, StaffSelectionMode: segment.StaffSelectionMode,
			GuestReference: segment.GuestReference, Quantity: 1,
		})
	}
	if len(result.Segments) > 0 {
		result.ServiceID, result.StaffID, result.StaffSelectionMode = result.Segments[0].ServiceID, result.Segments[0].StaffID, result.Segments[0].StaffSelectionMode
	}
	return result
}

func quoteMatchesAction(quote quotedAggregateSlot, req InternalCreateRequest) bool {
	if quote.PartySize != req.PartySize || len(quote.Segments) != len(req.Segments) || !quote.StartTime.Equal(req.RequestedStartTime) || !quote.EndTime.Equal(req.RequestedEndTime) {
		return false
	}
	for index, quoted := range quote.Segments {
		requested := req.Segments[index]
		if quoted.SortOrder != index+1 || quoted.ServiceID != requested.ServiceID || quoted.StaffID != requested.StaffID ||
			quoted.StaffSelectionMode != requested.StaffSelectionMode || quoted.GuestReference != requested.GuestReference || requested.Quantity != 1 ||
			!quoted.StartTime.Equal(requested.RequestedStartTime) || !quoted.EndTime.Equal(requested.RequestedEndTime) {
			return false
		}
	}
	return true
}

func exactAggregatePlan(planned []InternalAvailabilitySlot, quoted InternalAvailabilitySlot) bool {
	quotedHash := aggregateAvailabilitySlotFingerprint("", quoted)
	for _, candidate := range planned {
		if aggregateAvailabilitySlotFingerprint("", candidate) == quotedHash {
			return true
		}
	}
	return false
}

func distinctPoolIDs(segments []quotedAggregateSegment) []string {
	set := map[string]struct{}{}
	for _, segment := range segments {
		for _, allocation := range segment.ResourceAllocations {
			set[allocation.ResourcePoolID] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func lockAggregateCanonicalRows(ctx context.Context, tx *sql.Tx, salonID string, quote quotedAggregateSlot) error {
	serviceSet, staffSet := map[string]struct{}{}, map[string]struct{}{}
	pairSet := map[string]struct{}{}
	for _, segment := range quote.Segments {
		serviceSet[segment.ServiceID] = struct{}{}
		staffSet[segment.StaffID] = struct{}{}
		pairSet[segment.ServiceID+":"+segment.StaffID] = struct{}{}
	}
	services, staff := mapKeysSorted(serviceSet), mapKeysSorted(staffSet)
	var serviceCount, staffCount int
	if err := tx.QueryRowContext(ctx, `
		WITH locked AS (
			SELECT service.id FROM services service
			JOIN manleai_calendar_service_policies policy ON policy.salon_id=service.salon_id AND policy.service_id=service.id
			WHERE service.salon_id=$1 AND service.id=ANY($2::uuid[]) AND service.active AND service.ai_bookable AND service.archived_at IS NULL
			  AND policy.enabled AND policy.capacity_mode IN ('staff_only','pooled') ORDER BY service.id FOR UPDATE OF service, policy
		) SELECT count(*) FROM locked
	`, salonID, pq.Array(services)).Scan(&serviceCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		WITH locked AS (
			SELECT staff.id FROM staff WHERE staff.salon_id=$1 AND staff.id=ANY($2::uuid[])
			  AND staff.active AND staff.ai_bookable AND staff.archived_at IS NULL ORDER BY staff.id FOR UPDATE
		) SELECT count(*) FROM locked
	`, salonID, pq.Array(staff)).Scan(&staffCount); err != nil {
		return err
	}
	if serviceCount != len(services) || staffCount != len(staff) {
		return booking.ErrAvailabilityQuoteStale
	}
	pairs := mapKeysSorted(pairSet)
	pairServices := make([]string, 0, len(pairs))
	pairStaff := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		pairServices = append(pairServices, parts[0])
		pairStaff = append(pairStaff, parts[1])
	}
	var pairCount int
	if err := tx.QueryRowContext(ctx, `
		WITH requested AS (
			SELECT * FROM unnest($2::uuid[],$3::uuid[]) AS pair(service_id,staff_id)
		), locked AS (
			SELECT link.service_id, link.staff_id
			FROM manleai_calendar_service_staff link
			JOIN requested ON requested.service_id=link.service_id AND requested.staff_id=link.staff_id
			WHERE link.salon_id=$1 ORDER BY link.service_id,link.staff_id FOR UPDATE OF link
		) SELECT count(*) FROM locked
	`, salonID, pq.Array(pairServices), pq.Array(pairStaff)).Scan(&pairCount); err != nil {
		return err
	}
	if pairCount != len(pairs) {
		return booking.ErrAvailabilityQuoteStale
	}
	// Requirements are part of the exact quote evidence. Lock the complete
	// service-owned set in canonical order so a concurrent configuration write
	// cannot change resource demand between recheck and persistence.
	if _, err := tx.ExecContext(ctx, `
		SELECT requirement.service_id, requirement.resource_pool_id
		FROM manleai_calendar_service_resources requirement
		WHERE requirement.salon_id=$1 AND requirement.service_id=ANY($2::uuid[])
		ORDER BY requirement.service_id,requirement.resource_pool_id FOR UPDATE
	`, salonID, pq.Array(services)); err != nil {
		return err
	}
	return nil
}

func mapKeysSorted(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// CreateAppointment consumes an exact aggregate quote and persists one root
// appointment plus all child segments and allocations atomically.
func (r *Repository) CreateAppointment(ctx context.Context, salonID string, ownerUserID string, req InternalCreateRequest, now time.Time) (*InternalCreateResult, bool, error) {
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := lockCalendarExecutionTx(ctx, tx, salonID); err != nil {
		return nil, false, err
	}
	if replay, found, err := replayAggregateCreateTx(ctx, tx, salonID, ownerUserID, req); err != nil {
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
	quote, err := lockQuotedAggregateSlot(ctx, tx, salonID, req.AvailabilityQuoteID, req.SlotFingerprint)
	if err != nil {
		return nil, false, err
	}
	if fence.Authority != booking.SchedulingAuthorityManleAICalendar || fence.AuthorityVersion != quote.AuthorityVersion ||
		fence.ConfigVersion != quote.ConfigVersion || fence.ActivatedVersion != fence.ConfigVersion || quote.OperationType != booking.BookingActionBook ||
		!quote.ExpiresAt.After(now) || quote.ConsumedAt.Valid || quote.ConsumedAttemptID.Valid || req.RequestedTimezone != fence.Timezone ||
		!quoteMatchesAction(quote, req) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}
	requested := quotedRequest(quote, fence.Timezone)
	if quote.RequestFingerprint != aggregateAvailabilityRequestFingerprint(quote.AuthorityVersion, quote.ConfigVersion, requested) {
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
	conflicts, unresolved, err := loadStaffConflicts(ctx, tx, salonID, windowStart, windowEnd)
	if err != nil {
		return nil, false, err
	}
	resourceConflicts, err := loadResourceConflicts(ctx, tx, salonID, windowStart, windowEnd)
	if err != nil {
		return nil, false, err
	}
	planned, err := planAggregateAvailability(AvailabilitySnapshot{
		Aggregate: aggregate, Conflicts: conflicts, ResourceConflicts: resourceConflicts, UnresolvedExternalConflict: unresolved,
	}, requested, now)
	if err != nil {
		if errors.Is(err, booking.ErrValidation) || errors.Is(err, booking.ErrSchedulingAuthorityNotReady) || errors.Is(err, ErrAvailabilityConflictState) {
			return nil, false, booking.ErrAvailabilityQuoteStale
		}
		return nil, false, err
	}
	quotedPlan := quotedSlotAsPlan(quote, aggregate)
	if quote.SlotFingerprint != aggregateAvailabilitySlotFingerprint(quote.RequestFingerprint, quotedPlan) || !exactAggregatePlan(planned, quotedPlan) {
		return nil, false, booking.ErrAvailabilityQuoteStale
	}

	attemptID := uuid.NewString()
	appointmentID := uuid.NewString()
	primary := quote.Segments[0]
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
			$1,$2,$3,'confirmed',NULL,NULL,NULL,NULL,NULL,$4,$5,$6,$7,'book',NULL,NULL,NULL,'not_started','none','not_required',
			$8,$9,NULLIF($10,''),$11,$12,$13,$14,$15,NULLIF($16,''),NULL,NULL,
			'manleai_calendar',NULL,$17,1,NULL,$4,NULL,NULL,$18,$19,$20,$21,$21
		)
	`, attemptID, salonID, req.Source, req.OperationKey, req.RequestFingerprint, quote.QuoteID, quote.SlotFingerprint,
		req.CustomerName, req.CustomerPhone, req.CustomerEmail, primary.ServiceID, primary.StaffID, primary.StaffSelectionMode,
		quote.StartTime, quote.EndTime, req.Notes, appointmentID, quote.AuthorityVersion, quote.ConfigVersion, quote.PartySize, now); err != nil {
		return nil, false, fmt.Errorf("insert aggregate booking attempt: %w", classifyExecutionWriteError(err))
	}
	type stagedSegment struct {
		quotedAggregateSegment
		AttemptSegmentID     string
		AppointmentServiceID string
	}
	staged := make([]stagedSegment, 0, len(quote.Segments))
	for _, segment := range quote.Segments {
		attemptSegmentID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO booking_attempt_segments (
				id, salon_id, booking_attempt_id, service_id, staff_id, staff_selection_mode, guest_reference,
				pos_service_id, pos_service_version, pos_staff_id, name, duration_minutes, sort_order,
				scheduling_authority, authority_provider, authority_service_id, authority_service_version, authority_staff_id,
				scheduled_start_time, scheduled_end_time, buffer_before_minutes, buffer_after_minutes,
				occupied_start_time, occupied_end_time, created_at
			) VALUES (
				$1,$2,$3,$4::uuid,$5::uuid,$6,NULLIF($7,''),NULL,NULL,NULL,$8,$9,$10,
				'manleai_calendar',NULL,$4::uuid::text,NULL,$5::uuid::text,$11,$12,$13,$14,$15,$16,$17
			)
		`, attemptSegmentID, salonID, attemptID, segment.ServiceID, segment.StaffID, segment.StaffSelectionMode, segment.GuestReference,
			serviceName(aggregate, segment.ServiceID), segment.DurationMinutes, segment.SortOrder, segment.StartTime, segment.EndTime,
			segment.BufferBeforeMinutes, segment.BufferAfterMinutes, segment.OccupiedStartTime, segment.OccupiedEndTime, now); err != nil {
			return nil, false, fmt.Errorf("insert aggregate attempt segment: %w", classifyExecutionWriteError(err))
		}
		for _, allocation := range segment.ResourceAllocations {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO booking_attempt_segment_resource_allocations
					(id, salon_id, booking_attempt_segment_id, resource_pool_id, units_allocated, created_at)
				VALUES ($1,$2,$3,$4,$5,$6)
			`, uuid.NewString(), salonID, attemptSegmentID, allocation.ResourcePoolID, allocation.UnitsAllocated, now); err != nil {
				return nil, false, classifyExecutionWriteError(err)
			}
		}
		staged = append(staged, stagedSegment{quotedAggregateSegment: segment, AttemptSegmentID: attemptSegmentID, AppointmentServiceID: uuid.NewString()})
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO appointments (
			id, salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			pos_customer_id, pos_sync_status, last_pos_synced_at, pos_sync_error, status,
			customer_name, customer_phone, customer_email, service_id, staff_id, staff_selection_mode,
			start_time, end_time, notes, scheduling_authority, authority_provider, authority_appointment_id,
			authority_appointment_version, authority_customer_id, confirmed_at, confirmed_by_user_id, confirmation_source,
			scheduling_authority_version, authority_config_version, party_size, created_at, updated_at
		) VALUES (
			$1::uuid,$2,$3,NULL,NULL,NULL,NULL,NULL,NULL,NULL,'confirmed',$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,NULLIF($12,''),
			'manleai_calendar',NULL,$1::uuid::text,1,NULL,$13,NULL,'manleai_calendar',$14,$15,$16,$13,$13
		)
	`, appointmentID, salonID, attemptID, req.CustomerName, req.CustomerPhone, req.CustomerEmail,
		primary.ServiceID, primary.StaffID, primary.StaffSelectionMode, quote.StartTime, quote.EndTime, req.Notes,
		now, quote.AuthorityVersion, quote.ConfigVersion, quote.PartySize); err != nil {
		return nil, false, fmt.Errorf("insert aggregate appointment: %w", classifyExecutionWriteError(err))
	}
	children := make([]InternalCreateSegmentResult, 0, len(staged))
	for _, segment := range staged {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO appointment_services (
				id, salon_id, appointment_id, service_id, staff_id, staff_selection_mode, guest_reference,
				pos_service_id, pos_service_version, pos_staff_id, name, duration_minutes, sort_order, plan_version,
				scheduling_authority, authority_provider, authority_service_id, authority_service_version, authority_staff_id,
				scheduled_start_time, scheduled_end_time, buffer_before_minutes, buffer_after_minutes,
				occupied_start_time, occupied_end_time, created_at
			) VALUES (
				$1,$2,$3,$4::uuid,$5::uuid,$6,NULLIF($7,''),NULL,NULL,NULL,$8,$9,$10,1,
				'manleai_calendar',NULL,$4::uuid::text,NULL,$5::uuid::text,$11,$12,$13,$14,$15,$16,$17
			)
		`, segment.AppointmentServiceID, salonID, appointmentID, segment.ServiceID, segment.StaffID, segment.StaffSelectionMode,
			segment.GuestReference, serviceName(aggregate, segment.ServiceID), segment.DurationMinutes, segment.SortOrder,
			segment.StartTime, segment.EndTime, segment.BufferBeforeMinutes, segment.BufferAfterMinutes,
			segment.OccupiedStartTime, segment.OccupiedEndTime, now); err != nil {
			return nil, false, fmt.Errorf("insert aggregate reservation: %w", classifyExecutionWriteError(err))
		}
		for _, allocation := range segment.ResourceAllocations {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO manleai_calendar_appointment_resource_allocations (
					id, salon_id, appointment_service_id, resource_pool_id, units_allocated, plan_version,
					occupied_start_time, occupied_end_time, created_at
				) VALUES ($1,$2,$3,$4,$5,1,$6,$7,$8)
			`, uuid.NewString(), salonID, segment.AppointmentServiceID, allocation.ResourcePoolID, allocation.UnitsAllocated,
				segment.OccupiedStartTime, segment.OccupiedEndTime, now); err != nil {
				return nil, false, classifyExecutionWriteError(err)
			}
		}
		children = append(children, InternalCreateSegmentResult{
			AppointmentServiceID: segment.AppointmentServiceID, GuestReference: segment.GuestReference,
			ServiceID: segment.ServiceID, StaffID: segment.StaffID, StaffSelectionMode: segment.StaffSelectionMode, Quantity: 1,
			ScheduledStartTime: segment.StartTime, ScheduledEndTime: segment.EndTime,
			OccupiedStartTime: segment.OccupiedStartTime, OccupiedEndTime: segment.OccupiedEndTime,
			BufferBeforeMinutes: segment.BufferBeforeMinutes, BufferAfterMinutes: segment.BufferAfterMinutes,
			ResourceAllocations: append([]InternalResourceAllocation{}, segment.ResourceAllocations...),
		})
	}
	consumed, err := tx.ExecContext(ctx, `
		UPDATE availability_quotes SET consumed_at=$1, consumed_by_attempt_id=$2
		WHERE id=$3 AND salon_id=$4 AND scheduling_authority='manleai_calendar'
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
	eventPayload := hashCalendarValue(children)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO manleai_calendar_execution_events (
			id, salon_id, booking_attempt_id, appointment_id, event_type,
			scheduling_authority_version, authority_config_version, authority_appointment_version, payload, created_at
		) VALUES ($1,$2,$3,$4,'appointment_confirmed',$5,$6,1,jsonb_build_object('children_hash',$7::text),$8)
	`, uuid.NewString(), salonID, attemptID, appointmentID, quote.AuthorityVersion, quote.ConfigVersion, eventPayload, now); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT validate_manleai_calendar_resource_capacity($1::uuid,$2::uuid)`, salonID, appointmentID); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT validate_manleai_calendar_execution_graph($1::uuid)`, attemptID); err != nil {
		return nil, false, classifyExecutionWriteError(err)
	}
	if _, err := tx.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
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

func replayAggregateCreateTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, req InternalCreateRequest) (*InternalCreateResult, bool, error) {
	var authority, fingerprint, status, attemptID string
	var appointmentID sql.NullString
	var authorityAppointmentVersion int
	err := tx.QueryRowContext(ctx, `
		SELECT attempt.scheduling_authority, attempt.request_fingerprint, attempt.status,
		       attempt.id::text, appointment.id::text, event.authority_appointment_version
		FROM booking_attempts attempt
		JOIN salons salon ON salon.id=attempt.salon_id
		LEFT JOIN manleai_calendar_execution_events event ON event.salon_id=attempt.salon_id AND event.booking_attempt_id=attempt.id AND event.event_type='appointment_confirmed'
		LEFT JOIN appointments appointment ON appointment.salon_id=event.salon_id AND appointment.id=event.appointment_id
		WHERE attempt.salon_id=$1 AND salon.owner_user_id=$2 AND attempt.operation_key=$3
		FOR UPDATE OF attempt
	`, salonID, ownerUserID, req.OperationKey).Scan(
		&authority, &fingerprint, &status, &attemptID, &appointmentID, &authorityAppointmentVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if authority != booking.SchedulingAuthorityManleAICalendar || fingerprint != req.RequestFingerprint || status != booking.StatusConfirmed || !appointmentID.Valid {
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
