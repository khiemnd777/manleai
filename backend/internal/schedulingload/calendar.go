package schedulingload

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/manleai/ai-receptionist/modules/booking"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	calendar "github.com/manleai/ai-receptionist/modules/scheduling_manleai_calendar"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

func runCalendarWorkload(
	ctx context.Context,
	db *sql.DB,
	config Config,
	seed seededRun,
	violations *InvariantViolations,
) ([]WorkloadReport, error) {
	repository := calendar.NewRepository(db)
	configuration := calendar.NewService(repository)
	if err := configureCalendar(ctx, db, configuration, config, seed); err != nil {
		return nil, err
	}
	executor := calendar.NewExecutor(repository, fixedClock{now: config.BaseTime})
	availabilityRequest := calendarAvailabilityRequest(config.workloadTime(), seed.Calendar.ServiceID)
	availability, err := executor.CheckAvailability(ctx, seed.Calendar.SalonID, seed.OwnerID, availabilityRequest)
	if err != nil || availability == nil || availability.VerifiedSlots == nil || len(availability.VerifiedSlots.Slots) == 0 {
		return nil, fmt.Errorf("create calendar idempotency quote: %w", err)
	}
	idempotentAction := calendarAction(availability.VerifiedSlots, "load-calendar-create-"+config.RunID, config)

	var appointmentID string
	var appointmentMutex sync.Mutex
	started := time.Now()
	createSamples := runConcurrent(ctx, config.OperationsPerWorkload, config.Concurrency, func(callCtx context.Context, _ int) sampleResult {
		result, callErr := executor.ExecuteAction(callCtx, seed.Calendar.SalonID, seed.OwnerID, idempotentAction)
		if callErr != nil || result == nil || result.ConfirmedAppointment == nil {
			return sampleResult{UnexpectedError: true}
		}
		appointmentMutex.Lock()
		if appointmentID == "" {
			appointmentID = result.ConfirmedAppointment.AppointmentID
		} else if appointmentID != result.ConfirmedAppointment.AppointmentID {
			violations.Idempotency++
		}
		appointmentMutex.Unlock()
		return sampleResult{Success: true, Replayed: result.Replayed}
	})
	reports := []WorkloadReport{summarizeWorkload("manleai_calendar_party_create_replay", started, createSamples)}
	if appointmentID == "" {
		return reports, errors.New("calendar workload did not create an appointment")
	}

	if _, err := executor.CheckAvailability(ctx, seed.Calendar.SalonID, seed.OtherOwnerID, availabilityRequest); err == nil {
		violations.Tenant++
	}

	conflictActions := make([]scheduling.ActionRequest, 0, config.OperationsPerWorkload)
	for index := 0; index < config.OperationsPerWorkload; index++ {
		quoted, quoteErr := executor.CheckAvailability(ctx, seed.Calendar.SalonID, seed.OwnerID, availabilityRequest)
		if quoteErr != nil || quoted == nil || quoted.VerifiedSlots == nil || len(quoted.VerifiedSlots.Slots) == 0 {
			return reports, fmt.Errorf("create calendar conflict quote %d: %w", index, quoteErr)
		}
		conflictActions = append(conflictActions, calendarAction(
			quoted.VerifiedSlots,
			fmt.Sprintf("load-calendar-conflict-%s-%d", config.RunID, index),
			config,
		))
	}
	conflictStarted := time.Now()
	conflictSamples := runConcurrent(ctx, len(conflictActions), config.Concurrency, func(callCtx context.Context, index int) sampleResult {
		result, callErr := executor.ExecuteAction(callCtx, seed.Calendar.SalonID, seed.OwnerID, conflictActions[index])
		if callErr == nil && result != nil && result.ConfirmedAppointment != nil {
			return sampleResult{Success: true, Replayed: result.Replayed}
		}
		if errors.Is(callErr, booking.ErrAvailabilityQuoteStale) || errors.Is(callErr, booking.ErrOperationConflict) {
			return sampleResult{ExpectedConflict: true}
		}
		return sampleResult{UnexpectedError: true}
	})
	reports = append(reports, summarizeWorkload("manleai_calendar_resource_conflict", conflictStarted, conflictSamples))

	if err := inspectCalendarInvariants(ctx, db, seed.Calendar.SalonID, violations); err != nil {
		return reports, err
	}
	return reports, nil
}

func configureCalendar(ctx context.Context, db *sql.DB, service *calendar.Service, config Config, seed seededRun) error {
	response, err := service.PutConfig(ctx, seed.Calendar.SalonID, seed.OwnerID, calendar.CalendarConfigInput{
		MutationMeta:                calendar.MutationMeta{ActionKey: "load-calendar-config-" + config.RunID, ExpectedConfigVersion: 0},
		SlotStepMinutes:             15,
		MinimumBookingNoticeMinutes: 0,
		BookingHorizonDays:          30,
		MaxPartySize:                4,
		DefaultBufferBeforeMinutes:  0,
		DefaultBufferAfterMinutes:   0,
	})
	if err != nil {
		return fmt.Errorf("configure calendar root: %w", err)
	}
	version, err := calendarConfigVersion(response)
	if err != nil {
		return err
	}
	response, err = service.PutHours(ctx, seed.Calendar.SalonID, seed.OwnerID, calendar.ReplaceBusinessHoursInput{
		MutationMeta: calendar.MutationMeta{ActionKey: "load-calendar-hours-" + config.RunID, ExpectedConfigVersion: version},
		Periods:      []calendar.BusinessHourPeriodInput{{DayOfWeek: int(time.Monday), StartMinute: 9 * 60, EndMinute: 17 * 60}},
	})
	if err != nil {
		return fmt.Errorf("configure calendar hours: %w", err)
	}
	version, _ = calendarConfigVersion(response)
	for index, staffID := range []string{seed.Calendar.StaffID, seed.Calendar.SecondStaffID} {
		response, err = service.PutStaffProfile(ctx, seed.Calendar.SalonID, seed.OwnerID, staffID, calendar.StaffProfileInput{
			MutationMeta:       calendar.MutationMeta{ActionKey: fmt.Sprintf("load-calendar-staff-%s-%d", config.RunID, index), ExpectedConfigVersion: version},
			WeeklyPeriods:      []calendar.WeeklyPeriodInput{{DayOfWeek: int(time.Monday), StartMinute: 9 * 60, EndMinute: 17 * 60}},
			EligibleServiceIDs: []string{seed.Calendar.ServiceID},
		})
		if err != nil {
			return fmt.Errorf("configure calendar staff %d: %w", index, err)
		}
		version, _ = calendarConfigVersion(response)
	}
	resourceName := "Synthetic pooled resource " + config.RunID
	response, err = service.CreateResource(ctx, seed.Calendar.SalonID, seed.OwnerID, calendar.ResourcePoolInput{
		MutationMeta: calendar.MutationMeta{ActionKey: "load-calendar-resource-" + config.RunID, ExpectedConfigVersion: version},
		Name:         resourceName,
		Capacity:     2,
	})
	if err != nil {
		return fmt.Errorf("configure calendar resource: %w", err)
	}
	version, _ = calendarConfigVersion(response)
	poolID := ""
	for _, resource := range response.ManleaiCalendar.Resources {
		if resource.Name == resourceName {
			poolID = resource.ID
			break
		}
	}
	if poolID == "" {
		return errors.New("configured calendar resource was not returned")
	}
	pooled := calendar.CapacityModePooled
	response, err = service.PutServicePolicy(ctx, seed.Calendar.SalonID, seed.OwnerID, seed.Calendar.ServiceID, calendar.ServicePolicyInput{
		MutationMeta:         calendar.MutationMeta{ActionKey: "load-calendar-policy-" + config.RunID, ExpectedConfigVersion: version},
		Enabled:              true,
		CapacityMode:         &pooled,
		EligibleStaffIDs:     []string{seed.Calendar.StaffID, seed.Calendar.SecondStaffID},
		ResourceRequirements: []calendar.ResourceRequirementInput{{ResourcePoolID: poolID, UnitsRequired: 1}},
	})
	if err != nil {
		return fmt.Errorf("configure calendar policy: %w", err)
	}
	version, _ = calendarConfigVersion(response)
	response, err = service.Activate(ctx, seed.Calendar.SalonID, seed.OwnerID, calendar.MutationMeta{
		ActionKey: "load-calendar-activate-" + config.RunID, ExpectedConfigVersion: version,
	})
	if err != nil {
		return fmt.Errorf("activate calendar: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='manleai_calendar' WHERE salon_id::text=$1`, seed.Calendar.SalonID); err != nil {
		return fmt.Errorf("select calendar authority for synthetic salon: %w", err)
	}
	selected, err := service.GetAggregate(ctx, seed.Calendar.SalonID, seed.OwnerID)
	if err != nil {
		return fmt.Errorf("read selected calendar aggregate: %w", err)
	}
	if selected == nil || selected.ManleaiCalendar == nil || !selected.ManleaiCalendar.Readiness.Capabilities.PartyCreate || !selected.ManleaiCalendar.Readiness.Capabilities.PooledCapacity {
		return errors.New("calendar activation did not expose required party/pooled capabilities")
	}
	return nil
}

func calendarConfigVersion(response *calendar.MutationResponse) (int64, error) {
	if response == nil || response.ManleaiCalendar == nil {
		return 0, errors.New("calendar mutation returned no aggregate")
	}
	return response.ManleaiCalendar.ConfigVersion, nil
}

func calendarAvailabilityRequest(baseTime time.Time, serviceID string) booking.AvailabilityRequest {
	location, _ := time.LoadLocation("America/Chicago")
	return booking.AvailabilityRequest{
		PartySize:     2,
		PreferredDate: baseTime.In(location).Format("2006-01-02"),
		Limit:         10,
		Segments: []booking.BookingSegmentRequest{
			{ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "synthetic-guest-1", Quantity: 1},
			{ServiceID: serviceID, StaffSelectionMode: booking.StaffSelectionAnyone, GuestReference: "synthetic-guest-2", Quantity: 1},
		},
	}
}

func calendarAction(availability *booking.AvailabilityResult, operationKey string, config Config) scheduling.ActionRequest {
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
		Source: booking.SourceOwnerDashboard, CustomerName: "Synthetic Customer",
		CustomerPhone:     syntheticPhone(config.RunID, operationKey),
		CustomerEmail:     syntheticEmail(config.RunID, "calendar-customer"),
		RequestedTimezone: availability.Timezone, PartySize: 2,
		RequestedStartTime: slot.StartTime, RequestedEndTime: slot.EndTime,
		Segments: segments,
	}
}

func inspectCalendarInvariants(ctx context.Context, db *sql.DB, salonID string, violations *InvariantViolations) error {
	var roots, attempts, children, allocations, events int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM appointments WHERE salon_id::text=$1 AND scheduling_authority='manleai_calendar'`, salonID).Scan(&roots); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM booking_attempts WHERE salon_id::text=$1 AND scheduling_authority='manleai_calendar'`, salonID).Scan(&attempts); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM appointment_services WHERE salon_id::text=$1 AND scheduling_authority='manleai_calendar'`, salonID).Scan(&children); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_appointment_resource_allocations WHERE salon_id::text=$1`, salonID).Scan(&allocations); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM manleai_calendar_execution_events WHERE salon_id::text=$1 AND event_type='appointment_confirmed'`, salonID).Scan(&events); err != nil {
		return err
	}
	if roots != 2 || attempts != 2 {
		violations.Duplicates += absoluteDifference(roots, 2) + absoluteDifference(attempts, 2)
	}
	if children != roots*2 || allocations != children || events != roots {
		violations.Orphans += absoluteDifference(children, roots*2) + absoluteDifference(allocations, children) + absoluteDifference(events, roots)
	}
	var duplicateOperationGroups, orphanChildren, orphanAllocations, providerEvidence int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM (
			SELECT operation_key FROM booking_attempts WHERE salon_id::text=$1 GROUP BY operation_key HAVING count(*) > 1
		) duplicate
	`, salonID).Scan(&duplicateOperationGroups); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM appointment_services child
		LEFT JOIN appointments root ON root.id=child.appointment_id AND root.salon_id=child.salon_id
		WHERE child.salon_id::text=$1 AND root.id IS NULL
	`, salonID).Scan(&orphanChildren); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM manleai_calendar_appointment_resource_allocations allocation
		LEFT JOIN appointment_services child ON child.id=allocation.appointment_service_id AND child.salon_id=allocation.salon_id
		WHERE allocation.salon_id::text=$1 AND child.id IS NULL
	`, salonID).Scan(&orphanAllocations); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT
			(SELECT count(*) FROM booking_attempts WHERE salon_id::text=$1 AND scheduling_authority='manleai_calendar' AND (pos_provider IS NOT NULL OR pos_booking_id IS NOT NULL OR authority_provider IS NOT NULL))
			+
			(SELECT count(*) FROM appointments WHERE salon_id::text=$1 AND scheduling_authority='manleai_calendar' AND (pos_provider IS NOT NULL OR authority_provider IS NOT NULL))
	`, salonID).Scan(&providerEvidence); err != nil {
		return err
	}
	violations.Duplicates += duplicateOperationGroups
	violations.Orphans += orphanChildren + orphanAllocations
	violations.ProviderEvidence += providerEvidence
	return nil
}
