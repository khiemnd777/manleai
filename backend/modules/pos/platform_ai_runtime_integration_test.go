package pos

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func TestPlatformAIRuntimeMutationUsesActualActorVersionAndReplay(t *testing.T) {
	databaseURL := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not set")
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
		VALUES ($1,$2,'test','Owner','active'),($3,$4,'test','Platform Ops','active')
	`, ownerID, "ai-owner-"+uuid.NewString()+"@example.com", actorID, "ai-actor-"+uuid.NewString()+"@example.com"); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,owner_user_id)
		VALUES ('AI Runtime Test',$1,$2) RETURNING id::text
	`, "+1"+uuid.NewString()[:10], ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}

	repo := NewRepository(db)
	state, replayed, err := repo.SetSalonAIRuntimeForPlatform(ctx, AIRuntimeMutation{
		SalonID: salonID, ActorUserID: actorID, ActionKey: "enable-ai-runtime",
		RequestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExpectedVersion:    0, Enabled: true,
	})
	if err != nil {
		t.Fatalf("enable AI runtime: %v", err)
	}
	if replayed || !state.Enabled || state.Version != 1 {
		t.Fatalf("first result=%+v replayed=%t", state, replayed)
	}
	replayedState, replayed, err := repo.SetSalonAIRuntimeForPlatform(ctx, AIRuntimeMutation{
		SalonID: salonID, ActorUserID: actorID, ActionKey: "enable-ai-runtime",
		RequestFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ExpectedVersion:    0, Enabled: true,
	})
	if err != nil || !replayed || replayedState != state {
		t.Fatalf("replay result=%+v replayed=%t error=%v", replayedState, replayed, err)
	}
	_, _, err = repo.SetSalonAIRuntimeForPlatform(ctx, AIRuntimeMutation{
		SalonID: salonID, ActorUserID: actorID, ActionKey: "stale-disable",
		RequestFingerprint: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ExpectedVersion:    0, Enabled: false,
	})
	if !errors.Is(err, ErrTechnicalVersionConflict) {
		t.Fatalf("stale error=%v, want ErrTechnicalVersionConflict", err)
	}
	var auditedActor string
	if err := db.QueryRowContext(ctx, `
		SELECT actor_user_id::text FROM technical_actions
		WHERE salon_id=$1 AND action_key='enable-ai-runtime'
	`, salonID).Scan(&auditedActor); err != nil {
		t.Fatalf("load technical action: %v", err)
	}
	if auditedActor != actorID {
		t.Fatalf("audited actor=%q, want actual Platform actor %q", auditedActor, actorID)
	}
}
