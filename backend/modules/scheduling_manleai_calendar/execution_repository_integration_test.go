package scheduling_manleai_calendar

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

var executionTestNow = time.Date(2026, time.February, 8, 12, 0, 0, 0, time.UTC)

func TestExecutionRepositoryPostgresAtomicCreateReplayChangedProofAndTenantIsolation(t *testing.T) {
	fixture, repository := newReadyExecutionPGFixture(t)
	ctx := context.Background()
	availability := requireExecutionAvailability(t, repository, fixture, booking.StaffSelectionAnyone)
	if _, err := repository.CheckStaffOnlyAvailability(ctx, fixture.salonID, fixture.otherOwnerID, executionAvailabilityRequest(fixture, booking.StaffSelectionSpecific), executionTestNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant availability error = %v, want ErrNotFound", err)
	}
	action := executionAction(fixture, availability, "atomic-create", "Atomic Caller")
	normalized, err := normalizeInternalCreateRequest(action)
	if err != nil {
		t.Fatalf("normalize create: %v", err)
	}
	if _, _, err := repository.CreateStaffOnlyAppointment(ctx, fixture.salonID, fixture.otherOwnerID, normalized, executionTestNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant create error = %v, want ErrNotFound", err)
	}
	assertExecutionResidue(t, fixture, 0, 0, 0, 0)

	created, replayed, err := repository.CreateStaffOnlyAppointment(ctx, fixture.salonID, fixture.ownerID, normalized, executionTestNow)
	if err != nil || replayed || created == nil || created.AppointmentID == "" || created.BookingAttemptID == "" ||
		created.AppointmentStatus != booking.StatusConfirmed || created.AuthorityAppointmentVersion != 1 ||
		created.ActiveChildCount != 1 || len(created.Children) != 1 || created.Children[0].AppointmentServiceID == "" {
		t.Fatalf("create result = %#v replay=%t error=%v", created, replayed, err)
	}
	assertExecutionResidue(t, fixture, 1, 1, 1, 1)
	var fakeAttemptEvidence, fakeAppointmentEvidence, fakeSegmentEvidence int
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT count(*) FROM booking_attempts
		WHERE id = $1 AND (
			pos_provider IS NOT NULL OR pos_booking_id IS NOT NULL OR pos_booking_version IS NOT NULL
			OR target_pos_booking_version IS NOT NULL OR pos_idempotency_key IS NOT NULL
			OR provider_location_id IS NOT NULL OR provider_snapshot_generation IS NOT NULL
			OR authority_provider IS NOT NULL OR authority_location_id IS NOT NULL OR authority_snapshot_generation IS NOT NULL
		)
	`, created.BookingAttemptID).Scan(&fakeAttemptEvidence); err != nil {
		t.Fatalf("inspect attempt evidence: %v", err)
	}
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT count(*) FROM appointments
		WHERE id = $1 AND (
			pos_provider IS NOT NULL OR pos_appointment_id IS NOT NULL OR pos_appointment_version IS NOT NULL
			OR pos_customer_id IS NOT NULL OR pos_sync_status IS NOT NULL OR last_pos_synced_at IS NOT NULL
			OR pos_sync_error IS NOT NULL OR authority_provider IS NOT NULL OR authority_customer_id IS NOT NULL
		)
	`, created.AppointmentID).Scan(&fakeAppointmentEvidence); err != nil {
		t.Fatalf("inspect appointment evidence: %v", err)
	}
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT count(*) FROM appointment_services
		WHERE appointment_id = $1 AND (pos_service_id IS NOT NULL OR pos_service_version IS NOT NULL OR pos_staff_id IS NOT NULL OR authority_provider IS NOT NULL)
	`, created.AppointmentID).Scan(&fakeSegmentEvidence); err != nil {
		t.Fatalf("inspect reservation evidence: %v", err)
	}
	if fakeAttemptEvidence != 0 || fakeAppointmentEvidence != 0 || fakeSegmentEvidence != 0 {
		t.Fatalf("fake POS/provider evidence counts = attempt:%d appointment:%d segment:%d", fakeAttemptEvidence, fakeAppointmentEvidence, fakeSegmentEvidence)
	}

	replay, replayed, err := repository.CreateStaffOnlyAppointment(ctx, fixture.salonID, fixture.ownerID, normalized, executionTestNow.Add(time.Minute))
	if err != nil || !replayed || replay == nil || !reflect.DeepEqual(*replay, *created) {
		t.Fatalf("exact replay = %#v replay=%t error=%v", replay, replayed, err)
	}
	changedAction := action
	changedAction.AvailabilityQuoteID = uuid.NewString()
	changedAction.SlotFingerprint = hashCalendarValue("changed-slot-proof")
	changed, err := normalizeInternalCreateRequest(changedAction)
	if err != nil {
		t.Fatalf("normalize changed proof: %v", err)
	}
	if _, _, err := repository.CreateStaffOnlyAppointment(ctx, fixture.salonID, fixture.ownerID, changed, executionTestNow); !errors.Is(err, booking.ErrOperationConflict) {
		t.Fatalf("changed proof replay error = %v, want operation conflict", err)
	}
	assertExecutionResidue(t, fixture, 1, 1, 1, 1)
}

func TestExecutionRepositoryPostgresConcurrentStaffClaimHasOneAtomicWinner(t *testing.T) {
	fixture, repository := newReadyExecutionPGFixture(t)
	firstQuote := requireExecutionAvailability(t, repository, fixture, booking.StaffSelectionSpecific)
	secondQuote := requireExecutionAvailability(t, repository, fixture, booking.StaffSelectionSpecific)
	if !firstQuote.Slots[0].StartTime.Equal(secondQuote.Slots[0].StartTime) {
		t.Fatalf("pre-claim quotes do not target the same slot: %s/%s", firstQuote.Slots[0].StartTime, secondQuote.Slots[0].StartTime)
	}
	actions := []scheduling.ActionRequest{
		executionAction(fixture, firstQuote, "concurrent-claim-a", "Concurrent A"),
		executionAction(fixture, secondQuote, "concurrent-claim-b", "Concurrent B"),
	}
	type outcome struct {
		created  *InternalCreateResult
		replayed bool
		err      error
	}
	results := make(chan outcome, len(actions))
	var wait sync.WaitGroup
	for _, action := range actions {
		action := action
		wait.Add(1)
		go func() {
			defer wait.Done()
			normalized, normalizeErr := normalizeInternalCreateRequest(action)
			if normalizeErr != nil {
				results <- outcome{err: normalizeErr}
				return
			}
			created, replayed, createErr := repository.CreateStaffOnlyAppointment(context.Background(), fixture.salonID, fixture.ownerID, normalized, executionTestNow)
			results <- outcome{created: created, replayed: replayed, err: createErr}
		}()
	}
	wait.Wait()
	close(results)
	successes, stale := 0, 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			if result.created == nil || result.replayed {
				t.Fatalf("winner result = %#v replay=%t", result.created, result.replayed)
			}
		case errors.Is(result.err, booking.ErrAvailabilityQuoteStale):
			stale++
			if result.created != nil || result.replayed {
				t.Fatalf("loser leaked success evidence = %#v replay=%t", result.created, result.replayed)
			}
		default:
			t.Fatalf("unexpected concurrent error: %v", result.err)
		}
	}
	if successes != 1 || stale != 1 {
		t.Fatalf("concurrent outcomes success=%d stale=%d", successes, stale)
	}
	assertExecutionResidue(t, fixture, 1, 1, 1, 1)
}

func TestExecutionRepositoryPostgresQuoteDriftRollsBackAllExecutionEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *calendarPGFixture)
	}{
		{name: "config version", mutate: func(t *testing.T, fixture *calendarPGFixture) {
			if _, err := fixture.db.Exec(`UPDATE manleai_calendar_configs SET slot_step_minutes = 30 WHERE salon_id = $1`, fixture.salonID); err != nil {
				t.Fatalf("drift config: %v", err)
			}
		}},
		{name: "authority version", mutate: func(t *testing.T, fixture *calendarPGFixture) {
			if _, err := fixture.db.Exec(`UPDATE salon_settings SET scheduling_authority = 'external_provider' WHERE salon_id = $1`, fixture.salonID); err != nil {
				t.Fatalf("drift authority: %v", err)
			}
		}},
		{name: "staff eligibility", mutate: func(t *testing.T, fixture *calendarPGFixture) {
			if _, err := fixture.db.Exec(`UPDATE staff SET ai_bookable = false WHERE salon_id = $1 AND id = $2`, fixture.salonID, fixture.staffID); err != nil {
				t.Fatalf("drift staff: %v", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, repository := newReadyExecutionPGFixture(t)
			quote := requireExecutionAvailability(t, repository, fixture, booking.StaffSelectionSpecific)
			action := executionAction(fixture, quote, "drift-"+test.name, "Drift Caller")
			normalized, err := normalizeInternalCreateRequest(action)
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			test.mutate(t, fixture)
			if _, _, err := repository.CreateStaffOnlyAppointment(context.Background(), fixture.salonID, fixture.ownerID, normalized, executionTestNow); !errors.Is(err, booking.ErrAvailabilityQuoteStale) {
				t.Fatalf("drift create error = %v, want stale quote", err)
			}
			assertExecutionResidue(t, fixture, 0, 0, 0, 0)
			var consumed bool
			if err := fixture.db.QueryRow(`SELECT consumed_at IS NOT NULL FROM availability_quotes WHERE id = $1`, quote.QuoteID).Scan(&consumed); err != nil {
				t.Fatalf("load quote consumption: %v", err)
			}
			if consumed {
				t.Fatal("failed drift create consumed its quote")
			}
		})
	}
}

func TestExecutionRepositoryPostgresUncertainExternalOccupancyFailsClosedBeforeQuote(t *testing.T) {
	fixture, repository := newReadyExecutionPGFixture(t)
	ctx := context.Background()
	start := time.Date(2026, time.February, 9, 15, 0, 0, 0, time.UTC)
	providerID := "uncertain-" + uuid.NewString()
	var attemptID string
	if err := fixture.db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			operation_type, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, requested_start_time, requested_end_time,
			scheduling_authority, authority_provider, authority_appointment_id, authority_appointment_version
		) VALUES (
			$1,'pos_calendar_sync','confirmed','square',$2,1,
			'book','succeeded','none','not_required','External Pending','+13125550999',$3,$4,
			'external_provider','square',$2,1
		) RETURNING id::text
	`, fixture.salonID, providerID, start, start.Add(time.Hour)).Scan(&attemptID); err != nil {
		t.Fatalf("insert external attempt: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			status, customer_name, customer_phone, start_time, end_time, pos_sync_status,
			scheduling_authority, authority_provider, authority_appointment_id, authority_appointment_version
		) VALUES (
			$1,$2,'square',$3,1,'provider_pending','External Pending','+13125550999',$4,$5,'synced',
			'external_provider','square',$3,1
		)
	`, fixture.salonID, attemptID, providerID, start, start.Add(time.Hour)); err != nil {
		t.Fatalf("insert unresolved external appointment: %v", err)
	}
	result, err := repository.CheckStaffOnlyAvailability(ctx, fixture.salonID, fixture.ownerID, executionAvailabilityRequest(fixture, booking.StaffSelectionSpecific), executionTestNow)
	if !errors.Is(err, booking.ErrAvailabilityQuoteStale) || result != nil {
		t.Fatalf("uncertain external availability = %#v/%v, want fail-closed stale", result, err)
	}
	var quotes int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM availability_quotes WHERE salon_id = $1`, fixture.salonID).Scan(&quotes); err != nil {
		t.Fatalf("count quotes: %v", err)
	}
	if quotes != 0 {
		t.Fatalf("uncertain external occupancy persisted %d quotes", quotes)
	}
}

func TestExecutionRepositoryPostgresInvalidExternalTimingFailsClosedBeforeQuote(t *testing.T) {
	fixture, repository := newReadyExecutionPGFixture(t)
	ctx := context.Background()
	start := time.Date(2026, time.February, 9, 15, 0, 0, 0, time.UTC)
	providerID := "invalid-timing-" + uuid.NewString()
	var attemptID string
	if err := fixture.db.QueryRowContext(ctx, `
		INSERT INTO booking_attempts (
			salon_id, source, status, pos_provider, pos_booking_id, pos_booking_version,
			operation_type, provider_outcome, retry_policy, reconciliation_status,
			customer_name, customer_phone, requested_start_time, requested_end_time,
			scheduling_authority, authority_provider, authority_appointment_id, authority_appointment_version
		) VALUES (
			$1,'pos_calendar_sync','confirmed','square',$2,1,
			'book','succeeded','none','not_required','External Invalid Time','+13125550998',$3,$4,
			'external_provider','square',$2,1
		) RETURNING id::text
	`, fixture.salonID, providerID, start, start.Add(time.Hour)).Scan(&attemptID); err != nil {
		t.Fatalf("insert external attempt: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `
		INSERT INTO appointments (
			salon_id, booking_attempt_id, pos_provider, pos_appointment_id, pos_appointment_version,
			status, customer_name, customer_phone, staff_id, staff_selection_mode,
			start_time, end_time, pos_sync_status,
			scheduling_authority, authority_provider, authority_appointment_id, authority_appointment_version
		) VALUES (
			$1,$2,'square',$3,1,'unknown','External Invalid Time','+13125550998',$4,'specific',
			$5,$6,'synced','external_provider','square',$3,1
		)
	`, fixture.salonID, attemptID, providerID, fixture.staffID, start, start.Add(-time.Minute)); err != nil {
		t.Fatalf("insert invalid external appointment: %v", err)
	}
	result, err := repository.CheckStaffOnlyAvailability(ctx, fixture.salonID, fixture.ownerID, executionAvailabilityRequest(fixture, booking.StaffSelectionSpecific), executionTestNow)
	if !errors.Is(err, booking.ErrAvailabilityQuoteStale) || result != nil {
		t.Fatalf("invalid external timing availability = %#v/%v, want fail-closed stale", result, err)
	}
	var quotes int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM availability_quotes WHERE salon_id = $1`, fixture.salonID).Scan(&quotes); err != nil {
		t.Fatalf("count quotes: %v", err)
	}
	if quotes != 0 {
		t.Fatalf("invalid external timing persisted %d quotes", quotes)
	}
}

func TestExecutionRepositoryPostgresAggregatePartyPooledEvidenceReplayAndAtomicConflict(t *testing.T) {
	fixture, repository, poolID := newReadyAggregateExecutionPGFixture(t, 2, 4)
	ctx := context.Background()
	req := aggregateExecutionAvailabilityRequest(fixture)
	first, err := repository.CheckAvailability(ctx, fixture.salonID, fixture.ownerID, req, executionTestNow)
	if err != nil || first == nil || len(first.Slots) == 0 || len(first.Slots[0].Segments) != 2 {
		t.Fatalf("first aggregate availability = %#v/%v", first, err)
	}
	second, err := repository.CheckAvailability(ctx, fixture.salonID, fixture.ownerID, req, executionTestNow)
	if err != nil || second == nil || len(second.Slots) == 0 {
		t.Fatalf("second aggregate availability = %#v/%v", second, err)
	}
	for index, segment := range first.Slots[0].Segments {
		if segment.Quantity != 1 || segment.GuestReference != fmt.Sprintf("guest-%d", index+1) || segment.StaffID == "" ||
			segment.ScheduledStartTime.IsZero() || !segment.ScheduledEndTime.After(segment.ScheduledStartTime) ||
			len(segment.ResourceAllocations) != 1 || segment.ResourceAllocations[0].ResourcePoolID != poolID || segment.ResourceAllocations[0].UnitsAllocated != 1 {
			t.Fatalf("aggregate segment %d = %#v", index, segment)
		}
	}
	if first.Slots[0].Segments[0].StaffID == first.Slots[0].Segments[1].StaffID {
		t.Fatalf("parallel party reused staff: %#v", first.Slots[0].Segments)
	}
	action := aggregateExecutionAction(first, "aggregate-party-create")
	normalized, err := normalizeInternalCreateRequest(action)
	if err != nil {
		t.Fatalf("normalize aggregate action: %v", err)
	}
	created, replayed, err := repository.CreateAppointment(ctx, fixture.salonID, fixture.ownerID, normalized, executionTestNow)
	if err != nil || replayed || created == nil || len(created.Children) != 2 {
		t.Fatalf("aggregate create = %#v replay=%t err=%v", created, replayed, err)
	}
	replay, replayed, err := repository.CreateAppointment(ctx, fixture.salonID, fixture.ownerID, normalized, executionTestNow.Add(time.Minute))
	if err != nil || !replayed || !reflect.DeepEqual(replay, created) {
		t.Fatalf("aggregate replay = %#v replay=%t err=%v", replay, replayed, err)
	}

	var attemptSegments, appointmentSegments, quoteAllocations, attemptAllocations, appointmentAllocations int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM booking_attempt_segments WHERE booking_attempt_id=$1`, created.BookingAttemptID).Scan(&attemptSegments); err != nil {
		t.Fatalf("count attempt segments: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT count(*) FROM appointment_services WHERE appointment_id=$1`, created.AppointmentID).Scan(&appointmentSegments); err != nil {
		t.Fatalf("count appointment segments: %v", err)
	}
	if err := fixture.db.QueryRow(`
		SELECT count(*) FROM availability_quote_slot_resource_allocations allocation
		JOIN availability_quote_slot_segments segment ON segment.id=allocation.quote_slot_segment_id
		JOIN availability_quote_slots slot ON slot.id=segment.quote_slot_id WHERE slot.quote_id=$1 AND slot.slot_fingerprint=$2
	`, first.QuoteID, first.Slots[0].Fingerprint).Scan(&quoteAllocations); err != nil {
		t.Fatalf("count quote allocations: %v", err)
	}
	if err := fixture.db.QueryRow(`
		SELECT count(*) FROM booking_attempt_segment_resource_allocations allocation
		JOIN booking_attempt_segments segment ON segment.id=allocation.booking_attempt_segment_id WHERE segment.booking_attempt_id=$1
	`, created.BookingAttemptID).Scan(&attemptAllocations); err != nil {
		t.Fatalf("count attempt allocations: %v", err)
	}
	if err := fixture.db.QueryRow(`
		SELECT count(*) FROM manleai_calendar_appointment_resource_allocations allocation
		JOIN appointment_services segment ON segment.id=allocation.appointment_service_id WHERE segment.appointment_id=$1
	`, created.AppointmentID).Scan(&appointmentAllocations); err != nil {
		t.Fatalf("count appointment allocations: %v", err)
	}
	if attemptSegments != 2 || appointmentSegments != 2 || quoteAllocations != 2 || attemptAllocations != 2 || appointmentAllocations != 2 {
		t.Fatalf("exact evidence counts quote/attempt/root = %d/%d/%d children=%d/%d", quoteAllocations, attemptAllocations, appointmentAllocations, attemptSegments, appointmentSegments)
	}

	conflicting := aggregateExecutionAction(second, "aggregate-party-conflict")
	conflictingNormalized, err := normalizeInternalCreateRequest(conflicting)
	if err != nil {
		t.Fatalf("normalize conflict: %v", err)
	}
	if _, _, err := repository.CreateAppointment(ctx, fixture.salonID, fixture.ownerID, conflictingNormalized, executionTestNow); !errors.Is(err, booking.ErrAvailabilityQuoteStale) {
		t.Fatalf("conflicting aggregate error = %v, want stale", err)
	}
	assertAggregateExecutionResidue(t, fixture, 1, 2, 2)

	changed := action
	changed.Segments = append([]scheduling.ActionSegment{}, action.Segments...)
	changed.Segments[1].StaffID = fixture.staffID
	if changed.Segments[1].StaffID == action.Segments[1].StaffID {
		changed.Segments[1].StaffID = fixture.secondStaffID
	}
	changedNormalized, err := normalizeInternalCreateRequest(changed)
	if err != nil {
		t.Fatalf("normalize changed aggregate: %v", err)
	}
	if _, _, err := repository.CreateAppointment(ctx, fixture.salonID, fixture.ownerID, changedNormalized, executionTestNow); !errors.Is(err, booking.ErrOperationConflict) {
		t.Fatalf("changed aggregate replay error = %v, want conflict", err)
	}
}

func TestExecutionRepositoryPostgresWholeRootLifecycleSwitchedAuthorityHistoricalReplay(t *testing.T) {
	fixture, repository, _ := newReadyAggregateExecutionPGFixture(t, 2, 4)
	ctx := context.Background()
	lifecycleNow := time.Now().UTC()
	firstMonday := lifecycleNow.AddDate(0, 0, 8)
	for firstMonday.Weekday() != time.Monday {
		firstMonday = firstMonday.AddDate(0, 0, 1)
	}
	createAvailabilityRequest := aggregateExecutionAvailabilityRequest(fixture)
	createAvailabilityRequest.PreferredDate = firstMonday.Format("2006-01-02")
	createAvailability, err := repository.CheckAvailability(ctx, fixture.salonID, fixture.ownerID, createAvailabilityRequest, lifecycleNow)
	if err != nil || createAvailability == nil || len(createAvailability.Slots) == 0 {
		t.Fatalf("create availability = %#v/%v", createAvailability, err)
	}
	createRequest := aggregateExecutionAction(createAvailability, "lifecycle-create")
	normalizedCreate, err := normalizeInternalCreateRequest(createRequest)
	if err != nil {
		t.Fatalf("normalize create: %v", err)
	}
	created, replayed, err := repository.CreateAppointment(ctx, fixture.salonID, fixture.ownerID, normalizedCreate, lifecycleNow)
	if err != nil || replayed || created == nil || created.AuthorityAppointmentVersion != 1 ||
		created.AppointmentStatus != booking.StatusConfirmed || created.ActiveChildCount != 2 ||
		len(created.Children) != 2 {
		t.Fatalf("create = %#v replay=%t err=%v", created, replayed, err)
	}
	if _, err := fixture.db.ExecContext(ctx, `
		UPDATE salon_settings SET scheduling_authority='external_provider' WHERE salon_id=$1
	`, fixture.salonID); err != nil {
		t.Fatalf("switch current authority: %v", err)
	}

	rescheduleAvailabilityRequest := aggregateExecutionAvailabilityRequest(fixture)
	rescheduleAvailabilityRequest.TargetAppointmentID = created.AppointmentID
	rescheduleAvailabilityRequest.PreferredDate = firstMonday.AddDate(0, 0, 7).Format("2006-01-02")
	rescheduleAvailability, err := repository.CheckAvailability(
		ctx, fixture.salonID, fixture.ownerID, rescheduleAvailabilityRequest, lifecycleNow,
	)
	if err != nil || rescheduleAvailability == nil || len(rescheduleAvailability.Slots) == 0 ||
		rescheduleAvailability.TargetAuthorityAppointmentVersion != 1 {
		t.Fatalf("target-origin availability = %#v/%v", rescheduleAvailability, err)
	}
	rescheduleRequest := lifecycleRescheduleAction(rescheduleAvailability, created.AppointmentID, "lifecycle-reschedule")
	normalizedReschedule, err := normalizeInternalLifecycleRequest(rescheduleRequest)
	if err != nil {
		t.Fatalf("normalize reschedule: %v", err)
	}
	rescheduled, replayed, err := repository.RescheduleAppointment(
		ctx, fixture.salonID, fixture.ownerID, normalizedReschedule, lifecycleNow,
	)
	if err != nil || replayed || rescheduled == nil ||
		rescheduled.TargetAuthorityAppointmentVersion != 1 ||
		rescheduled.AuthorityAppointmentVersion != 2 ||
		rescheduled.AppointmentStatus != booking.StatusRescheduled ||
		rescheduled.ActiveChildCount != 2 ||
		len(rescheduled.Children) != 2 {
		t.Fatalf("reschedule = %#v replay=%t err=%v", rescheduled, replayed, err)
	}

	bookingRepository := booking.NewRepository(fixture.db)
	candidates, err := bookingRepository.ListRescheduleCandidates(ctx, fixture.salonID, fixture.ownerID, booking.RescheduleLookupRequest{
		CustomerName: "Aggregate Caller", CustomerPhone: "+1 (312) 555-0101", Limit: 5,
	})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("internal candidates after authority switch = %#v/%v", candidates, err)
	}
	candidate := candidates[0]
	if candidate.ID != created.AppointmentID ||
		candidate.SchedulingAuthority != booking.SchedulingAuthorityManleAICalendar ||
		candidate.AuthorityAppointmentVersion != 2 || candidate.PartySize != 2 ||
		candidate.POSProvider != "" || candidate.POSAppointmentID != "" ||
		len(candidate.Segments) != 2 {
		t.Fatalf("internal lifecycle candidate = %#v", candidate)
	}
	for index, segment := range candidate.Segments {
		if segment.PlanVersion != 2 || segment.Quantity != 1 || segment.GuestReference == "" ||
			segment.ScheduledStartTime.IsZero() || segment.OccupiedStartTime.IsZero() ||
			len(segment.ResourceAllocations) != 1 {
			t.Fatalf("candidate active segment %d = %#v", index, segment)
		}
	}
	wrongPhone, err := bookingRepository.ListRescheduleCandidates(ctx, fixture.salonID, fixture.ownerID, booking.RescheduleLookupRequest{
		CustomerPhone: "+13125559999", Limit: 5,
	})
	if err != nil || len(wrongPhone) != 0 {
		t.Fatalf("wrong-phone candidates = %#v/%v", wrongPhone, err)
	}
	wrongOwner, err := bookingRepository.ListRescheduleCandidates(ctx, fixture.salonID, fixture.otherOwnerID, booking.RescheduleLookupRequest{
		CustomerPhone: "+13125550101", Limit: 5,
	})
	if err != nil || len(wrongOwner) != 0 {
		t.Fatalf("cross-owner candidates = %#v/%v", wrongOwner, err)
	}
	appointments, err := bookingRepository.ListAppointments(ctx, fixture.salonID, fixture.ownerID, 10, 0)
	if err != nil || len(appointments) != 1 {
		t.Fatalf("operational appointment list = %#v/%v", appointments, err)
	}
	if appointments[0].AuthorityAppointmentVersion != 2 || appointments[0].PartySize != 2 ||
		len(appointments[0].Segments) != 2 {
		t.Fatalf("active-plan-only appointment = %#v", appointments[0])
	}
	for index, segment := range appointments[0].Segments {
		if segment.PlanVersion != 2 || segment.Quantity != 1 || segment.GuestReference == "" ||
			len(segment.ResourceAllocations) != 1 {
			t.Fatalf("operational active segment %d = %#v", index, segment)
		}
	}

	cancelRequest := InternalLifecycleRequest{}
	cancelAction := scheduling.ActionRequest{
		OperationType: scheduling.OperationKindCancel, OperationKey: "lifecycle-cancel",
		Source: booking.SourceOwnerDashboard, Notes: "customer requested cancellation",
		TargetAppointmentID:                       created.AppointmentID,
		TargetAuthority:                           booking.SchedulingAuthorityManleAICalendar,
		ExpectedTargetAuthorityAppointmentVersion: 2,
	}
	cancelRequest, err = normalizeInternalLifecycleRequest(cancelAction)
	if err != nil {
		t.Fatalf("normalize cancel: %v", err)
	}
	cancelled, replayed, err := repository.CancelAppointment(
		ctx, fixture.salonID, fixture.ownerID, cancelRequest, lifecycleNow,
	)
	if err != nil || replayed || cancelled == nil ||
		cancelled.TargetAuthorityAppointmentVersion != 2 ||
		cancelled.AuthorityAppointmentVersion != 3 ||
		cancelled.AppointmentStatus != booking.StatusCancelled ||
		cancelled.ActiveChildCount != 0 ||
		len(cancelled.Children) != 2 {
		t.Fatalf("cancel = %#v replay=%t err=%v", cancelled, replayed, err)
	}
	for index, child := range cancelled.Children {
		if child.AppointmentServiceID == "" {
			t.Fatalf("cancel historical snapshot child %d lacks durable appointment-service evidence: %#v", index, child)
		}
	}

	createReplay, replayed, err := repository.CreateAppointment(
		ctx, fixture.salonID, fixture.ownerID, normalizedCreate, lifecycleNow.Add(time.Minute),
	)
	if err != nil || !replayed || createReplay.AuthorityAppointmentVersion != 1 ||
		createReplay.AppointmentStatus != booking.StatusConfirmed || createReplay.ActiveChildCount != 2 || len(createReplay.Children) != 2 {
		t.Fatalf("historical create replay = %#v replay=%t err=%v", createReplay, replayed, err)
	}
	rescheduleReplay, replayed, err := repository.RescheduleAppointment(
		ctx, fixture.salonID, fixture.ownerID, normalizedReschedule, lifecycleNow.Add(time.Minute),
	)
	if err != nil || !replayed || rescheduleReplay.AuthorityAppointmentVersion != 2 ||
		rescheduleReplay.AppointmentStatus != booking.StatusRescheduled || rescheduleReplay.ActiveChildCount != 2 || len(rescheduleReplay.Children) != 2 {
		t.Fatalf("historical reschedule replay = %#v replay=%t err=%v", rescheduleReplay, replayed, err)
	}
	cancelReplay, replayed, err := repository.CancelAppointment(
		ctx, fixture.salonID, fixture.ownerID, cancelRequest, lifecycleNow.Add(time.Minute),
	)
	if err != nil || !replayed || cancelReplay.AuthorityAppointmentVersion != 3 ||
		cancelReplay.AppointmentStatus != booking.StatusCancelled || cancelReplay.ActiveChildCount != 0 || len(cancelReplay.Children) != 2 {
		t.Fatalf("historical cancel replay = %#v replay=%t err=%v", cancelReplay, replayed, err)
	}
	cancelledCandidates, err := bookingRepository.ListRescheduleCandidates(ctx, fixture.salonID, fixture.ownerID, booking.RescheduleLookupRequest{
		CustomerPhone: "+13125550101", Limit: 5,
	})
	if err != nil || len(cancelledCandidates) != 0 {
		t.Fatalf("cancelled candidates = %#v/%v", cancelledCandidates, err)
	}
	appointments, err = bookingRepository.ListAppointments(ctx, fixture.salonID, fixture.ownerID, 10, 0)
	if err != nil || len(appointments) != 1 || appointments[0].AuthorityAppointmentVersion != 3 ||
		appointments[0].Status != booking.StatusCancelled || len(appointments[0].Segments) != 0 {
		t.Fatalf("cancelled operational appointment = %#v/%v", appointments, err)
	}

	var rootStatus string
	var rootVersion, activePlans, releasedPlans, releaseOwners, events, fakeProviderEvidence int
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT status, authority_appointment_version
		FROM appointments WHERE salon_id=$1 AND id=$2
	`, fixture.salonID, created.AppointmentID).Scan(&rootStatus, &rootVersion); err != nil {
		t.Fatalf("load lifecycle root: %v", err)
	}
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE released_at IS NULL),
		       count(*) FILTER (WHERE released_at IS NOT NULL),
		       count(DISTINCT released_by_attempt_id) FILTER (WHERE released_at IS NOT NULL)
		FROM appointment_services WHERE salon_id=$1 AND appointment_id=$2
	`, fixture.salonID, created.AppointmentID).Scan(&activePlans, &releasedPlans, &releaseOwners); err != nil {
		t.Fatalf("load lifecycle plans: %v", err)
	}
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT count(*) FROM manleai_calendar_execution_events
		WHERE salon_id=$1 AND appointment_id=$2
	`, fixture.salonID, created.AppointmentID).Scan(&events); err != nil {
		t.Fatalf("count lifecycle events: %v", err)
	}
	if err := fixture.db.QueryRowContext(ctx, `
		SELECT count(*) FROM booking_attempts
		WHERE salon_id=$1 AND authority_appointment_id=$2::uuid::text
		  AND (
			pos_provider IS NOT NULL OR pos_booking_id IS NOT NULL OR pos_booking_version IS NOT NULL
			OR target_pos_booking_version IS NOT NULL OR pos_idempotency_key IS NOT NULL
			OR provider_location_id IS NOT NULL OR provider_snapshot_generation IS NOT NULL
			OR authority_provider IS NOT NULL OR authority_location_id IS NOT NULL
			OR authority_snapshot_generation IS NOT NULL
		  )
	`, fixture.salonID, created.AppointmentID).Scan(&fakeProviderEvidence); err != nil {
		t.Fatalf("inspect lifecycle provider evidence: %v", err)
	}
	if rootStatus != booking.StatusCancelled || rootVersion != 3 || activePlans != 0 ||
		releasedPlans != 4 || releaseOwners != 2 || events != 3 || fakeProviderEvidence != 0 {
		t.Fatalf(
			"lifecycle graph status/version/active/released/owners/events/provider=%s/%d/%d/%d/%d/%d/%d",
			rootStatus, rootVersion, activePlans, releasedPlans, releaseOwners, events, fakeProviderEvidence,
		)
	}
}

func TestExecutionRepositoryPostgresRejectsMixedGuestReferencesBeforeQuoteWriteAtSinglePartyLimit(t *testing.T) {
	fixture, repository, _ := newReadyAggregateExecutionPGFixture(t, 1, 1)
	request := booking.AvailabilityRequest{
		PartySize: 1, PreferredDate: "2026-02-09", Limit: 10,
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: fixture.serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-a", Quantity: 1},
			{ServiceID: fixture.serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, Quantity: 1},
		},
	}
	result, err := repository.CheckAvailability(context.Background(), fixture.salonID, fixture.ownerID, request, executionTestNow)
	if !errors.Is(err, booking.ErrValidation) || result != nil {
		t.Fatalf("mixed guest availability = %#v/%v, want validation without result", result, err)
	}
	var quotes int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM availability_quotes WHERE salon_id=$1`, fixture.salonID).Scan(&quotes); err != nil {
		t.Fatalf("count quotes: %v", err)
	}
	if quotes != 0 {
		t.Fatalf("mixed guest availability persisted %d quotes", quotes)
	}
}

func newReadyAggregateExecutionPGFixture(t *testing.T, capacity int, maxPartySize int) (*calendarPGFixture, *Repository, string) {
	t.Helper()
	fixture := newCalendarPGFixture(t)
	service := NewService(NewRepository(fixture.db))
	ctx := context.Background()
	config := validConfigRequest("aggregate-config-"+uuid.NewString(), 0)
	config.MinimumBookingNoticeMinutes = 0
	config.BookingHorizonDays = 30
	config.DefaultBufferBeforeMinutes = 0
	config.DefaultBufferAfterMinutes = 0
	config.MaxPartySize = maxPartySize
	response, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, config)
	if err != nil {
		t.Fatalf("create aggregate config: %v", err)
	}
	response, err = service.PutHours(ctx, fixture.salonID, fixture.ownerID, ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "aggregate-hours-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: int(time.Monday), StartMinute: 9 * 60, EndMinute: 17 * 60}},
	})
	if err != nil {
		t.Fatalf("create aggregate hours: %v", err)
	}
	for _, staffID := range []string{fixture.staffID, fixture.secondStaffID} {
		response, err = service.PutStaffProfile(ctx, fixture.salonID, fixture.ownerID, staffID, StaffProfileInput{
			MutationMeta:       MutationMeta{ActionKey: "aggregate-staff-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
			WeeklyPeriods:      []WeeklyPeriodInput{{DayOfWeek: int(time.Monday), StartMinute: 9 * 60, EndMinute: 17 * 60}},
			EligibleServiceIDs: []string{fixture.serviceID},
		})
		if err != nil {
			t.Fatalf("create aggregate staff profile: %v", err)
		}
	}
	resourceName := "aggregate chairs " + uuid.NewString()
	response, err = service.CreateResource(ctx, fixture.salonID, fixture.ownerID, ResourcePoolInput{
		MutationMeta: MutationMeta{ActionKey: "aggregate-resource-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Name:         resourceName, Capacity: capacity,
	})
	if err != nil {
		t.Fatalf("create aggregate resource: %v", err)
	}
	poolID := ""
	for _, resource := range response.ManleaiCalendar.Resources {
		if resource.Name == resourceName {
			poolID = resource.ID
		}
	}
	if poolID == "" {
		t.Fatal("created resource missing")
	}
	pooled := CapacityModePooled
	response, err = service.PutServicePolicy(ctx, fixture.salonID, fixture.ownerID, fixture.serviceID, ServicePolicyInput{
		MutationMeta:         MutationMeta{ActionKey: "aggregate-policy-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Enabled:              true,
		CapacityMode:         &pooled,
		EligibleStaffIDs:     []string{fixture.staffID, fixture.secondStaffID},
		ResourceRequirements: []ResourceRequirementInput{{ResourcePoolID: poolID, UnitsRequired: 1}},
	})
	if err != nil {
		t.Fatalf("create aggregate policy: %v", err)
	}
	response, err = service.Activate(ctx, fixture.salonID, fixture.ownerID, MutationMeta{
		ActionKey: "aggregate-activate-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion,
	})
	if err != nil {
		t.Fatalf("activate aggregate config: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='manleai_calendar' WHERE salon_id=$1`, fixture.salonID); err != nil {
		t.Fatalf("select aggregate authority: %v", err)
	}
	return fixture, NewRepository(fixture.db), poolID
}

func aggregateExecutionAvailabilityRequest(fixture *calendarPGFixture) booking.AvailabilityRequest {
	return booking.AvailabilityRequest{
		PartySize: 2, PreferredDate: "2026-02-09", Limit: 10,
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: fixture.serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-1", Quantity: 1},
			{ServiceID: fixture.serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "guest-2", Quantity: 1},
		},
	}
}

func aggregateExecutionAction(availability *booking.AvailabilityResult, operationKey string) scheduling.ActionRequest {
	slot := availability.Slots[0]
	segments := make([]scheduling.ActionSegment, 0, len(slot.Segments))
	for _, segment := range slot.Segments {
		segments = append(segments, scheduling.ActionSegment{
			ServiceID: segment.ServiceID, StaffID: segment.StaffID, StaffSelectionMode: segment.StaffSelectionMode,
			GuestReference: segment.GuestReference, Quantity: segment.Quantity,
			RequestedStartTime: segment.ScheduledStartTime, RequestedEndTime: segment.ScheduledEndTime,
		})
	}
	return scheduling.ActionRequest{
		OperationType: scheduling.OperationKindBook, OperationKey: operationKey,
		AvailabilityQuoteID: availability.QuoteID, SlotFingerprint: slot.Fingerprint,
		Source: booking.SourceOwnerDashboard, CustomerName: "Aggregate Caller", CustomerPhone: "+13125550101",
		RequestedTimezone: availability.Timezone, PartySize: 2, RequestedStartTime: slot.StartTime, RequestedEndTime: slot.EndTime,
		Segments: segments,
	}
}

func lifecycleRescheduleAction(availability *booking.AvailabilityResult, appointmentID string, operationKey string) scheduling.ActionRequest {
	action := aggregateExecutionAction(availability, operationKey)
	action.OperationType = scheduling.OperationKindReschedule
	action.TargetAppointmentID = appointmentID
	action.TargetAuthority = booking.SchedulingAuthorityManleAICalendar
	action.ExpectedTargetAuthorityAppointmentVersion = availability.TargetAuthorityAppointmentVersion
	return action
}

func assertAggregateExecutionResidue(t *testing.T, fixture *calendarPGFixture, roots int, children int, allocations int) {
	t.Helper()
	var gotRoots, gotChildren, gotAllocations int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM appointments WHERE salon_id=$1 AND scheduling_authority='manleai_calendar'`, fixture.salonID).Scan(&gotRoots); err != nil {
		t.Fatalf("count roots: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT count(*) FROM appointment_services WHERE salon_id=$1 AND scheduling_authority='manleai_calendar'`, fixture.salonID).Scan(&gotChildren); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if err := fixture.db.QueryRow(`SELECT count(*) FROM manleai_calendar_appointment_resource_allocations WHERE salon_id=$1`, fixture.salonID).Scan(&gotAllocations); err != nil {
		t.Fatalf("count allocations: %v", err)
	}
	if gotRoots != roots || gotChildren != children || gotAllocations != allocations {
		t.Fatalf("aggregate residue roots/children/allocations=%d/%d/%d want %d/%d/%d", gotRoots, gotChildren, gotAllocations, roots, children, allocations)
	}
}

func newReadyExecutionPGFixture(t *testing.T) (*calendarPGFixture, *Repository) {
	t.Helper()
	fixture := newCalendarPGFixture(t)
	service := NewService(NewRepository(fixture.db))
	ctx := context.Background()
	configRequest := validConfigRequest("execution-config-"+uuid.NewString(), 0)
	configRequest.MinimumBookingNoticeMinutes = 0
	configRequest.BookingHorizonDays = 30
	configRequest.DefaultBufferBeforeMinutes = 0
	configRequest.DefaultBufferAfterMinutes = 0
	response, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, configRequest)
	if err != nil {
		t.Fatalf("create execution config: %v", err)
	}
	response, err = service.PutHours(ctx, fixture.salonID, fixture.ownerID, ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "execution-hours-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: int(time.Monday), StartMinute: 9 * 60, EndMinute: 17 * 60}},
	})
	if err != nil {
		t.Fatalf("create execution hours: %v", err)
	}
	response, err = service.PutStaffProfile(ctx, fixture.salonID, fixture.ownerID, fixture.staffID, StaffProfileInput{
		MutationMeta:       MutationMeta{ActionKey: "execution-staff-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		WeeklyPeriods:      []WeeklyPeriodInput{{DayOfWeek: int(time.Monday), StartMinute: 9 * 60, EndMinute: 17 * 60}},
		EligibleServiceIDs: []string{fixture.serviceID},
	})
	if err != nil {
		t.Fatalf("create execution staff schedule: %v", err)
	}
	staffOnly := CapacityModeStaffOnly
	response, err = service.PutServicePolicy(ctx, fixture.salonID, fixture.ownerID, fixture.serviceID, ServicePolicyInput{
		MutationMeta:     MutationMeta{ActionKey: "execution-policy-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Enabled:          true,
		CapacityMode:     &staffOnly,
		EligibleStaffIDs: []string{fixture.staffID},
	})
	if err != nil {
		t.Fatalf("create execution policy: %v", err)
	}
	response, err = service.Activate(ctx, fixture.salonID, fixture.ownerID, MutationMeta{
		ActionKey: "execution-activate-" + uuid.NewString(), ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion,
	})
	if err != nil {
		t.Fatalf("activate execution config: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority = 'manleai_calendar' WHERE salon_id = $1`, fixture.salonID); err != nil {
		t.Fatalf("select internal authority: %v", err)
	}
	return fixture, NewRepository(fixture.db)
}

func requireExecutionAvailability(t *testing.T, repository *Repository, fixture *calendarPGFixture, mode string) *booking.AvailabilityResult {
	t.Helper()
	result, err := repository.CheckStaffOnlyAvailability(context.Background(), fixture.salonID, fixture.ownerID, executionAvailabilityRequest(fixture, mode), executionTestNow)
	if err != nil {
		t.Fatalf("check availability: %v", err)
	}
	if result == nil || result.QuoteID == "" || result.RequestFingerprint == "" || result.ExpiresAt == nil || len(result.Slots) == 0 {
		t.Fatalf("availability result = %#v", result)
	}
	return result
}

func executionAvailabilityRequest(fixture *calendarPGFixture, mode string) booking.AvailabilityRequest {
	request := booking.AvailabilityRequest{
		ServiceID: fixture.serviceID, StaffSelectionMode: mode, PreferredDate: "2026-02-09", Limit: 50,
	}
	if mode == booking.StaffSelectionSpecific {
		request.StaffID = fixture.staffID
	}
	return request
}

func executionAction(fixture *calendarPGFixture, availability *booking.AvailabilityResult, operationKey string, customerName string) scheduling.ActionRequest {
	slot := availability.Slots[0]
	segment := slot.Segments[0]
	return scheduling.ActionRequest{
		OperationType: scheduling.OperationKindBook, OperationKey: operationKey,
		AvailabilityQuoteID: availability.QuoteID, SlotFingerprint: slot.Fingerprint,
		Source: booking.SourceOwnerDashboard, CustomerName: customerName,
		CustomerPhone: "+13125550101", RequestedTimezone: availability.Timezone, PartySize: 1,
		RequestedStartTime: slot.StartTime, RequestedEndTime: slot.EndTime,
		Segments: []scheduling.ActionSegment{{
			ServiceID: segment.ServiceID, StaffID: segment.StaffID, StaffSelectionMode: segment.StaffSelectionMode,
			Quantity: 1, RequestedStartTime: slot.StartTime, RequestedEndTime: slot.EndTime,
		}},
	}
}

func assertExecutionResidue(t *testing.T, fixture *calendarPGFixture, attempts int, appointments int, events int, consumedQuotes int) {
	t.Helper()
	queries := []struct {
		name string
		sql  string
		want int
	}{
		{name: "attempts", sql: `SELECT count(*) FROM booking_attempts WHERE salon_id = $1 AND scheduling_authority = 'manleai_calendar'`, want: attempts},
		{name: "appointments", sql: `SELECT count(*) FROM appointments WHERE salon_id = $1 AND scheduling_authority = 'manleai_calendar'`, want: appointments},
		{name: "events", sql: `SELECT count(*) FROM manleai_calendar_execution_events WHERE salon_id = $1`, want: events},
		{name: "consumed quotes", sql: `SELECT count(*) FROM availability_quotes WHERE salon_id = $1 AND consumed_by_attempt_id IS NOT NULL`, want: consumedQuotes},
	}
	for _, query := range queries {
		var got int
		if err := fixture.db.QueryRow(query.sql, fixture.salonID).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", query.name, err)
		}
		if got != query.want {
			t.Fatalf("%s residue = %d, want %d", query.name, got, query.want)
		}
	}
	var orphanReservations int
	if err := fixture.db.QueryRow(`
		SELECT count(*) FROM appointment_services segment
		WHERE segment.salon_id = $1 AND segment.scheduling_authority = 'manleai_calendar'
	`, fixture.salonID).Scan(&orphanReservations); err != nil {
		t.Fatalf("count reservations: %v", err)
	}
	if orphanReservations != appointments {
		t.Fatalf("reservation residue = %d, want %d", orphanReservations, appointments)
	}
}
