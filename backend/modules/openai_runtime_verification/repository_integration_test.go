package openairuntimeverification

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/manleai/ai-receptionist/internal/config"
	"github.com/manleai/ai-receptionist/internal/openairuntime"
)

func TestRepositoryPostgresReplayVersionFenceAndStaleEvidence(t *testing.T) {
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

	suffix := uuid.NewString()
	var actorID, salonID, configID string
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users(email,password_hash,full_name)
		VALUES($1,'verification-test','OpenAI verification actor') RETURNING id::text
	`, suffix+"@verification.example.test").Scan(&actorID); err != nil {
		t.Fatalf("insert actor: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salons(name,phone,owner_user_id)
		VALUES('OpenAI verification salon',$1,$2) RETURNING id::text
	`, "+1312"+suffix[0:7], actorID).Scan(&salonID); err != nil {
		t.Fatalf("insert salon: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO salon_integration_configs (
			salon_id,provider,enabled,settings,credential_fingerprint_hmac,credential_revision,destination_profile
		) VALUES (
			$1,'openai',true,$2::jsonb,$3,1,'openai_public'
		) RETURNING id::text
	`, salonID,
		`{"base_url":"https://api.openai.com/v1","transcription_model":"transcribe","reply_model":"reply","speech_model":"speech","speech_voice":"voice"}`,
		"abababababababababababababababababababababababababababababababab").Scan(&configID); err != nil {
		t.Fatalf("insert OpenAI config: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE technical_resource_versions SET version=4
		WHERE salon_id=$1 AND resource_type='integration_config' AND resource_id='openai'
	`, salonID); err != nil {
		t.Fatalf("set technical version: %v", err)
	}

	resolved := openairuntime.ResolvedConfig{
		SalonID: salonID, IntegrationConfigID: configID, ConfigVersion: 4,
		CredentialRevision: 1, CredentialIdentityEstablished: true,
		DestinationProfile: openairuntime.DestinationProfile, Enabled: true,
		Config: config.OpenAIVoiceConfig{
			APIKey: "test-key", BaseURL: openairuntime.CanonicalBaseURL,
			TranscriptionModel: "transcribe", ReplyModel: "reply",
			SpeechModel: "speech", SpeechVoice: "voice",
		},
	}
	request := VerifyRequest{ActionKey: "verify-" + suffix, ExpectedConfigVersion: 4}
	plans := verificationPlan(resolved)
	fingerprint, err := requestFingerprint(request, resolved, plans)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db)
	created, replayed, err := repository.Enqueue(ctx, resolved, actorID, request, plans, fingerprint)
	if err != nil || replayed || created == nil {
		t.Fatalf("create run=%#v replayed=%t err=%v", created, replayed, err)
	}
	replayedRun, replayed, err := repository.Enqueue(ctx, resolved, actorID, request, plans, fingerprint)
	if err != nil || !replayed || replayedRun.ID != created.ID {
		t.Fatalf("exact replay=%#v replayed=%t err=%v", replayedRun, replayed, err)
	}
	changed := request
	changed.ExpectedConfigVersion = 5
	changedFingerprint, _ := requestFingerprint(changed, resolved, plans)
	if _, _, err := repository.Enqueue(ctx, resolved, actorID, changed, plans, changedFingerprint); !errors.Is(err, ErrActionConflict) {
		t.Fatalf("changed action replay error=%v", err)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE technical_resource_versions SET version=5
		WHERE salon_id=$1 AND resource_type='integration_config' AND resource_id='openai'
	`, salonID); err != nil {
		t.Fatalf("advance config fence: %v", err)
	}
	stale, err := repository.GetByID(ctx, salonID, created.ID)
	if err != nil || stale.Fresh {
		t.Fatalf("stale evidence=%#v err=%v", stale, err)
	}
	request.ActionKey = "verify-after-change-" + suffix
	request.ExpectedConfigVersion = 4
	fingerprint, _ = requestFingerprint(request, resolved, plans)
	if _, _, err := repository.Enqueue(ctx, resolved, actorID, request, plans, fingerprint); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("concurrent config fence error=%v", err)
	}
}
