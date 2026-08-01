package pos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSquareSchedulingCapabilityEvaluationBuyerReplayAndSellerInvalidation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OWNER_FIRST_RELEASE_GATE_DATABASE_REQUIRED") == "1" {
			t.Fatal("TEST_DATABASE_URL is required in release-gate mode")
		}
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repository := NewRepository(db)

	actorID := uuid.New()
	salonID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,email,password_hash,full_name) VALUES($1,$2,'capability-test','Capability reviewer')`, actorID, "capability-"+uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO salons(id,name,phone,owner_user_id,active_pos_provider) VALUES($1,'Capability test',$2,$3,'square')`, salonID, "+1312555"+strings.ReplaceAll(uuid.NewString(), "-", "")[:4], actorID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, actorID)
	}()
	configID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO salon_integration_configs(id,salon_id,provider,enabled,settings) VALUES($1,$2,'square',true,'{"api_version":"2026-05-20"}')`, configID, salonID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO technical_resource_versions(salon_id,resource_type,resource_id,version) VALUES($1,'integration_config','square',3) ON CONFLICT(salon_id,resource_type,resource_id) DO UPDATE SET version=3`, salonID); err != nil {
		t.Fatal(err)
	}
	connectionID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO pos_connections(id,salon_id,provider,status,location_id,snapshot_generation,scopes,last_sync_at)
		VALUES($1,$2,'square','active','location-capability',5,ARRAY['CUSTOMERS_READ','APPOINTMENTS_WRITE'],now())
	`, connectionID, salonID); err != nil {
		t.Fatal(err)
	}
	var connectionVersion int64
	if err := db.QueryRowContext(ctx, `SELECT booking_write_capability_version FROM pos_connections WHERE id=$1`, connectionID).Scan(&connectionVersion); err != nil {
		t.Fatal(err)
	}
	input := SchedulingCapabilityEvaluationInput{
		SalonID: salonID.String(), ActorUserID: actorID.String(), ActionKey: "buyer-review-" + uuid.NewString(),
		RequestFingerprint: strings.Repeat("a", 64), ExpectedConnectionCapabilityVersion: connectionVersion,
		ExpectedIntegrationConfigVersion: 3,
	}
	result, replayed, err := repository.ReevaluateSquareSchedulingCapability(ctx, input)
	if err != nil || replayed || !result.AutomaticSingleCreate || result.AutomaticReschedule || result.AutomaticPartyCreate || result.ResourceCapacity || result.WritePermissionMode != SchedulingWriteModeBuyer {
		t.Fatalf("buyer result/replay/error=%#v/%t/%v", result, replayed, err)
	}
	replay, replayed, err := repository.ReevaluateSquareSchedulingCapability(ctx, input)
	if err != nil || !replayed || replay.EvidenceID != result.EvidenceID {
		t.Fatalf("exact replay=%#v/%t/%v", replay, replayed, err)
	}
	changed := input
	changed.RequestFingerprint = strings.Repeat("b", 64)
	if _, _, err := repository.ReevaluateSquareSchedulingCapability(ctx, changed); !errors.Is(err, ErrTechnicalActionConflict) {
		t.Fatalf("changed action replay error=%v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE pos_connections SET scopes=ARRAY['APPOINTMENTS_WRITE','APPOINTMENTS_ALL_WRITE'] WHERE id=$1`, connectionID); err != nil {
		t.Fatal(err)
	}
	current, err := repository.GetSquareSchedulingCapabilityEvaluation(ctx, salonID.String())
	if err != nil {
		t.Fatal(err)
	}
	if current.EvidenceCurrent || current.AutomaticSingleCreate || current.WritePermissionMode != SchedulingWriteModeSeller || !current.ReconnectRequired {
		t.Fatalf("seller scope did not invalidate buyer evidence: %#v", current)
	}
	input.ActionKey = "seller-review-" + uuid.NewString()
	input.RequestFingerprint = strings.Repeat("c", 64)
	input.ExpectedConnectionCapabilityVersion = current.ConnectionCapabilityVersion
	seller, replayed, err := repository.ReevaluateSquareSchedulingCapability(ctx, input)
	if err != nil || replayed || seller.AutomaticSingleCreate || seller.AutomaticReschedule || seller.AutomaticPartyCreate || seller.ResourceCapacity || seller.BlockerCode != "SQUARE_SELLER_WRITE_UNSAFE" {
		t.Fatalf("seller result/replay/error=%#v/%t/%v", seller, replayed, err)
	}
}
