package scheduling_manleai_calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/manleai/ai-receptionist/modules/booking"
)

func TestEvaluateReadinessUsesExplicitPolicyAndActivationVersionFence(t *testing.T) {
	aggregate := readyAggregateFixture()
	aggregate.ServicePolicies[0].Configured = false

	readiness := EvaluateReadiness(aggregate)
	assertBlocker(t, readiness, BlockerServicePolicyRequired, ReadinessDimensionConfiguration)
	if readiness.ConfigurationReady {
		t.Fatal("configuration_ready = true with staff-first eligibility but no explicit service policy")
	}

	aggregate.ServicePolicies[0].Configured = true
	readiness = EvaluateReadiness(aggregate)
	if !readiness.ConfigurationReady {
		t.Fatalf("configuration_ready = false, blockers = %#v", readiness.Blockers)
	}
	if hasBlocker(readiness, BlockerConfigNotActivated) {
		t.Fatalf("current activated_version reported stale: %#v", readiness.Blockers)
	}
	if readiness.ExecutionReady {
		t.Fatal("execution_ready = true before the Phase 4 engine exists")
	}
	if readiness.Capabilities.PartyCreate || readiness.Capabilities.PooledCapacity || readiness.Capabilities.Reschedule || readiness.Capabilities.Cancel {
		t.Fatalf("capabilities exposed without selected internal authority: %#v", readiness.Capabilities)
	}

	staleVersion := aggregate.Config.Version - 1
	aggregate.Config.ActivatedVersion = &staleVersion
	readiness = EvaluateReadiness(aggregate)
	if !readiness.ConfigurationReady {
		t.Fatalf("stale activation changed configuration readiness: %#v", readiness.Blockers)
	}
	assertBlocker(t, readiness, BlockerConfigNotActivated, ReadinessDimensionExecution)
}

func TestServicePolicyCapacityModesDoNotMixStaffOnlyAndPooledResources(t *testing.T) {
	staffOnly := CapacityModeStaffOnly
	resourceID := uuid.NewString()
	req := ServicePolicyInput{
		MutationMeta:         MutationMeta{ActionKey: "staff-only-with-resource", ExpectedConfigVersion: 3},
		Enabled:              true,
		CapacityMode:         &staffOnly,
		EligibleStaffIDs:     []string{uuid.NewString()},
		ResourceRequirements: []ResourceRequirementInput{{ResourcePoolID: resourceID, UnitsRequired: 1}},
	}
	if err := normalizeAndValidateServicePolicy(&req); !errors.Is(err, ErrValidation) {
		t.Fatalf("staff-only resources error = %v, want ErrValidation", err)
	}

	pooled := CapacityModePooled
	pooledReq := ServicePolicyInput{
		MutationMeta:         MutationMeta{ActionKey: "pooled-with-resource", ExpectedConfigVersion: 3},
		Enabled:              true,
		CapacityMode:         &pooled,
		EligibleStaffIDs:     []string{uuid.NewString()},
		ResourceRequirements: []ResourceRequirementInput{{ResourcePoolID: resourceID, UnitsRequired: 1}},
	}
	if err := normalizeAndValidateServicePolicy(&pooledReq); err != nil {
		t.Fatalf("valid pooled policy error = %v", err)
	}

	aggregate := readyAggregateFixture()
	aggregate.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
	aggregate.ServicePolicies = append(aggregate.ServicePolicies, ServicePolicy{
		Service:              ServiceRef{ID: uuid.NewString(), Name: "Pedicure", DurationMinutes: 45, Active: true, AIBookable: true},
		Configured:           true,
		Enabled:              true,
		CapacityMode:         &pooled,
		EligibleStaff:        []StaffRef{aggregate.StaffProfiles[0].Staff},
		ResourceRequirements: []ResourceRequirement{{ResourcePoolID: resourceID, ResourceName: "Chair", UnitsRequired: 1, PoolCapacity: 2}},
	})
	readiness := EvaluateReadiness(aggregate)
	if !readiness.ConfigurationReady {
		t.Fatalf("two independently valid service modes were not configuration-ready: %#v", readiness.Blockers)
	}
	if !readiness.Capabilities.PooledCapacity || !readiness.Capabilities.Reschedule || !readiness.Capabilities.Cancel {
		t.Fatalf("granular pooled capability = %#v", readiness.Capabilities)
	}
}

func TestExceptionValidationMatchesPersistedScopeEffectContract(t *testing.T) {
	now := time.Now().UTC()
	staffID := uuid.NewString()
	resourceID := uuid.NewString()
	tests := []struct {
		name    string
		req     ExceptionInput
		wantErr bool
	}{
		{name: "salon unavailable", req: ExceptionInput{MutationMeta: MutationMeta{ActionKey: "salon", ExpectedConfigVersion: 1}, ScopeType: ExceptionScopeSalon, Effect: ExceptionEffectUnavailable, StartsAt: now, EndsAt: now.Add(time.Hour)}},
		{name: "staff available", req: ExceptionInput{MutationMeta: MutationMeta{ActionKey: "staff", ExpectedConfigVersion: 1}, ScopeType: ExceptionScopeStaff, StaffID: staffID, Effect: ExceptionEffectAvailable, StartsAt: now, EndsAt: now.Add(time.Hour)}},
		{name: "resource capacity", req: ExceptionInput{MutationMeta: MutationMeta{ActionKey: "resource", ExpectedConfigVersion: 1}, ScopeType: ExceptionScopeResource, ResourcePoolID: resourceID, Effect: ExceptionEffectCapacityOverride, CapacityOverride: intTestPointer(0), StartsAt: now, EndsAt: now.Add(time.Hour)}},
		{name: "salon cannot override capacity", req: ExceptionInput{MutationMeta: MutationMeta{ActionKey: "bad-salon", ExpectedConfigVersion: 1}, ScopeType: ExceptionScopeSalon, Effect: ExceptionEffectCapacityOverride, CapacityOverride: intTestPointer(1), StartsAt: now, EndsAt: now.Add(time.Hour)}, wantErr: true},
		{name: "resource cannot use unavailable", req: ExceptionInput{MutationMeta: MutationMeta{ActionKey: "bad-resource", ExpectedConfigVersion: 1}, ScopeType: ExceptionScopeResource, ResourcePoolID: resourceID, Effect: ExceptionEffectUnavailable, StartsAt: now, EndsAt: now.Add(time.Hour)}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := normalizeAndValidateException(&test.req)
			if test.wantErr != errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want validation=%t", err, test.wantErr)
			}
		})
	}
}

func TestPeriodValidationAllowsAdjacentRangesAndRejectsOverlap(t *testing.T) {
	valid := ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "adjacent", ExpectedConfigVersion: 1},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: 1, StartMinute: 720, EndMinute: 900}, {DayOfWeek: 1, StartMinute: 540, EndMinute: 720}},
	}
	if err := normalizeAndValidateHours(&valid); err != nil {
		t.Fatalf("adjacent periods error = %v", err)
	}
	overlap := ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "overlap", ExpectedConfigVersion: 1},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: 2, StartMinute: 600, EndMinute: 800}, {DayOfWeek: 2, StartMinute: 700, EndMinute: 900}},
	}
	if err := normalizeAndValidateHours(&overlap); !errors.Is(err, ErrValidation) {
		t.Fatalf("overlap error = %v, want ErrValidation", err)
	}
}

func readyAggregateFixture() *Aggregate {
	staff := StaffRef{ID: uuid.NewString(), Name: "Kim", Active: true, AIBookable: true}
	service := ServiceRef{ID: uuid.NewString(), Name: "Gel Manicure", DurationMinutes: 60, Active: true, AIBookable: true}
	mode := CapacityModeStaffOnly
	version := int64(9)
	now := time.Now().UTC()
	return &Aggregate{
		SalonID:          uuid.NewString(),
		AuthorityVersion: 1,
		ConfigVersion:    version,
		Config: &CalendarConfig{
			Version: version, ActivatedAt: &now, ActivatedByUserID: uuid.NewString(), ActivatedVersion: &version,
		},
		Hours: []BusinessHourPeriod{{ID: uuid.NewString(), DayOfWeek: 1, StartMinute: 540, EndMinute: 1020}},
		StaffProfiles: []StaffProfile{{
			Staff: staff, WeeklyPeriods: []WeeklyPeriod{{ID: uuid.NewString(), StaffID: staff.ID, DayOfWeek: 1, StartMinute: 540, EndMinute: 1020}}, EligibleServices: []ServiceRef{service},
		}},
		ServicePolicies: []ServicePolicy{{Service: service, Configured: true, Enabled: true, CapacityMode: &mode, EligibleStaff: []StaffRef{staff}, ResourceRequirements: []ResourceRequirement{}}},
		Resources:       []ResourcePool{},
		Exceptions:      []CalendarException{},
	}
}

func TestSchedulingTargetReadinessUsesStaffOnlyCapabilitiesWithoutAggregateExecutionReady(t *testing.T) {
	aggregate := readyAggregateFixture()
	runtimeReadiness := EvaluateReadiness(func() *Aggregate {
		preview := *aggregate
		preview.SchedulingAuthority = booking.SchedulingAuthorityManleAICalendar
		return &preview
	}())
	if !runtimeReadiness.Capabilities.StaffOnlyAvailability || !runtimeReadiness.Capabilities.StaffOnlyCreate || runtimeReadiness.ExecutionReady {
		t.Fatalf("fixture readiness=%#v, want staff-only ready and aggregate execution not ready", runtimeReadiness)
	}
	service := NewService(&fakeCalendarStore{aggregate: aggregate})
	target, err := service.SchedulingTargetReadiness(context.Background(), aggregate.SalonID, uuid.NewString())
	if err != nil {
		t.Fatalf("target readiness: %v", err)
	}
	if !target.Ready || !target.AvailabilityReady || !target.ExecutionReady || len(target.Blockers) != 0 || len(target.AvailabilityBlockers) != 0 || len(target.ExecutionBlockers) != 0 {
		t.Fatalf("target readiness=%#v, want ready from staff-only capabilities", target)
	}
}

func assertBlocker(t *testing.T, readiness Readiness, code string, dimension string) {
	t.Helper()
	for _, item := range readiness.Blockers {
		if item.Code == code && item.Dimension == dimension {
			return
		}
	}
	t.Fatalf("blocker %s/%s missing from %#v", code, dimension, readiness.Blockers)
}

func hasBlocker(readiness Readiness, code string) bool {
	for _, item := range readiness.Blockers {
		if item.Code == code {
			return true
		}
	}
	return false
}

func intTestPointer(value int) *int { return &value }
