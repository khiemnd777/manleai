package migrations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestV83ProviderLocatorBindsOnlyEnabledTwilioVoiceRoutes(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	salonA, salonB := insertV79Salons(t, tx)
	routeA := uuid.NewString()
	routeB := uuid.NewString()
	insertV83Route(t, tx, routeA, salonA, "+13125550181", true, true)
	insertV83Route(t, tx, routeB, salonB, "+13125550182", true, false)
	callSID := "CA" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := tx.Exec(`
		INSERT INTO call_sessions(salon_id,provider,provider_call_id)
		VALUES($1,'twilio',$2)
	`, salonB, callSID); err != nil {
		t.Fatalf("insert call route: %v", err)
	}

	assertNoV83LocatorRow(t, tx, "", routeA)
	assertNoV83LocatorRow(t, tx, "worker", routeA)
	setV79DatabaseContext(t, tx, "provider", "")
	var located string
	if err := tx.QueryRow(`SELECT public.app_provider_twilio_voice_route_salon($1::uuid)::text`, routeA).Scan(&located); err != nil {
		t.Fatalf("locate enabled route: %v", err)
	}
	if located != salonA {
		t.Fatalf("located salon=%q, want %q", located, salonA)
	}
	var legacyLocated sql.NullString
	if err := tx.QueryRow(`SELECT public.app_provider_voice_route_salon('twilio',$1)::text`, callSID).Scan(&legacyLocated); err != nil {
		t.Fatalf("locate unbound legacy call route: %v", err)
	}
	if !legacyLocated.Valid || legacyLocated.String != salonB {
		t.Fatalf("legacy call route salon=%q valid=%t, want %q", legacyLocated.String, legacyLocated.Valid, salonB)
	}
	assertNoV83LocatorRow(t, tx, "provider", routeB)
	assertNoV83LocatorRow(t, tx, "provider", uuid.NewString())

	setV79DatabaseContext(t, tx, "provider", salonA)
	assertNoV83LocatorRowCurrentContext(t, tx, routeB)
	if err := tx.QueryRow(`SELECT public.app_provider_voice_route_salon('twilio',$1)::text`, callSID).Scan(&legacyLocated); err != nil {
		t.Fatalf("query bound legacy call locator: %v", err)
	}
	if legacyLocated.Valid {
		t.Fatalf("bound legacy locator rebound to salon %q", legacyLocated.String)
	}
}

func TestV83ActiveTwilioInboundNumberIsGloballyUnique(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	salonA, salonB := insertV79Salons(t, tx)
	number := "+13125550183"
	insertV83Route(t, tx, uuid.NewString(), salonA, number, true, true)
	_, err = tx.Exec(`
		INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings)
		VALUES($1,$2,'twilio',true,jsonb_build_object('voice_inbound_number',$3::text,'voice_routing_enabled','true'))
	`, uuid.NewString(), salonB, number)
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != "23505" || pqErr.Constraint != "idx_twilio_voice_active_inbound_number" {
		t.Fatalf("duplicate active inbound number error=%v", err)
	}
}

func insertV83Route(t *testing.T, tx *sql.Tx, routeID, salonID, number string, enabled, routingEnabled bool) {
	t.Helper()
	if _, err := tx.Exec(`
		INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings)
		VALUES($1,$2,'twilio',$3,jsonb_build_object(
			'voice_inbound_number',$4::text,
			'voice_routing_enabled',$5::text
		))
	`, routeID, salonID, enabled, number, map[bool]string{true: "true", false: "false"}[routingEnabled]); err != nil {
		t.Fatalf("insert Twilio route: %v", err)
	}
}

func assertNoV83LocatorRow(t *testing.T, tx *sql.Tx, scope, routeID string) {
	t.Helper()
	setV79DatabaseContext(t, tx, scope, "")
	assertNoV83LocatorRowCurrentContext(t, tx, routeID)
}

func assertNoV83LocatorRowCurrentContext(t *testing.T, tx *sql.Tx, routeID string) {
	t.Helper()
	var salonID string
	err := tx.QueryRow(`
		SELECT located.salon_id::text
		FROM (SELECT public.app_provider_twilio_voice_route_salon($1::uuid) AS salon_id) located
		WHERE located.salon_id IS NOT NULL
	`, routeID).Scan(&salonID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("route=%q locator result=%q error=%v, want no row", routeID, salonID, err)
	}
}
