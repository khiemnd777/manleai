package scheduling_manleai_calendar

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type calendarPGFixture struct {
	db            *sql.DB
	ownerID       string
	otherOwnerID  string
	salonID       string
	serviceID     string
	staffID       string
	secondStaffID string
}

func TestRepositoryPostgresOwnerReplayConflictCASAndConcurrency(t *testing.T) {
	fixture := newCalendarPGFixture(t)
	service := NewService(NewRepository(fixture.db))
	ctx := context.Background()

	create := validConfigRequest("config-create", 0)
	created, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, create)
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	if created.Replayed || created.ManleaiCalendar.Config == nil || created.ManleaiCalendar.ConfigVersion != 1 {
		t.Fatalf("created response = %#v", created)
	}
	if created.ManleaiCalendar.Hours == nil || created.ManleaiCalendar.StaffProfiles == nil || created.ManleaiCalendar.ServicePolicies == nil || created.ManleaiCalendar.Resources == nil || created.ManleaiCalendar.Exceptions == nil || created.ManleaiCalendar.Readiness.Blockers == nil {
		t.Fatalf("aggregate collections must be non-nil: %#v", created.ManleaiCalendar)
	}
	if _, err := service.GetAggregate(ctx, fixture.salonID, fixture.otherOwnerID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant aggregate error = %v, want ErrNotFound", err)
	}

	replay, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, create)
	if err != nil || !replay.Replayed || replay.ManleaiCalendar.ConfigVersion != 1 {
		t.Fatalf("exact replay = %#v/%v", replay, err)
	}
	changedReuse := create
	changedReuse.SlotStepMinutes = 30
	if _, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, changedReuse); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed fingerprint error = %v, want ErrActionConflict", err)
	}
	stale := validConfigRequest("stale-new-action", 0)
	stale.SlotStepMinutes = 30
	if _, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale CAS error = %v, want ErrVersionConflict", err)
	}

	expected := created.ManleaiCalendar.ConfigVersion
	requests := []CalendarConfigInput{validConfigRequest("concurrent-a", expected), validConfigRequest("concurrent-b", expected)}
	requests[0].SlotStepMinutes = 30
	requests[1].SlotStepMinutes = 20
	type outcome struct {
		response *MutationResponse
		err      error
	}
	results := make(chan outcome, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		request := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, callErr := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, request)
			results <- outcome{response: response, err: callErr}
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	conflicts := 0
	for result := range results {
		switch {
		case result.err == nil:
			successes++
			if result.response == nil || result.response.Replayed {
				t.Fatalf("concurrent success response = %#v", result.response)
			}
		case errors.Is(result.err, ErrVersionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent result error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes success=%d conflict=%d", successes, conflicts)
	}
	var eventCount int
	if err := fixture.db.QueryRow(`SELECT count(*) FROM manleai_calendar_config_events WHERE salon_id = $1`, fixture.salonID).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("event count = %d, want create plus one concurrent winner", eventCount)
	}
	current, err := service.GetAggregate(ctx, fixture.salonID, fixture.ownerID)
	if err != nil {
		t.Fatalf("load after concurrent update: %v", err)
	}
	beforeEmptyReplace := current.ManleaiCalendar.ConfigVersion
	emptyReplace, err := service.PutHours(ctx, fixture.salonID, fixture.ownerID, ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "empty-hours-replace", ExpectedConfigVersion: beforeEmptyReplace},
		Periods:      []BusinessHourPeriodInput{},
	})
	if err != nil {
		t.Fatalf("empty replacement: %v", err)
	}
	if emptyReplace.ManleaiCalendar.ConfigVersion <= beforeEmptyReplace {
		t.Fatalf("empty replacement version = %d, want > %d", emptyReplace.ManleaiCalendar.ConfigVersion, beforeEmptyReplace)
	}
	if err := fixture.db.QueryRow(`SELECT count(*) FROM manleai_calendar_config_events WHERE salon_id = $1`, fixture.salonID).Scan(&eventCount); err != nil {
		t.Fatalf("count events after empty replacement: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("event count after empty replacement = %d, want 3", eventCount)
	}
}

func TestRepositoryPostgresReadinessActivationRecoveryAndArchiveIntegrity(t *testing.T) {
	fixture := newCalendarPGFixture(t)
	service := NewService(NewRepository(fixture.db))
	ctx := context.Background()

	response, err := service.PutConfig(ctx, fixture.salonID, fixture.ownerID, validConfigRequest("config", 0))
	if err != nil {
		t.Fatalf("create config: %v", err)
	}
	response, err = service.PutHours(ctx, fixture.salonID, fixture.ownerID, ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "hours", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: 1, StartMinute: 540, EndMinute: 1020}},
	})
	if err != nil {
		t.Fatalf("put hours: %v", err)
	}
	response, err = service.PutStaffProfile(ctx, fixture.salonID, fixture.ownerID, fixture.staffID, StaffProfileInput{
		MutationMeta:       MutationMeta{ActionKey: "staff-first", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		WeeklyPeriods:      []WeeklyPeriodInput{{DayOfWeek: 1, StartMinute: 540, EndMinute: 1020}},
		EligibleServiceIDs: []string{fixture.serviceID},
	})
	if err != nil {
		t.Fatalf("staff-first profile: %v", err)
	}
	policy := findPolicy(t, response.ManleaiCalendar, fixture.serviceID)
	if policy.Configured {
		t.Fatal("staff-first eligibility created an implicit service policy")
	}
	assertBlocker(t, response.ManleaiCalendar.Readiness, BlockerServicePolicyRequired, ReadinessDimensionConfiguration)

	staffOnly := CapacityModeStaffOnly
	response, err = service.PutServicePolicy(ctx, fixture.salonID, fixture.ownerID, fixture.serviceID, ServicePolicyInput{
		MutationMeta:     MutationMeta{ActionKey: "service-policy", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Enabled:          true,
		CapacityMode:     &staffOnly,
		EligibleStaffIDs: []string{fixture.staffID},
	})
	if err != nil {
		t.Fatalf("put service policy: %v", err)
	}
	if !response.ManleaiCalendar.Readiness.ConfigurationReady {
		t.Fatalf("configured calendar blockers = %#v", response.ManleaiCalendar.Readiness.Blockers)
	}

	activationRequest := MutationMeta{ActionKey: "activate-v1", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion}
	response, err = service.Activate(ctx, fixture.salonID, fixture.ownerID, activationRequest)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	assertCurrentActivation(t, response.ManleaiCalendar.Config)
	activatedVersion := response.ManleaiCalendar.ConfigVersion

	response, err = service.PutHours(ctx, fixture.salonID, fixture.ownerID, ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "hours-after-activation", ExpectedConfigVersion: activatedVersion},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: 2, StartMinute: 600, EndMinute: 1080}},
	})
	if err != nil {
		t.Fatalf("mutate activated config: %v", err)
	}
	if !response.ManleaiCalendar.Readiness.ConfigurationReady {
		t.Fatalf("ordinary mutation made config incomplete: %#v", response.ManleaiCalendar.Readiness.Blockers)
	}
	assertBlocker(t, response.ManleaiCalendar.Readiness, BlockerConfigNotActivated, ReadinessDimensionExecution)
	if response.ManleaiCalendar.Config.ActivatedVersion == nil || *response.ManleaiCalendar.Config.ActivatedVersion == response.ManleaiCalendar.Config.Version {
		t.Fatalf("activation fence did not become stale: %#v", response.ManleaiCalendar.Config)
	}

	reactivate := MutationMeta{ActionKey: "reactivate-v2", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion}
	response, err = service.Activate(ctx, fixture.salonID, fixture.ownerID, reactivate)
	if err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	assertCurrentActivation(t, response.ManleaiCalendar.Config)
	replayed, err := service.Activate(ctx, fixture.salonID, fixture.ownerID, reactivate)
	if err != nil || !replayed.Replayed {
		t.Fatalf("reactivation replay = %#v/%v", replayed, err)
	}
	changedReplay := reactivate
	changedReplay.ExpectedConfigVersion++
	if _, err := service.Activate(ctx, fixture.salonID, fixture.ownerID, changedReplay); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("reactivation changed fingerprint = %v, want ErrActionConflict", err)
	}

	if _, err := fixture.db.Exec(`UPDATE staff SET archived_at = now(), active = false, ai_bookable = false WHERE id = $1`, fixture.staffID); err != nil {
		t.Fatalf("archive assigned staff: %v", err)
	}
	aggregateResponse, err := service.GetAggregate(ctx, fixture.salonID, fixture.ownerID)
	if err != nil {
		t.Fatalf("load invalidated aggregate: %v", err)
	}
	assertBlocker(t, aggregateResponse.ManleaiCalendar.Readiness, BlockerStaffIneligible, ReadinessDimensionConfiguration)
	response, err = service.PutStaffProfile(ctx, fixture.salonID, fixture.ownerID, fixture.secondStaffID, StaffProfileInput{
		MutationMeta:       MutationMeta{ActionKey: "repair-staff-schedule", ExpectedConfigVersion: aggregateResponse.ManleaiCalendar.ConfigVersion},
		WeeklyPeriods:      []WeeklyPeriodInput{{DayOfWeek: 2, StartMinute: 600, EndMinute: 1080}},
		EligibleServiceIDs: []string{fixture.serviceID},
	})
	if err != nil {
		t.Fatalf("first repair step must be allowed while incomplete: %v", err)
	}
	response, err = service.PutServicePolicy(ctx, fixture.salonID, fixture.ownerID, fixture.serviceID, ServicePolicyInput{
		MutationMeta:     MutationMeta{ActionKey: "repair-service-assignment", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Enabled:          true,
		CapacityMode:     &staffOnly,
		EligibleStaffIDs: []string{fixture.secondStaffID},
	})
	if err != nil {
		t.Fatalf("second repair step: %v", err)
	}
	if !response.ManleaiCalendar.Readiness.ConfigurationReady {
		t.Fatalf("repaired config blockers = %#v", response.ManleaiCalendar.Readiness.Blockers)
	}

	response, err = service.CreateResource(ctx, fixture.salonID, fixture.ownerID, ResourcePoolInput{
		MutationMeta: MutationMeta{ActionKey: "resource-create", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion}, Name: "Pedicure chairs", Capacity: 3,
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	resourceID := response.ManleaiCalendar.Resources[0].ID
	response, err = service.ArchiveResource(ctx, fixture.salonID, fixture.ownerID, resourceID, MutationMeta{ActionKey: "resource-archive", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion})
	if err != nil {
		t.Fatalf("archive resource: %v", err)
	}
	_, err = service.UpdateResource(ctx, fixture.salonID, fixture.ownerID, resourceID, ResourcePoolInput{
		MutationMeta: MutationMeta{ActionKey: "resource-update-archived", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion}, Name: "Rewritten history", Capacity: 9,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("update archived resource error = %v, want ErrValidation", err)
	}

	var authority string
	if err := fixture.db.QueryRow(`SELECT scheduling_authority FROM salon_settings WHERE salon_id = $1`, fixture.salonID).Scan(&authority); err != nil {
		t.Fatalf("load authority: %v", err)
	}
	if authority != "external_provider" {
		t.Fatalf("Phase 3 changed scheduling authority to %q", authority)
	}
}

func newCalendarPGFixture(t *testing.T) *calendarPGFixture {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("ping database: %v", err)
	}
	fixture := &calendarPGFixture{db: db}
	ctx := context.Background()
	for target, name := range map[*string]string{&fixture.ownerID: "Calendar Owner", &fixture.otherOwnerID: "Other Calendar Owner"} {
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash, full_name) VALUES ($1,'integration-test',$2) RETURNING id::text
		`, "calendar-"+uuid.NewString()+"@example.com", name).Scan(target); err != nil {
			db.Close()
			t.Fatalf("insert user: %v", err)
		}
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons (name, phone, owner_user_id, timezone)
		VALUES ('ManleAI Calendar Test Salon', '+13125550123', $1, 'America/Chicago') RETURNING id::text
	`, fixture.ownerID).Scan(&fixture.salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_settings (salon_id, scheduling_authority) VALUES ($1,'external_provider')`, fixture.salonID); err != nil {
		t.Fatalf("insert settings: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO services (salon_id, pos_provider, pos_service_id, name, duration_minutes, active, ai_bookable)
		VALUES ($1,'square',$2,'Gel Manicure',60,true,true) RETURNING id::text
	`, fixture.salonID, "calendar-service-"+uuid.NewString()).Scan(&fixture.serviceID); err != nil {
		t.Fatalf("insert service: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, active, ai_bookable)
		VALUES ($1,'square',$2,'Kim',true,true) RETURNING id::text
	`, fixture.salonID, "calendar-staff-"+uuid.NewString()).Scan(&fixture.staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO staff (salon_id, pos_provider, pos_staff_id, name, active, ai_bookable)
		VALUES ($1,'square',$2,'Linh',true,true) RETURNING id::text
	`, fixture.salonID, "calendar-staff-"+uuid.NewString()).Scan(&fixture.secondStaffID); err != nil {
		t.Fatalf("insert second staff: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id = $1`, fixture.salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, fixture.ownerID, fixture.otherOwnerID)
		db.Close()
	})
	return fixture
}

func validConfigRequest(actionKey string, expected int64) CalendarConfigInput {
	return CalendarConfigInput{
		MutationMeta:                MutationMeta{ActionKey: actionKey, ExpectedConfigVersion: expected},
		SlotStepMinutes:             15,
		MinimumBookingNoticeMinutes: 60,
		BookingHorizonDays:          60,
		MaxPartySize:                6,
		DefaultBufferBeforeMinutes:  5,
		DefaultBufferAfterMinutes:   10,
		RescheduleCutoffMinutes:     intTestPointer(240),
		CancellationCutoffMinutes:   intTestPointer(240),
	}
}

func findPolicy(t *testing.T, aggregate *Aggregate, serviceID string) ServicePolicy {
	t.Helper()
	for _, policy := range aggregate.ServicePolicies {
		if policy.Service.ID == serviceID {
			return policy
		}
	}
	t.Fatalf("service policy %s not found", serviceID)
	return ServicePolicy{}
}

func assertCurrentActivation(t *testing.T, config *CalendarConfig) {
	t.Helper()
	if config == nil || config.ActivatedAt == nil || config.ActivatedByUserID == "" || config.ActivatedVersion == nil || *config.ActivatedVersion != config.Version {
		t.Fatalf("activation is not fenced to current version: %#v", config)
	}
}
