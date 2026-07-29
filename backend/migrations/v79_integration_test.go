package migrations

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func TestV79PostgresPreflightRejectsExistingCallChildTenantMismatch(t *testing.T) {
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
	var sessionB string
	if err := tx.QueryRow(`INSERT INTO call_sessions(salon_id) VALUES($1) RETURNING id::text`, salonB).Scan(&sessionB); err != nil {
		t.Fatalf("insert session B: %v", err)
	}
	if _, err := tx.Exec(`
		ALTER TABLE call_transcript_messages
		DROP CONSTRAINT call_transcript_messages_salon_session_fkey,
		ADD CONSTRAINT call_transcript_messages_session_id_fkey
		FOREIGN KEY (session_id) REFERENCES call_sessions(id) ON DELETE CASCADE
	`); err != nil {
		t.Fatalf("restore pre-V79 transcript shape: %v", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO call_transcript_messages(session_id,salon_id,speaker,body,sequence)
		VALUES($1,$2,'customer','cross-tenant fixture',1)
	`, sessionB, salonA); err != nil {
		t.Fatalf("insert mismatch: %v", err)
	}
	raw, err := Files.ReadFile("V79__system_tenant_contract_preparation.sql")
	if err != nil {
		t.Fatalf("read V79: %v", err)
	}
	_, err = tx.Exec(string(raw))
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || string(pqErr.Code) != "23514" || pqErr.Constraint != "call_children_salon_call_session_preflight" {
		t.Fatalf("preflight error=%v", err)
	}
}

func TestV79WorkerClaimsRequireUnboundWorkerScopeAndPreserveTenantIdentity(t *testing.T) {
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
	for _, salonID := range []string{salonA, salonB} {
		if _, err := tx.Exec(`
			INSERT INTO pos_sync_jobs(salon_id,provider,entity_type,entity_id,operation)
			VALUES($1,'square','service',$2,'upsert_service')
		`, salonID, uuid.NewString()); err != nil {
			t.Fatalf("insert POS sync job: %v", err)
		}
	}

	if got := countV79ClaimedJobs(t, tx); got != 0 {
		t.Fatalf("unscoped claim count=%d, want 0", got)
	}
	setV79DatabaseContext(t, tx, "worker", "")
	rows, err := tx.Query(`SELECT salon_id::text FROM public.app_worker_claim_pos_sync_jobs(10)`)
	if err != nil {
		t.Fatalf("claim worker jobs: %v", err)
	}
	var claimed []string
	for rows.Next() {
		var salonID string
		if err := rows.Scan(&salonID); err != nil {
			rows.Close()
			t.Fatalf("scan claimed salon: %v", err)
		}
		claimed = append(claimed, salonID)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close claimed rows: %v", err)
	}
	sort.Strings(claimed)
	want := []string{salonA, salonB}
	sort.Strings(want)
	if len(claimed) != 2 || claimed[0] != want[0] || claimed[1] != want[1] {
		t.Fatalf("claimed salons=%v, want %v", claimed, want)
	}

	setV79DatabaseContext(t, tx, "worker", salonA)
	if got := countV79ClaimedJobs(t, tx); got != 0 {
		t.Fatalf("bound worker discovery count=%d, want 0", got)
	}
}

func TestV79CompositeCallChildKeysRejectCrossTenantWrites(t *testing.T) {
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
	var sessionB string
	if err := tx.QueryRow(`INSERT INTO call_sessions(salon_id) VALUES($1) RETURNING id::text`, salonB).Scan(&sessionB); err != nil {
		t.Fatalf("insert session B: %v", err)
	}
	statements := []string{
		`INSERT INTO call_transcript_messages(session_id,salon_id,speaker,body,sequence) VALUES($1,$2,'customer','blocked',1)`,
		`INSERT INTO handoff_requests(call_session_id,salon_id,reason,summary) VALUES($1,$2,'test','blocked')`,
		`INSERT INTO voice_webhook_events(call_session_id,salon_id,provider,event_type) VALUES($1,$2,'twilio','blocked')`,
		`INSERT INTO voice_audio_outputs(call_session_id,salon_id,provider,content_type,audio_data,expires_at) VALUES($1,$2,'openai','audio/mpeg','x'::bytea,now()+interval '1 minute')`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec("SAVEPOINT v79_child"); err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err := tx.Exec(statement, sessionB, salonA)
		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || (string(pqErr.Code) != "23503" && string(pqErr.Code) != "23514") {
			t.Fatalf("cross-tenant statement error=%v", err)
		}
		if _, err := tx.Exec("ROLLBACK TO SAVEPOINT v79_child"); err != nil {
			t.Fatalf("rollback savepoint: %v", err)
		}
	}
}

func insertV79Salons(t *testing.T, tx *sql.Tx) (string, string) {
	t.Helper()
	owners := make([]string, 2)
	for index := range owners {
		if err := tx.QueryRow(`
			INSERT INTO users(email,password_hash,full_name)
			VALUES($1,'v79-test','V79 owner') RETURNING id::text
		`, "v79-"+uuid.NewString()+"@example.test").Scan(&owners[index]); err != nil {
			t.Fatalf("insert owner: %v", err)
		}
	}
	salons := make([]string, 2)
	for index := range salons {
		if err := tx.QueryRow(`
			INSERT INTO salons(name,phone,owner_user_id)
			VALUES($1,$2,$3) RETURNING id::text
		`, "V79 Salon", "+1312555"+uuid.NewString()[0:4], owners[index]).Scan(&salons[index]); err != nil {
			t.Fatalf("insert salon: %v", err)
		}
	}
	return salons[0], salons[1]
}

func setV79DatabaseContext(t *testing.T, tx *sql.Tx, scope, salonID string) {
	t.Helper()
	if _, err := tx.Exec(`
		SELECT set_config('app.database_scope',$1,true),
		       set_config('app.system_salon_id',$2,true),
		       set_config('app.actor_user_id','',true)
	`, scope, salonID); err != nil {
		t.Fatalf("set database context: %v", err)
	}
}

func countV79ClaimedJobs(t *testing.T, tx *sql.Tx) int {
	t.Helper()
	rows, err := tx.Query(`SELECT job_id FROM public.app_worker_claim_pos_sync_jobs(10)`)
	if err != nil {
		t.Fatalf("claim jobs: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate claimed jobs: %v", err)
	}
	return count
}
