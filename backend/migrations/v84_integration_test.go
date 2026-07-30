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

func TestV84OpenAICredentialIdentityIsExclusiveAcrossTenants(t *testing.T) {
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
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	insertV84OpenAIConfig(t, tx, salonA, fingerprint)
	_, err = tx.Exec(`
		INSERT INTO salon_integration_configs (
			id,salon_id,provider,enabled,settings,credential_fingerprint_hmac,credential_revision,destination_profile
		) VALUES ($1,$2,'openai',true,'{}'::jsonb,$3,1,'openai_public')
	`, uuid.NewString(), salonB, fingerprint)
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) || string(postgresError.Code) != "23505" || postgresError.Constraint != "idx_openai_unique_credential_identity" {
		t.Fatalf("duplicate OpenAI credential identity error=%v", err)
	}
}

func TestV84VerificationRunRejectsCrossTenantConfigIdentity(t *testing.T) {
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
	configB := insertV84OpenAIConfig(t, tx, salonB, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	var actorA string
	if err := tx.QueryRow(`SELECT owner_user_id::text FROM salons WHERE id=$1`, salonA).Scan(&actorA); err != nil {
		t.Fatalf("load tenant A actor: %v", err)
	}
	_, err = tx.Exec(`
		INSERT INTO openai_runtime_verification_runs (
			salon_id,integration_config_id,actor_user_id,action_key,request_fingerprint,
			config_version,credential_revision,destination_policy_version,verification_contract_version
		) VALUES ($1,$2,$3,'cross-tenant-test',$4,1,1,'openai-public-v1','openai-voice-v1')
	`, salonA, configB, actorA, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	var postgresError *pq.Error
	if !errors.As(err, &postgresError) || string(postgresError.Code) != "23503" || postgresError.Constraint != "openai_verification_config_tenant_fk" {
		t.Fatalf("cross-tenant verification config error=%v", err)
	}
}

func TestV84ConfigurationTransferRunAcceptsV10WithoutRewritingHistory(t *testing.T) {
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

	salonID, _ := insertV79Salons(t, tx)
	var actorID string
	if err := tx.QueryRow(`SELECT owner_user_id::text FROM salons WHERE id=$1`, salonID).Scan(&actorID); err != nil {
		t.Fatalf("load actor: %v", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO configuration_transfer_runs (
			salon_id,source_type,actor_user_id,schema_version,included_sections,
			source_fingerprint,request_fingerprint,target_scheduling_authority,
			target_scheduling_authority_version,status
		) VALUES (
			$1,'json_upload',$2,'manleai.salon_configuration.v10',ARRAY['integrations'],
			$3,$4,'owner_manual',1,'previewed'
		)
	`, salonID, actorID,
		"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"); err != nil {
		t.Fatalf("insert v10 configuration transfer run: %v", err)
	}
	for _, historical := range []string{"manleai.salon_configuration.v8", "manleai.salon_configuration.v9"} {
		if _, err := tx.Exec(`
			INSERT INTO configuration_transfer_runs (
				salon_id,source_type,actor_user_id,schema_version,included_sections,
				source_fingerprint,request_fingerprint,target_scheduling_authority,
				target_scheduling_authority_version,status
			) VALUES ($1,'json_upload',$2,$3,ARRAY['integrations'],$4,$5,'owner_manual',1,'previewed')
		`, salonID, actorID, historical,
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			"1111111111111111111111111111111111111111111111111111111111111111"); err != nil {
			t.Fatalf("insert historical configuration transfer run %q: %v", historical, err)
		}
	}
}

func insertV84OpenAIConfig(t *testing.T, tx *sql.Tx, salonID, fingerprint string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(`
		INSERT INTO salon_integration_configs (
			id,salon_id,provider,enabled,settings,credential_fingerprint_hmac,credential_revision,destination_profile
		) VALUES ($1,$2,'openai',true,'{}'::jsonb,$3,1,'openai_public')
		RETURNING id::text
	`, uuid.NewString(), salonID, fingerprint).Scan(&id); err != nil {
		t.Fatalf("insert OpenAI config: %v", err)
	}
	return id
}
