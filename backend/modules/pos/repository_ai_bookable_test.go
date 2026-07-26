package pos

import (
	"context"
	"testing"
)

func TestAIBookableAuthorityRulesFailClosedForUnknownOrIncompleteFences(t *testing.T) {
	validService := serviceAIBookableTarget{Active: true, DurationMinutes: 30, Provider: "provider-a", SyncStatus: SyncStatusSynced}
	validStaff := staffAIBookableTarget{Active: true, Provider: "provider-a", SyncStatus: SyncStatusSynced}
	tests := []struct {
		name  string
		fence aiBookableMutationFence
	}{
		{name: "missing settings", fence: aiBookableMutationFence{SchedulingAuthority: schedulingAuthorityOwnerManual, SchedulingAuthorityVersion: 1}},
		{name: "missing authority", fence: aiBookableMutationFence{HasSchedulingSettings: true, SchedulingAuthorityVersion: 1}},
		{name: "unknown authority", fence: aiBookableMutationFence{HasSchedulingSettings: true, SchedulingAuthority: "future_authority", SchedulingAuthorityVersion: 1}},
		{name: "missing authority version", fence: aiBookableMutationFence{HasSchedulingSettings: true, SchedulingAuthority: schedulingAuthorityOwnerManual}},
		{name: "external missing active provider", fence: aiBookableMutationFence{HasSchedulingSettings: true, SchedulingAuthority: schedulingAuthorityExternalProvider, SchedulingAuthorityVersion: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serviceReady, err := serviceCanEnableAIBookingTx(context.Background(), nil, "salon", test.fence, validService)
			if err != nil || serviceReady {
				t.Fatalf("service readiness = %t/%v, want false/nil", serviceReady, err)
			}
			staffReady, err := staffCanEnableAIBookingTx(context.Background(), nil, "salon", test.fence, validStaff)
			if err != nil || staffReady {
				t.Fatalf("staff readiness = %t/%v, want false/nil", staffReady, err)
			}
		})
	}
}

func TestAIBookableInternalAuthoritiesStillRequireCanonicalEligibility(t *testing.T) {
	for _, authority := range []string{schedulingAuthorityOwnerManual, schedulingAuthorityManleAICalendar} {
		fence := aiBookableMutationFence{HasSchedulingSettings: true, SchedulingAuthority: authority, SchedulingAuthorityVersion: 1}
		serviceReady, err := serviceCanEnableAIBookingTx(context.Background(), nil, "salon", fence, serviceAIBookableTarget{Active: true, DurationMinutes: 0})
		if err != nil || serviceReady {
			t.Fatalf("%s zero-duration service readiness = %t/%v, want false/nil", authority, serviceReady, err)
		}
		staffReady, err := staffCanEnableAIBookingTx(context.Background(), nil, "salon", fence, staffAIBookableTarget{Active: false})
		if err != nil || staffReady {
			t.Fatalf("%s inactive staff readiness = %t/%v, want false/nil", authority, staffReady, err)
		}
	}
}
