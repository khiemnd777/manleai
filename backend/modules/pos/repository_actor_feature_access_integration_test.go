package pos_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/database"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/training"
)

func TestPOSAndTrainingRepositoriesDoNotUseLegacySupportHelpers(t *testing.T) {
	for _, path := range []string{"repository.go", "../training/repository.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, forbidden := range []string{
			"app_active_support_authorization",
			"app_active_support_pii_grant",
			"has_active_tenant_membership",
		} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s still uses legacy actor gate %q", path, forbidden)
			}
		}
		if !strings.Contains(string(source), "app_actor_feature_access") {
			t.Fatalf("%s does not use app_actor_feature_access", path)
		}
	}
}

func TestPOSAndTrainingRepositoriesUseCanonicalActorFeatureAccess(t *testing.T) {
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
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	suffix := uuid.NewString()
	ownerID := insertActorFeatureAccessUser(t, db, "owner-"+suffix+"@example.test", "tenant")
	adminID := insertActorFeatureAccessUser(t, db, "admin-"+suffix+"@example.test", "platform")
	unprivilegedPlatformID := insertActorFeatureAccessUser(t, db, "unprivileged-"+suffix+"@example.test", "platform")
	salonID := insertActorFeatureAccessSalon(t, db, ownerID, suffix)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id=$1`, salonID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3)`, ownerID, adminID, unprivilegedPlatformID)
	})

	if _, err := db.ExecContext(ctx, `
		INSERT INTO platform_role_assignments (
			user_id, role_id, status, created_by_user_id, updated_by_user_id
		)
		SELECT $1, role.id, 'active', $1, $1
		FROM roles role
		WHERE role.name='platform_admin'
	`, adminID); err != nil {
		t.Fatalf("assign platform admin role: %v", err)
	}

	var knowledgeID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO knowledge_items (salon_id, title, category, body, status, source)
		VALUES ($1, 'Canonical access knowledge', 'faq', 'Regression body', 'active', 'owner')
		RETURNING id::text
	`, salonID).Scan(&knowledgeID); err != nil {
		t.Fatalf("insert knowledge item: %v", err)
	}
	var correctionID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO owner_corrections (salon_id, correction, status)
		VALUES ($1, 'Canonical access correction', 'pending')
		RETURNING id::text
	`, salonID).Scan(&correctionID); err != nil {
		t.Fatalf("insert owner correction: %v", err)
	}

	posRepository := pos.NewRepository(db)
	trainingRepository := training.NewRepository(db)

	for _, actor := range []struct {
		name string
		id   string
	}{
		{name: "tenant owner", id: ownerID},
		{name: "platform admin", id: adminID},
	} {
		t.Run(actor.name, func(t *testing.T) {
			provider, err := posRepository.GetActiveProvider(ctx, salonID, actor.id)
			if err != nil || provider != "square" {
				t.Fatalf("active provider=%q err=%v, want square", provider, err)
			}
			knowledge, err := trainingRepository.ListKnowledge(ctx, salonID, actor.id)
			if err != nil || len(knowledge) != 1 || knowledge[0].ID != knowledgeID {
				t.Fatalf("knowledge=%#v err=%v, want item %s", knowledge, err, knowledgeID)
			}
			corrections, err := trainingRepository.ListCorrections(ctx, salonID, actor.id)
			if err != nil || len(corrections) != 1 || corrections[0].ID != correctionID {
				t.Fatalf("corrections=%#v err=%v, want item %s", corrections, err, correctionID)
			}
		})
	}

	t.Run("platform account without capability", func(t *testing.T) {
		if _, err := posRepository.GetActiveProvider(ctx, salonID, unprivilegedPlatformID); !errors.Is(err, pos.ErrNotFound) {
			t.Fatalf("active provider error=%v, want ErrNotFound", err)
		}
		knowledge, err := trainingRepository.ListKnowledge(ctx, salonID, unprivilegedPlatformID)
		if err != nil || len(knowledge) != 0 {
			t.Fatalf("knowledge=%#v err=%v, want empty", knowledge, err)
		}
		corrections, err := trainingRepository.ListCorrections(ctx, salonID, unprivilegedPlatformID)
		if err != nil || len(corrections) != 0 {
			t.Fatalf("corrections=%#v err=%v, want empty", corrections, err)
		}
	})
}

func insertActorFeatureAccessUser(t *testing.T, db *sql.DB, email string, principalScope string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO users (email, password_hash, full_name, status, principal_scope)
		VALUES ($1, 'integration-test-only', 'Actor Feature Access Test', 'active', $2)
		RETURNING id::text
	`, email, principalScope).Scan(&id); err != nil {
		t.Fatalf("insert user %s: %v", email, err)
	}
	return id
}

func insertActorFeatureAccessSalon(t *testing.T, db *sql.DB, ownerID string, suffix string) string {
	t.Helper()
	var id string
	if err := db.QueryRow(`
		INSERT INTO salons (name, phone, owner_user_id, active_pos_provider)
		VALUES ($1, $2, $3, 'square')
		RETURNING id::text
	`, "Actor Feature Access "+suffix, "+1312"+suffix[:7], ownerID).Scan(&id); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	return id
}
