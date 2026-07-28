package sampledata

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeRequestRequiresExplicitProfileAndDistinctAccounts(t *testing.T) {
	valid := ApplyRequest{
		Profile:             ProfileSampleTest,
		Confirmation:        ConfirmationToken,
		AdminEmail:          "admin@example.test",
		AdminName:           "Platform Admin",
		AdminPassword:       "sample-admin-password",
		OpsEmail:            "ops@example.test",
		OpsName:             "Platform Ops",
		OpsPassword:         "sample-ops-password",
		TenantOwnerPassword: "sample-owner-password",
	}
	if normalized, err := normalizeRequest(valid); err != nil || normalized.AdminEmail != valid.AdminEmail {
		t.Fatalf("normalize valid request=%#v err=%v", normalized, err)
	}

	tests := []struct {
		name   string
		mutate func(*ApplyRequest)
	}{
		{name: "wrong profile", mutate: func(req *ApplyRequest) { req.Profile = "live" }},
		{name: "missing confirmation", mutate: func(req *ApplyRequest) { req.Confirmation = "" }},
		{name: "duplicate platform accounts", mutate: func(req *ApplyRequest) { req.OpsEmail = req.AdminEmail }},
		{name: "owner collision", mutate: func(req *ApplyRequest) { req.AdminEmail = LotusOwnerEmail }},
		{name: "short password", mutate: func(req *ApplyRequest) { req.OpsPassword = "too-short" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := valid
			test.mutate(&req)
			if _, err := normalizeRequest(req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestEmbeddedFixtureIsSeparateAndFailSafe(t *testing.T) {
	migrations, err := loadFixtureMigrations()
	if err != nil {
		t.Fatalf("load fixture migrations: %v", err)
	}
	if len(migrations) != 1 || migrations[0].Version != "1" {
		t.Fatalf("fixture migrations=%#v, want only V1", migrations)
	}
	sqlText := migrations[0].SQL
	for _, fragment := range []string{
		"'Lotus Nails Studio'",
		"'owner@lotusnails.example'",
		"'owner_manual'",
		"'pending_approval'",
		"'sample_test'",
		"INSERT INTO services",
		"INSERT INTO staff",
		"INSERT INTO manleai_calendar_service_staff",
	} {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("sample fixture missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"INSERT INTO salon_integration_configs",
		"INSERT INTO platform_pii_access_grants",
		"scheduling_authority,\n    'external_provider'",
	} {
		if strings.Contains(sqlText, forbidden) {
			t.Fatalf("sample fixture contains forbidden configuration %q", forbidden)
		}
	}
}

func TestUnsafeTargetSentinel(t *testing.T) {
	wrapped := errors.Join(ErrUnsafeTarget, errors.New("live user found"))
	if !errors.Is(wrapped, ErrUnsafeTarget) {
		t.Fatal("unsafe target errors must preserve the sentinel")
	}
}
