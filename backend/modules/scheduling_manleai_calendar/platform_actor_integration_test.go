package scheduling_manleai_calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRepositoryPostgresAuthorizedActualActorsCanManageCalendar(t *testing.T) {
	fixture := newCalendarPGFixture(t)
	ctx := context.Background()
	service := NewService(NewRepository(fixture.db))
	adminID := seedCalendarPlatformActor(t, fixture, "platform_admin", "")
	opsID := seedCalendarPlatformActor(t, fixture, "platform_ops", adminID)
	unassignedOpsID := seedCalendarPlatformActor(t, fixture, "platform_ops", "")
	managerID := seedCalendarTenantManager(t, fixture)

	if _, err := service.PutConfig(ctx, fixture.salonID, unassignedOpsID, validConfigRequest("unassigned-platform-ops", 0)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unassigned Platform Ops config error = %v, want ErrNotFound", err)
	}
	if _, err := service.PutConfig(ctx, fixture.salonID, fixture.otherOwnerID, validConfigRequest("cross-tenant-config", 0)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant config error = %v, want ErrNotFound", err)
	}

	response, err := service.PutConfig(ctx, fixture.salonID, adminID, validConfigRequest("platform-admin-config", 0))
	if err != nil {
		t.Fatalf("Platform Admin create config: %v", err)
	}
	response, err = service.PutHours(ctx, fixture.salonID, managerID, ReplaceBusinessHoursInput{
		MutationMeta: MutationMeta{ActionKey: "tenant-manager-hours", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Periods:      []BusinessHourPeriodInput{{DayOfWeek: 1, StartMinute: 570, EndMinute: 1140}},
	})
	if err != nil {
		t.Fatalf("Tenant Business Manager put hours: %v", err)
	}
	response, err = service.PutStaffProfile(ctx, fixture.salonID, opsID, fixture.staffID, StaffProfileInput{
		MutationMeta:       MutationMeta{ActionKey: "platform-ops-staff", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		WeeklyPeriods:      []WeeklyPeriodInput{{DayOfWeek: 1, StartMinute: 570, EndMinute: 1140}},
		EligibleServiceIDs: []string{fixture.serviceID},
	})
	if err != nil {
		t.Fatalf("Platform Ops put staff profile: %v", err)
	}
	staffOnly := CapacityModeStaffOnly
	response, err = service.PutServicePolicy(ctx, fixture.salonID, adminID, fixture.serviceID, ServicePolicyInput{
		MutationMeta:     MutationMeta{ActionKey: "platform-admin-service", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		Enabled:          true,
		CapacityMode:     &staffOnly,
		EligibleStaffIDs: []string{fixture.staffID},
	})
	if err != nil {
		t.Fatalf("Platform Admin put service policy: %v", err)
	}
	response, err = service.Activate(ctx, fixture.salonID, adminID, MutationMeta{
		ActionKey:             "platform-admin-activate",
		ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion,
	})
	if err != nil {
		t.Fatalf("Platform Admin activate: %v", err)
	}
	assertCurrentActivation(t, response.ManleaiCalendar.Config)
	if response.ManleaiCalendar.Config.ActivatedByUserID != adminID {
		t.Fatalf("activation actor=%q, want actual Platform Admin %q", response.ManleaiCalendar.Config.ActivatedByUserID, adminID)
	}

	startsAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	response, err = service.CreateException(ctx, fixture.salonID, opsID, ExceptionInput{
		MutationMeta: MutationMeta{ActionKey: "platform-ops-exception", ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion},
		ScopeType:    ExceptionScopeSalon,
		Effect:       ExceptionEffectUnavailable,
		StartsAt:     startsAt,
		EndsAt:       startsAt.Add(time.Hour),
		Reason:       "Staff meeting",
	})
	if err != nil {
		t.Fatalf("Platform Ops create exception: %v", err)
	}
	if len(response.ManleaiCalendar.Exceptions) != 1 || response.ManleaiCalendar.Exceptions[0].CreatedByUserID != opsID {
		t.Fatalf("exception creator did not preserve actual Platform Ops actor: %#v", response.ManleaiCalendar.Exceptions)
	}
	exceptionID := response.ManleaiCalendar.Exceptions[0].ID
	response, err = service.CancelException(ctx, fixture.salonID, opsID, exceptionID, MutationMeta{
		ActionKey:             "platform-ops-exception-cancel",
		ExpectedConfigVersion: response.ManleaiCalendar.ConfigVersion,
	})
	if err != nil {
		t.Fatalf("Platform Ops cancel exception: %v", err)
	}
	if response.ManleaiCalendar.Exceptions[0].CancelledByUserID != opsID {
		t.Fatalf("exception cancellation actor=%q, want actual Platform Ops %q", response.ManleaiCalendar.Exceptions[0].CancelledByUserID, opsID)
	}

	for actionKey, expectedActorID := range map[string]string{
		"platform-admin-config":         adminID,
		"tenant-manager-hours":          managerID,
		"platform-ops-staff":            opsID,
		"platform-admin-service":        adminID,
		"platform-admin-activate":       adminID,
		"platform-ops-exception":        opsID,
		"platform-ops-exception-cancel": opsID,
	} {
		var actorID string
		if err := fixture.db.QueryRowContext(ctx, `
			SELECT actor_user_id::text
			FROM manleai_calendar_config_events
			WHERE salon_id = $1 AND action_key = $2
		`, fixture.salonID, actionKey).Scan(&actorID); err != nil {
			t.Fatalf("load event actor for %s: %v", actionKey, err)
		}
		if actorID != expectedActorID {
			t.Fatalf("event %s actor=%q, want %q", actionKey, actorID, expectedActorID)
		}
	}
}

func seedCalendarPlatformActor(t *testing.T, fixture *calendarPGFixture, roleName string, assignmentActorID string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := fixture.db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name, principal_scope)
		VALUES ($1, 'integration-test', $2, 'platform')
		RETURNING id::text
	`, "calendar-"+roleName+"-"+uuid.NewString()+"@example.com", "Calendar "+roleName).Scan(&userID); err != nil {
		t.Fatalf("insert %s user: %v", roleName, err)
	}
	if _, err := fixture.db.ExecContext(ctx, `
		INSERT INTO platform_role_assignments (user_id, role_id, status)
		SELECT $1, role.id, 'active' FROM roles role WHERE role.name = $2
	`, userID, roleName); err != nil {
		t.Fatalf("assign %s role: %v", roleName, err)
	}
	if assignmentActorID != "" {
		var assignmentID string
		if err := fixture.db.QueryRowContext(ctx, `
			INSERT INTO platform_salon_assignments (
				salon_id, user_id, status, created_by_user_id, updated_by_user_id
			)
			VALUES ($1, $2, 'active', $3, $3)
			RETURNING id::text
		`, fixture.salonID, userID, assignmentActorID).Scan(&assignmentID); err != nil {
			t.Fatalf("assign Platform Ops salon: %v", err)
		}
		if _, err := fixture.db.ExecContext(ctx, `
			INSERT INTO platform_salon_assignment_permissions (assignment_id, permission_id)
			SELECT $1, permission.id
			FROM permissions permission
			WHERE permission.name IN ('technical.read', 'technical.write')
		`, assignmentID); err != nil {
			t.Fatalf("assign Platform Ops technical permissions: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = fixture.db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

func seedCalendarTenantManager(t *testing.T, fixture *calendarPGFixture) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := fixture.db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, full_name, principal_scope)
		VALUES ($1, 'integration-test', 'Calendar Tenant Manager', 'tenant')
		RETURNING id::text
	`, "calendar-manager-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert Tenant Business Manager: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `
		INSERT INTO salon_memberships (salon_id, user_id, role_id, status, is_owner)
		SELECT $1, $2, role.id, 'active', false
		FROM roles role
		WHERE role.name = 'tenant_business_manager'
	`, fixture.salonID, userID); err != nil {
		t.Fatalf("assign Tenant Business Manager membership: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}
