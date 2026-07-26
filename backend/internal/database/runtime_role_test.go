package database

import (
	"context"
	"errors"
	"testing"
)

func TestValidRuntimeRoleRejectsIdentifiersThatCouldExpandGrantScope(t *testing.T) {
	for _, valid := range []string{"manleai_runtime", "app1", "tenant_api"} {
		if !ValidRuntimeRole(valid) {
			t.Fatalf("valid role %q rejected", valid)
		}
	}
	for _, invalid := range []string{"", "ManleAI", "1runtime", "runtime-role", "runtime role", "runtime;grant", "public.manleai"} {
		if ValidRuntimeRole(invalid) {
			t.Fatalf("unsafe role %q accepted", invalid)
		}
	}
}

func TestOpenApplicationFailsBeforeConnectingWhenProductionRLSInputsAreUnsafe(t *testing.T) {
	if _, err := OpenApplication(context.Background(), "postgres://not-used", "", "", true, true); !errors.Is(err, ErrRuntimeRoleInvalid) {
		t.Fatalf("missing runtime role error=%v", err)
	}
	if _, err := OpenApplication(context.Background(), "postgres://not-used", "", "manleai_runtime", true, true); !errors.Is(err, ErrMigrationURLRequired) {
		t.Fatalf("missing migration URL error=%v", err)
	}
}
