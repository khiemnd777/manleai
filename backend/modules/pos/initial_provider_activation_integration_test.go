package pos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestInitialProviderActivationIsVersionFencedIdempotentAndTenantScoped(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	ownerID := uuid.New()
	salonID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,full_name) VALUES($1,$2,'activation-test','Activation owner')`, ownerID, "activation-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=$1`, ownerID)
	}()
	if _, err := db.ExecContext(ctx, `INSERT INTO salons(id,name,phone,owner_user_id,active_pos_provider) VALUES($1,'Activation salon',$2,$3,'')`, salonID, "+1312"+strings.ReplaceAll(uuid.NewString(), "-", "")[:7], ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_integration_configs(salon_id,provider,enabled,settings,secrets_encrypted)
		VALUES($1,'square',true,'{"environment":"sandbox","client_id":"tenant-client","redirect_url":"https://tenant.example.test/callback","api_version":"2026-05-20"}','encrypted')
	`, salonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE technical_resource_versions SET version=3 WHERE salon_id=$1 AND resource_type='integration_config' AND resource_id='square'`, salonID); err != nil {
		t.Fatal(err)
	}
	var capabilityVersion int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO pos_connections(salon_id,provider,status,merchant_id,location_id,snapshot_generation,last_sync_at,scopes)
		VALUES($1,'square','active','merchant-activation','location-activation',4,now(),ARRAY['APPOINTMENTS_WRITE'])
		RETURNING booking_write_capability_version
	`, salonID).Scan(&capabilityVersion); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	input := InitialProviderActivationMutation{
		SalonID: salonID.String(), ActorUserID: ownerID.String(), Provider: ProviderSquare,
		ActionKey: "activate-square-" + uuid.NewString(), RequestFingerprint: strings.Repeat("a", 64),
		ExpectedVersion: 0, ExpectedIntegrationConfigVersion: 3,
		ExpectedConnectionCapabilityVersion: capabilityVersion,
	}
	state, replayed, err := repo.ActivateInitialProviderForPlatform(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if replayed || state.Provider != ProviderSquare || state.Version != 1 {
		t.Fatalf("activation state=%#v replayed=%v", state, replayed)
	}
	replayedState, replayed, err := repo.ActivateInitialProviderForPlatform(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !replayed || replayedState != state {
		t.Fatalf("activation replay state=%#v replayed=%v", replayedState, replayed)
	}
	input.ActionKey = "activate-square-again-" + uuid.NewString()
	input.RequestFingerprint = strings.Repeat("b", 64)
	input.ExpectedVersion = state.Version
	if _, _, err := repo.ActivateInitialProviderForPlatform(ctx, input); !errors.Is(err, ErrActiveProviderAlreadyConfigured) {
		t.Fatalf("second activation error=%v, want ErrActiveProviderAlreadyConfigured", err)
	}
	var provider string
	if err := db.QueryRowContext(ctx, `SELECT active_pos_provider FROM salons WHERE id=$1`, salonID).Scan(&provider); err != nil {
		t.Fatal(err)
	}
	if provider != ProviderSquare {
		t.Fatalf("active provider=%q, want square", provider)
	}
}
