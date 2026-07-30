package configtransfer

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"

	integrationconfig "github.com/manleai/ai-receptionist/modules/integration_config"
)

const slugCheckTestDriverName = "config_transfer_slug_check_driver"

var slugCheckDriverState = struct {
	sync.Mutex
	query     string
	args      []driver.Value
	execQuery string
	execArgs  []driver.Value
	taken     bool
	err       error
}{}

func init() {
	sql.Register(slugCheckTestDriverName, slugCheckTestDriver{})
}

func TestUpsertSquareConfigPreservesTargetWebhookURLAndDoesNotImportSourceURL(t *testing.T) {
	db, err := sql.Open(slugCheckTestDriverName, "")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	slugCheckDriverState.Lock()
	slugCheckDriverState.execQuery = ""
	slugCheckDriverState.execArgs = nil
	slugCheckDriverState.err = nil
	slugCheckDriverState.Unlock()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx returned error: %v", err)
	}
	defer tx.Rollback()
	err = upsertSquareConfig(context.Background(), tx, "salon_1", integrationconfig.SquareSettingsResponse{
		Environment:            "sandbox",
		ClientID:               "source-client-id",
		RedirectURL:            "https://source.example.com/api/integrations/square/callback",
		APIVersion:             "2026-05-20",
		WebhookNotificationURL: "https://source.example.com/api/integrations/square/webhook",
	})
	if err != nil {
		t.Fatalf("upsertSquareConfig returned error: %v", err)
	}

	slugCheckDriverState.Lock()
	query := slugCheckDriverState.execQuery
	args := append([]driver.Value(nil), slugCheckDriverState.execArgs...)
	slugCheckDriverState.Unlock()
	if !strings.Contains(query, "salon_integration_configs.settings->'webhook_notification_url'") {
		t.Fatalf("query does not preserve target webhook URL: %s", query)
	}
	if len(args) != 2 {
		t.Fatalf("exec args = %#v, want salon id and settings JSON", args)
	}
	settingsJSON, _ := args[1].(string)
	if strings.Contains(settingsJSON, "source.example.com/api/integrations/square/webhook") || strings.Contains(settingsJSON, "webhook_notification_url") {
		t.Fatalf("source-specific webhook URL was included in imported settings: %s", settingsJSON)
	}
}

func TestUpsertTwilioConfigWritesOnlyPortableVoiceTransport(t *testing.T) {
	db, err := sql.Open(slugCheckTestDriverName, "")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()
	slugCheckDriverState.Lock()
	slugCheckDriverState.execQuery = ""
	slugCheckDriverState.execArgs = nil
	slugCheckDriverState.err = nil
	slugCheckDriverState.Unlock()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx returned error: %v", err)
	}
	defer tx.Rollback()
	if err := upsertTwilioConfig(context.Background(), tx, "salon_1", integrationconfig.TwilioSettingsResponse{
		VoiceRouteID: "source-route", VoiceInboundNumber: "+13125550101", PublicBaseURL: "https://source.example.com",
		IncomingPath: "/api/voice/twilio/source-route/incoming", VoiceTransport: "realtime_stream",
	}); err != nil {
		t.Fatalf("upsertTwilioConfig returned error: %v", err)
	}
	slugCheckDriverState.Lock()
	query := slugCheckDriverState.execQuery
	args := append([]driver.Value(nil), slugCheckDriverState.execArgs...)
	slugCheckDriverState.Unlock()
	if len(args) != 2 {
		t.Fatalf("exec args=%#v", args)
	}
	settingsJSON, _ := args[1].(string)
	if settingsJSON != `{"voice_transport":"realtime_stream"}` {
		t.Fatalf("Twilio portable settings=%s", settingsJSON)
	}
	if !strings.Contains(query, "salon_integration_configs.settings || EXCLUDED.settings") ||
		strings.Contains(query, "enabled = EXCLUDED.enabled") || strings.Contains(query, "secrets_encrypted = EXCLUDED.secrets_encrypted") {
		t.Fatalf("Twilio transfer does not preserve target route/enabled/secrets: %s", query)
	}
}

func TestPublicSlugTakenSupportsEmptyOnboardingSalonID(t *testing.T) {
	db, err := sql.Open(slugCheckTestDriverName, "")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	slugCheckDriverState.Lock()
	slugCheckDriverState.query = ""
	slugCheckDriverState.args = nil
	slugCheckDriverState.taken = false
	slugCheckDriverState.err = nil
	slugCheckDriverState.Unlock()

	taken, err := NewRepository(db).PublicSlugTaken(context.Background(), "", "lotus-nails")
	if err != nil {
		t.Fatalf("PublicSlugTaken returned error: %v", err)
	}
	if taken {
		t.Fatalf("taken = true, want false")
	}

	slugCheckDriverState.Lock()
	query := slugCheckDriverState.query
	args := append([]driver.Value(nil), slugCheckDriverState.args...)
	slugCheckDriverState.Unlock()

	if strings.Contains(query, "id <> $2") {
		t.Fatalf("query uses UUID comparison that fails for onboarding salon id: %s", query)
	}
	if !strings.Contains(query, "id::text <> $2") {
		t.Fatalf("query should compare salon id as text for onboarding-safe checks: %s", query)
	}
	if len(args) != 2 || args[0] != "lotus-nails" || args[1] != "" {
		t.Fatalf("query args = %#v, want slug and empty salon id", args)
	}
}

type slugCheckTestDriver struct{}

func (slugCheckTestDriver) Open(name string) (driver.Conn, error) {
	return slugCheckTestConn{}, nil
}

type slugCheckTestConn struct{}

func (slugCheckTestConn) Prepare(query string) (driver.Stmt, error) {
	return slugCheckTestStmt{query: query}, nil
}

func (slugCheckTestConn) Close() error {
	return nil
}

func (slugCheckTestConn) Begin() (driver.Tx, error) {
	return slugCheckTestTx{}, nil
}

type slugCheckTestStmt struct {
	query string
}

func (s slugCheckTestStmt) Close() error {
	return nil
}

func (s slugCheckTestStmt) NumInput() int {
	return -1
}

func (s slugCheckTestStmt) Exec(args []driver.Value) (driver.Result, error) {
	slugCheckDriverState.Lock()
	defer slugCheckDriverState.Unlock()
	slugCheckDriverState.execQuery = s.query
	slugCheckDriverState.execArgs = append([]driver.Value(nil), args...)
	if slugCheckDriverState.err != nil {
		return nil, slugCheckDriverState.err
	}
	return driver.RowsAffected(1), nil
}

func (s slugCheckTestStmt) Query(args []driver.Value) (driver.Rows, error) {
	slugCheckDriverState.Lock()
	defer slugCheckDriverState.Unlock()
	slugCheckDriverState.query = s.query
	slugCheckDriverState.args = append([]driver.Value(nil), args...)
	if slugCheckDriverState.err != nil {
		return nil, slugCheckDriverState.err
	}
	return &slugCheckTestRows{taken: slugCheckDriverState.taken}, nil
}

type slugCheckTestRows struct {
	sent  bool
	taken bool
}

func (r *slugCheckTestRows) Columns() []string {
	return []string{"exists"}
}

func (r *slugCheckTestRows) Close() error {
	return nil
}

func (r *slugCheckTestRows) Next(dest []driver.Value) error {
	if r.sent {
		return io.EOF
	}
	dest[0] = r.taken
	r.sent = true
	return nil
}

type slugCheckTestTx struct{}

func (slugCheckTestTx) Commit() error {
	return nil
}

func (slugCheckTestTx) Rollback() error {
	return nil
}
