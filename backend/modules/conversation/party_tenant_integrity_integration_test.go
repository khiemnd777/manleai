package conversation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestPostgresPartyRequestTenantFenceHydrationAndRedaction(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	ownerA, salonA, sessionA := seedPartyTenantSession(t, ctx, db, "a")
	ownerB, salonB, sessionB := seedPartyTenantSession(t, ctx, db, "b")
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM salons WHERE id IN ($1,$2)`, salonA, salonB)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerA, ownerB)
	})

	requestA := insertPartyTenantRequest(t, ctx, db, salonA, sessionA, "Alice", "request-a")
	requestB := insertPartyTenantRequest(t, ctx, db, salonB, sessionB, "Bob", "request-b")

	_, err = db.ExecContext(ctx, `
		INSERT INTO party_booking_requests(salon_id,call_session_id,event_key,summary)
		VALUES($1,$2,$3,'must be rejected')
	`, salonA, sessionB, "cross-tenant-"+uuid.NewString())
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != "23503" || pqErr.Constraint != "party_booking_requests_salon_call_session_fkey" {
		t.Fatalf("cross-tenant insert error=%v", err)
	}

	repository := NewRepository(db)
	hydrated, err := repository.latestPartyRequest(ctx, salonA, sessionA)
	if err != nil || hydrated == nil || hydrated.ID != requestA || hydrated.SalonID != salonA {
		t.Fatalf("tenant hydration=%#v err=%v", hydrated, err)
	}
	if crossTenant, err := repository.latestPartyRequest(ctx, salonB, sessionA); err != nil || crossTenant != nil {
		t.Fatalf("cross-tenant hydration=%#v err=%v", crossTenant, err)
	}

	if err := redactSessionInTx(ctx, db, sessionA, salonA); err != nil {
		t.Fatalf("redact salon A session: %v", err)
	}
	assertPartyTenantRedaction(t, ctx, db, requestA, true)
	assertPartyTenantRedaction(t, ctx, db, requestB, false)
}

func seedPartyTenantSession(t *testing.T, ctx context.Context, db *sql.DB, label string) (string, string, string) {
	t.Helper()
	var ownerID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'party-tenant-test',$2) RETURNING id::text
	`, "party-tenant-"+label+"-"+uuid.NewString()+"@example.test", "Party tenant "+label).Scan(&ownerID); err != nil {
		t.Fatalf("insert owner %s: %v", label, err)
	}
	var salonID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,timezone,owner_user_id)
		VALUES($1,$2,'America/Chicago',$3) RETURNING id::text
	`, "Party tenant "+label, "+13125550"+map[string]string{"a": "121", "b": "122"}[label], ownerID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon %s: %v", label, err)
	}
	var sessionID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO call_sessions(salon_id,channel,status,intent,outcome)
		VALUES($1,'simulator','completed','book','pending_owner_review') RETURNING id::text
	`, salonID).Scan(&sessionID); err != nil {
		t.Fatalf("insert call session %s: %v", label, err)
	}
	return ownerID, salonID, sessionID
}

func insertPartyTenantRequest(t *testing.T, ctx context.Context, db *sql.DB, salonID, sessionID, representativeName, suffix string) string {
	t.Helper()
	var requestID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO party_booking_requests(
			salon_id,call_session_id,event_key,party_size,representative_name,
			representative_phone,guest_service_requests,flexibility_notes,summary
		) VALUES($1,$2,$3,2,$4,'+13125550123','[{"guest_reference":"private guest"}]'::jsonb,'private note','private summary')
		RETURNING id::text
	`, salonID, sessionID, suffix+"-"+uuid.NewString(), representativeName).Scan(&requestID); err != nil {
		t.Fatalf("insert party request %s: %v", suffix, err)
	}
	return requestID
}

func assertPartyTenantRedaction(t *testing.T, ctx context.Context, db *sql.DB, requestID string, redacted bool) {
	t.Helper()
	var name, phone, notes sql.NullString
	var guestRequests []byte
	var summary string
	if err := db.QueryRowContext(ctx, `
		SELECT representative_name,representative_phone,guest_service_requests,flexibility_notes,summary
		FROM party_booking_requests WHERE id=$1
	`, requestID).Scan(&name, &phone, &guestRequests, &notes, &summary); err != nil {
		t.Fatalf("load party request %s: %v", requestID, err)
	}
	if redacted {
		if name.Valid || phone.Valid || string(guestRequests) != "[]" || notes.Valid || summary != redactedSummaryBody {
			t.Fatalf("redacted party request=%v/%v/%s/%v/%q", name, phone, guestRequests, notes, summary)
		}
		return
	}
	if !name.Valid || !phone.Valid || string(guestRequests) == "[]" || !notes.Valid || summary == redactedSummaryBody {
		t.Fatalf("other-tenant party request changed=%v/%v/%s/%v/%q", name, phone, guestRequests, notes, summary)
	}
}
