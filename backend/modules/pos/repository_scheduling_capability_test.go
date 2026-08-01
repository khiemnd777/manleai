package pos

import (
	"database/sql"
	"testing"
)

func TestSquareWritePermissionModeFailsSellerAndMalformedScopesClosed(t *testing.T) {
	for _, test := range []struct {
		name      string
		scopes    []string
		mode      string
		reconnect bool
		blocker   string
	}{
		{name: "buyer", scopes: []string{" customers_read ", "appointments_write"}, mode: SchedulingWriteModeBuyer},
		{name: "seller wins", scopes: []string{"APPOINTMENTS_WRITE", "APPOINTMENTS_ALL_WRITE"}, mode: SchedulingWriteModeSeller, reconnect: true, blocker: "SQUARE_SELLER_WRITE_UNSAFE"},
		{name: "seller only", scopes: []string{"APPOINTMENTS_ALL_WRITE"}, mode: SchedulingWriteModeSeller, reconnect: true, blocker: "SQUARE_SELLER_WRITE_UNSAFE"},
		{name: "missing", scopes: []string{"CUSTOMERS_WRITE"}, mode: SchedulingWriteModeUnsupported, reconnect: true, blocker: "SQUARE_APPOINTMENTS_WRITE_REQUIRED"},
		{name: "unknown", scopes: []string{"APPOINTMENTS_WRITE", "FUTURE_UNREVIEWED_SCOPE"}, mode: SchedulingWriteModeUnsupported, reconnect: true, blocker: "SQUARE_OAUTH_SCOPES_UNRECOGNIZED"},
		{name: "malformed", scopes: []string{"appointments-write", ""}, mode: SchedulingWriteModeUnsupported, reconnect: true, blocker: "SQUARE_OAUTH_SCOPES_UNRECOGNIZED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mode, reconnect, blocker := squareWritePermissionMode(test.scopes)
			if mode != test.mode || reconnect != test.reconnect || blocker != test.blocker {
				t.Fatalf("mode/reconnect/blocker=%q/%t/%q", mode, reconnect, blocker)
			}
		})
	}
}

func TestOAuthScopeFingerprintIsNormalizedAndOrderIndependent(t *testing.T) {
	left := OAuthScopeFingerprint([]string{" APPOINTMENTS_WRITE", "customers_read", "APPOINTMENTS_WRITE"})
	right := OAuthScopeFingerprint([]string{"CUSTOMERS_READ", "appointments_write"})
	if left != right || len(left) != 64 {
		t.Fatalf("fingerprints=%q/%q", left, right)
	}
}

func TestSquareCapabilityBlockerRequiresExactActiveConnectionState(t *testing.T) {
	ready := squareCapabilityFence{
		ConnectionID: "connection-1", ConnectionVersion: 1, ConnectionStatus: StatusActive,
		LocationID: "location-1", SnapshotGeneration: 1, Scopes: []string{"APPOINTMENTS_WRITE"},
		LastSyncAt: sql.NullTime{Valid: true}, ConfigID: "config-1", ConfigVersion: 1, ConfigEnabled: true,
		APIVersion: "2026-05-20",
	}
	if blocker := squareCapabilityBlocker(ready); blocker != "" {
		t.Fatalf("ready blocker=%q", blocker)
	}
	for _, status := range []string{StatusConnected, StatusSyncing, StatusError, StatusExpiredToken, StatusDisabled, StatusNotConnected} {
		item := ready
		item.ConnectionStatus = status
		if blocker := squareCapabilityBlocker(item); blocker != "SQUARE_NOT_CONNECTED" {
			t.Fatalf("status %q blocker=%q", status, blocker)
		}
	}
}
