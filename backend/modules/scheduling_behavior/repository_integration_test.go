package scheduling_behavior

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/scheduling"
)

func TestBookingModeMutationPreservesAuthorityAndAuditsReplay(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	ownerID := uuid.NewString()
	actorID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users(id,email,password_hash,full_name,status)
		VALUES ($1,$2,'test','Owner','active'),($3,$4,'test','Platform Admin','active')
	`, ownerID, "behavior-owner-"+uuid.NewString()+"@example.com", actorID, "behavior-actor-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,owner_user_id)
		VALUES ('Scheduling Behavior Test',$1,$2) RETURNING id::text
	`, "+1"+uuid.NewString()[:10], ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO salon_settings(salon_id,scheduling_authority,booking_mode)
		VALUES ($1,'manleai_calendar','pending_approval')
	`, salonID); err != nil {
		t.Fatalf("insert settings: %v", err)
	}

	repository := NewRepository(db)
	state, err := repository.Get(ctx, salonID)
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	command := UpdateBookingModeCommand{
		SalonID: salonID, ActorUserID: actorID, BookingMode: scheduling.BookingModeConfirmedBooking,
		ExpectedVersion: state.PolicyVersion, ActionKey: "confirm-automatically",
		RequestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	result, replayed, err := repository.UpdateBookingMode(ctx, command)
	if err != nil || replayed || result.Version != state.PolicyVersion+1 {
		t.Fatalf("update result=%#v replayed=%t error=%v", result, replayed, err)
	}
	replayResult, replayed, err := repository.UpdateBookingMode(ctx, command)
	if err != nil || !replayed || replayResult != result {
		t.Fatalf("replay result=%#v replayed=%t error=%v", replayResult, replayed, err)
	}
	var authority, mode, auditedActor string
	if err := db.QueryRowContext(ctx, `SELECT scheduling_authority,booking_mode FROM salon_settings WHERE salon_id=$1`, salonID).Scan(&authority, &mode); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if authority != "manleai_calendar" || mode != "confirmed_booking" {
		t.Fatalf("authority/mode=%q/%q", authority, mode)
	}
	if err := db.QueryRowContext(ctx, `SELECT actor_user_id::text FROM technical_actions WHERE salon_id=$1 AND action_key=$2`, salonID, command.ActionKey).Scan(&auditedActor); err != nil {
		t.Fatalf("load audit: %v", err)
	}
	if auditedActor != actorID {
		t.Fatalf("audited actor=%q, want %q", auditedActor, actorID)
	}
	stale := command
	stale.ActionKey = "stale-mode"
	stale.RequestFingerprint = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, _, err := repository.UpdateBookingMode(ctx, stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale error=%v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority='owner_manual',booking_mode='pending_approval' WHERE salon_id=$1`, salonID); err != nil {
		t.Fatalf("prepare owner mode: %v", err)
	}
	ownerState, err := repository.Get(ctx, salonID)
	if err != nil {
		t.Fatalf("get owner state: %v", err)
	}
	incompatible := UpdateBookingModeCommand{
		SalonID: salonID, ActorUserID: actorID, BookingMode: scheduling.BookingModeConfirmedBooking,
		ExpectedVersion: ownerState.PolicyVersion, ActionKey: "owner-confirmed-invalid",
		RequestFingerprint: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	if _, _, err := repository.UpdateBookingMode(ctx, incompatible); !errors.Is(err, ErrIncompatibleMode) {
		t.Fatalf("incompatible error=%v", err)
	}
}
