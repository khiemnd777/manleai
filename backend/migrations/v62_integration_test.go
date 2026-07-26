package migrations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestV62PostgresPreflightRejectsExistingTenantMismatch(t *testing.T) {
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	var ownerA, ownerB string
	for label, target := range map[string]*string{"a": &ownerA, "b": &ownerB} {
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO users(email,password_hash,full_name)
			VALUES($1,'v62-test',$2) RETURNING id::text
		`, "v62-"+label+"-"+uuid.NewString()+"@example.test", "V62 "+label).Scan(target); err != nil {
			t.Fatalf("insert owner %s: %v", label, err)
		}
	}
	var salonA, salonB string
	if err := tx.QueryRowContext(ctx, `INSERT INTO salons(name,phone,owner_user_id) VALUES('V62 A','+13125550131',$1) RETURNING id::text`, ownerA).Scan(&salonA); err != nil {
		t.Fatalf("insert salon A: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO salons(name,phone,owner_user_id) VALUES('V62 B','+13125550132',$1) RETURNING id::text`, ownerB).Scan(&salonB); err != nil {
		t.Fatalf("insert salon B: %v", err)
	}
	var sessionB string
	if err := tx.QueryRowContext(ctx, `INSERT INTO call_sessions(salon_id) VALUES($1) RETURNING id::text`, salonB).Scan(&sessionB); err != nil {
		t.Fatalf("insert session B: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		ALTER TABLE party_booking_requests
		DROP CONSTRAINT party_booking_requests_salon_call_session_fkey,
		ADD CONSTRAINT party_booking_requests_call_session_id_fkey
		FOREIGN KEY (call_session_id) REFERENCES call_sessions(id) ON DELETE CASCADE
	`); err != nil {
		t.Fatalf("restore pre-V62 shape: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO party_booking_requests(salon_id,call_session_id,event_key,summary)
		VALUES($1,$2,$3,'cross-tenant preflight fixture')
	`, salonA, sessionB, "v62-mismatch-"+uuid.NewString()); err != nil {
		t.Fatalf("insert preflight mismatch: %v", err)
	}
	raw, err := Files.ReadFile("V62__party_booking_request_tenant_integrity.sql")
	if err != nil {
		t.Fatalf("read V62: %v", err)
	}
	_, err = tx.ExecContext(ctx, string(raw))
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != "23514" || pqErr.Constraint != "party_booking_requests_salon_call_session_preflight" {
		t.Fatalf("preflight error=%v", err)
	}
}
