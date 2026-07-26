package pos_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/pos"
	"github.com/manleai/ai-receptionist/modules/training"
)

func TestServiceAliasOwnershipPostgresInvariant(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(12)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	fixture := newAliasOwnershipFixture(t, db)

	t.Run("parallel service then category permits exactly one active owner", func(t *testing.T) {
		assertParallelCrossTableAliasOwnership(t, fixture, "service", "category")
	})
	t.Run("parallel category then service permits exactly one active owner", func(t *testing.T) {
		assertParallelCrossTableAliasOwnership(t, fixture, "category", "service")
	})
	t.Run("same-table upserts remain idempotent", func(t *testing.T) {
		key := aliasOwnershipKey("same-service")
		serviceRepo := training.NewRepository(db)
		input := training.ServiceAliasInput{
			ServiceID: fixture.serviceIDs[0], Confidence: 1,
			Alias: key, Source: training.AliasSourceOwner, Status: training.AliasStatusActive,
		}
		first, err := serviceRepo.UpsertServiceAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, "", input)
		if err != nil {
			t.Fatalf("first service alias upsert: %v", err)
		}
		second, err := serviceRepo.UpsertServiceAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, "", input)
		if err != nil {
			t.Fatalf("second service alias upsert: %v", err)
		}
		if first.ID != second.ID {
			t.Fatalf("service alias IDs differ: first=%q second=%q", first.ID, second.ID)
		}

		categoryKey := aliasOwnershipKey("same-category")
		categoryRepo := pos.NewRepository(db)
		categoryInput := pos.ServiceCategoryAliasMutation{
			CategoryID: fixture.categoryIDs[0], Alias: categoryKey,
			NormalizedAlias: categoryKey, Confidence: 1,
		}
		firstCategory, err := categoryRepo.UpsertServiceCategoryAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, categoryInput)
		if err != nil {
			t.Fatalf("first category alias upsert: %v", err)
		}
		secondCategory, err := categoryRepo.UpsertServiceCategoryAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, categoryInput)
		if err != nil {
			t.Fatalf("second category alias upsert: %v", err)
		}
		if firstCategory.ID != secondCategory.ID {
			t.Fatalf("category alias IDs differ: first=%q second=%q", firstCategory.ID, secondCategory.ID)
		}
	})
	t.Run("repository conflicts map to typed validation", func(t *testing.T) {
		categoryOwnedKey := aliasOwnershipKey("category-owned")
		insertRawAlias(t, fixture.ctx, db, "category", fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], categoryOwnedKey)
		_, err := training.NewRepository(db).UpsertServiceAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, "", training.ServiceAliasInput{
			ServiceID: fixture.serviceIDs[0], Alias: categoryOwnedKey,
			Source: training.AliasSourceOwner, Status: training.AliasStatusActive, Confidence: 1,
		})
		if !errors.Is(err, training.ErrValidation) {
			t.Fatalf("training conflict error=%v, want ErrValidation", err)
		}

		serviceOwnedKey := aliasOwnershipKey("service-owned")
		insertRawAlias(t, fixture.ctx, db, "service", fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], serviceOwnedKey)
		_, err = pos.NewRepository(db).UpsertServiceCategoryAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, pos.ServiceCategoryAliasMutation{
			CategoryID: fixture.categoryIDs[0], Alias: serviceOwnedKey,
			NormalizedAlias: serviceOwnedKey, Confidence: 1,
		})
		if !errors.Is(err, pos.ErrValidation) {
			t.Fatalf("POS conflict error=%v, want ErrValidation", err)
		}
	})
	t.Run("deactivation transfers ownership between namespaces", func(t *testing.T) {
		key := aliasOwnershipKey("deactivate-transfer")
		serviceRepo := training.NewRepository(db)
		active := training.ServiceAliasInput{
			ServiceID: fixture.serviceIDs[0], Alias: key,
			Source: training.AliasSourceOwner, Status: training.AliasStatusActive, Confidence: 1,
		}
		if _, err := serviceRepo.UpsertServiceAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, "", active); err != nil {
			t.Fatalf("create service alias: %v", err)
		}
		active.Status = training.AliasStatusArchived
		if _, err := serviceRepo.UpsertServiceAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, "", active); err != nil {
			t.Fatalf("archive service alias: %v", err)
		}
		if _, err := pos.NewRepository(db).UpsertServiceCategoryAlias(fixture.ctx, fixture.salonIDs[0], fixture.ownerID, pos.ServiceCategoryAliasMutation{
			CategoryID: fixture.categoryIDs[0], Alias: key, NormalizedAlias: key, Confidence: 1,
		}); err != nil {
			t.Fatalf("transfer alias to category: %v", err)
		}
		assertActiveAliasOwnerCount(t, fixture.ctx, db, fixture.salonIDs[0], key, 1)
	})
	t.Run("same normalized alias is allowed in different salons", func(t *testing.T) {
		key := aliasOwnershipKey("cross-tenant")
		insertRawAlias(t, fixture.ctx, db, "service", fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], key)
		insertRawAlias(t, fixture.ctx, db, "category", fixture.salonIDs[1], fixture.serviceIDs[1], fixture.categoryIDs[1], key)
		assertActiveAliasOwnerCount(t, fixture.ctx, db, fixture.salonIDs[0], key, 1)
		assertActiveAliasOwnerCount(t, fixture.ctx, db, fixture.salonIDs[1], key, 1)
	})
	t.Run("rollback releases ownership lock and uncommitted row", func(t *testing.T) {
		key := aliasOwnershipKey("rollback")
		tx, err := db.BeginTx(fixture.ctx, nil)
		if err != nil {
			t.Fatalf("begin transaction: %v", err)
		}
		if _, err := tx.ExecContext(fixture.ctx, `SELECT public.lock_service_alias_ownership($1::uuid, $2)`, fixture.salonIDs[0], key); err != nil {
			_ = tx.Rollback()
			t.Fatalf("lock ownership: %v", err)
		}
		if err := insertRawAliasWithExecutor(fixture.ctx, tx, "service", fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], key); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert uncommitted service alias: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}

		ctx, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
		defer cancel()
		if err := insertRawAliasWithExecutor(ctx, db, "category", fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], key); err != nil {
			t.Fatalf("category insert after rollback: %v", err)
		}
		assertActiveAliasOwnerCount(t, fixture.ctx, db, fixture.salonIDs[0], key, 1)
	})
	t.Run("normalized ownership key is immutable", func(t *testing.T) {
		key := aliasOwnershipKey("immutable")
		insertRawAlias(t, fixture.ctx, db, "service", fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], key)
		_, err := db.ExecContext(fixture.ctx, `
			UPDATE service_aliases
			SET normalized_alias = $1
			WHERE salon_id = $2 AND normalized_alias = $3
		`, key+"changed", fixture.salonIDs[0], key)
		assertPostgresConstraint(t, err, "service_alias_ownership_key_immutable")
	})
}

type aliasOwnershipFixture struct {
	ctx         context.Context
	db          *sql.DB
	ownerID     string
	salonIDs    [2]string
	serviceIDs  [2]string
	categoryIDs [2]string
}

func newAliasOwnershipFixture(t *testing.T, db *sql.DB) aliasOwnershipFixture {
	t.Helper()
	fixture := aliasOwnershipFixture{ctx: context.Background(), db: db, ownerID: uuid.NewString()}
	if _, err := db.ExecContext(fixture.ctx, `
		INSERT INTO users (id, email, password_hash, full_name)
		VALUES ($1, $2, 'integration-test', 'Alias Ownership Test Owner')
	`, fixture.ownerID, fixture.ownerID+"@example.test"); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	for i := range fixture.salonIDs {
		fixture.salonIDs[i] = uuid.NewString()
		fixture.serviceIDs[i] = uuid.NewString()
		fixture.categoryIDs[i] = uuid.NewString()
		if _, err := db.ExecContext(fixture.ctx, `
			INSERT INTO salons (id, name, phone, owner_user_id)
			VALUES ($1, $2, $3, $4)
		`, fixture.salonIDs[i], fmt.Sprintf("Alias Ownership Salon %d", i), fmt.Sprintf("+155500001%02d", i), fixture.ownerID); err != nil {
			t.Fatalf("insert salon %d: %v", i, err)
		}
		if _, err := db.ExecContext(fixture.ctx, `
			INSERT INTO services (id, salon_id, pos_provider, pos_service_id, name, duration_minutes, active)
			VALUES ($1, $2, 'square', $3, $4, 45, true)
		`, fixture.serviceIDs[i], fixture.salonIDs[i], "alias-ownership-"+fixture.serviceIDs[i], fmt.Sprintf("Alias Test Service %d", i)); err != nil {
			t.Fatalf("insert service %d: %v", i, err)
		}
		if _, err := db.ExecContext(fixture.ctx, `
			INSERT INTO service_categories (id, salon_id, name, slug, source, status)
			VALUES ($1, $2, $3, $4, 'manual', 'active')
		`, fixture.categoryIDs[i], fixture.salonIDs[i], fmt.Sprintf("Alias Test Category %d", i), "alias-test-"+fixture.categoryIDs[i]); err != nil {
			t.Fatalf("insert category %d: %v", i, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE owner_user_id = $1`, fixture.ownerID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, fixture.ownerID)
	})
	return fixture
}

func assertParallelCrossTableAliasOwnership(t *testing.T, fixture aliasOwnershipFixture, firstKind string, secondKind string) {
	t.Helper()
	key := aliasOwnershipKey(firstKind + "-then-" + secondKind)
	tx, err := fixture.db.BeginTx(fixture.ctx, nil)
	if err != nil {
		t.Fatalf("begin first transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(fixture.ctx, `SELECT public.lock_service_alias_ownership($1::uuid, $2)`, fixture.salonIDs[0], key); err != nil {
		t.Fatalf("lock first ownership: %v", err)
	}
	if err := insertRawAliasWithExecutor(fixture.ctx, tx, firstKind, fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], key); err != nil {
		t.Fatalf("insert first %s alias: %v", firstKind, err)
	}

	result := make(chan error, 1)
	go func() {
		result <- insertRawAliasWithExecutor(fixture.ctx, fixture.db, secondKind, fixture.salonIDs[0], fixture.serviceIDs[0], fixture.categoryIDs[0], key)
	}()
	select {
	case earlyErr := <-result:
		t.Fatalf("second %s write completed before first transaction released ownership lock: %v", secondKind, earlyErr)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit first %s alias: %v", firstKind, err)
	}
	select {
	case err := <-result:
		assertPostgresConstraint(t, err, "service_alias_cross_table_active_unique")
	case <-time.After(2 * time.Second):
		t.Fatalf("second %s write remained blocked after first transaction committed", secondKind)
	}
	assertActiveAliasOwnerCount(t, fixture.ctx, fixture.db, fixture.salonIDs[0], key, 1)
}

type aliasSQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertRawAlias(t *testing.T, ctx context.Context, executor aliasSQLExecutor, kind string, salonID string, serviceID string, categoryID string, key string) {
	t.Helper()
	if err := insertRawAliasWithExecutor(ctx, executor, kind, salonID, serviceID, categoryID, key); err != nil {
		t.Fatalf("insert %s alias %q: %v", kind, key, err)
	}
}

func insertRawAliasWithExecutor(ctx context.Context, executor aliasSQLExecutor, kind string, salonID string, serviceID string, categoryID string, key string) error {
	switch kind {
	case "service":
		_, err := executor.ExecContext(ctx, `
			INSERT INTO service_aliases (salon_id, service_id, alias, normalized_alias, source, status, confidence)
			VALUES ($1, $2, $3, $3, 'owner', 'active', 1.000)
		`, salonID, serviceID, key)
		return err
	case "category":
		_, err := executor.ExecContext(ctx, `
			INSERT INTO service_category_aliases (salon_id, category_id, alias, normalized_alias, source, status, confidence)
			VALUES ($1, $2, $3, $3, 'owner', 'active', 1.000)
		`, salonID, categoryID, key)
		return err
	default:
		return fmt.Errorf("unsupported alias kind %q", kind)
	}
}

func assertActiveAliasOwnerCount(t *testing.T, ctx context.Context, db *sql.DB, salonID string, key string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM service_aliases WHERE salon_id = $1 AND normalized_alias = $2 AND status = 'active') +
			(SELECT COUNT(*) FROM service_category_aliases WHERE salon_id = $1 AND normalized_alias = $2 AND status = 'active')
	`, salonID, key).Scan(&count); err != nil {
		t.Fatalf("count active alias owners: %v", err)
	}
	if count != want {
		t.Fatalf("active alias owner count=%d, want %d for salon=%q key=%q", count, want, salonID, key)
	}
}

func assertPostgresConstraint(t *testing.T, err error, wantConstraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error=nil, want PostgreSQL constraint %q", wantConstraint)
	}
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("error=%T %v, want *pq.Error", err, err)
	}
	if pqErr.Constraint != wantConstraint {
		t.Fatalf("constraint=%q code=%q, want %q", pqErr.Constraint, pqErr.Code, wantConstraint)
	}
}

func aliasOwnershipKey(prefix string) string {
	return strings.ReplaceAll(prefix, "-", "") + strings.ReplaceAll(uuid.NewString(), "-", "")
}
