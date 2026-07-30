package openairuntimeverification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/openairuntime"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Enqueue(ctx context.Context, resolved openairuntime.ResolvedConfig, actorUserID string, req VerifyRequest, plans []capabilityPlan, fingerprint string) (*RunStatus, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var existingID, existingFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text, request_fingerprint
		FROM openai_runtime_verification_runs
		WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3
	`, resolved.SalonID, actorUserID, req.ActionKey).Scan(&existingID, &existingFingerprint)
	if err == nil {
		if existingFingerprint != fingerprint {
			return nil, false, ErrActionConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		status, err := r.GetByID(ctx, resolved.SalonID, existingID)
		return status, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM technical_resource_versions
		WHERE salon_id=$1 AND resource_type='integration_config' AND resource_id='openai'
	`, resolved.SalonID).Scan(&currentVersion); err != nil {
		return nil, false, err
	}
	if currentVersion != req.ExpectedConfigVersion || currentVersion != resolved.ConfigVersion {
		return nil, false, ErrVersionConflict
	}
	var runID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO openai_runtime_verification_runs (
			salon_id,integration_config_id,actor_user_id,action_key,request_fingerprint,
			config_version,credential_revision,destination_policy_version,verification_contract_version
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id::text
	`, resolved.SalonID, resolved.IntegrationConfigID, actorUserID, req.ActionKey, fingerprint,
		resolved.ConfigVersion, resolved.CredentialRevision, openairuntime.DestinationPolicyVersion,
		openairuntime.VerificationContract).Scan(&runID); err != nil {
		return nil, false, err
	}
	for _, plan := range plans {
		status := "pending"
		if !plan.Required {
			status = "not_required"
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO openai_runtime_verification_capabilities (salon_id,run_id,capability,required,status)
			VALUES ($1,$2,$3,$4,$5)
		`, resolved.SalonID, runID, plan.Capability, plan.Required, status); err != nil {
			return nil, false, err
		}
	}
	if err := insertEvent(ctx, tx, resolved.SalonID, runID, "queued", "queued", "", ""); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	status, err := r.GetByID(ctx, resolved.SalonID, runID)
	return status, false, err
}

func (r *Repository) Latest(ctx context.Context, salonID string) (*RunStatus, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text FROM openai_runtime_verification_runs
		WHERE salon_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1
	`, salonID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, salonID, id)
}

func (r *Repository) GetByID(ctx context.Context, salonID, runID string) (*RunStatus, error) {
	status := &RunStatus{}
	var storedStatus string
	err := r.db.QueryRowContext(ctx, `
		SELECT run.id::text,run.salon_id::text,run.status,run.config_version,run.credential_revision,
		       run.destination_policy_version,run.verification_contract_version,run.attempt_count,
		       COALESCE(run.error_code,''),run.started_at,run.completed_at,run.created_at,run.updated_at,
		       CASE WHEN version.version=run.config_version
		              AND config.credential_revision=run.credential_revision
		              AND config.destination_profile='openai_public'
		              AND run.destination_policy_version=$3
		              AND run.verification_contract_version=$4
		            THEN true ELSE false END
		FROM openai_runtime_verification_runs run
		JOIN salon_integration_configs config ON config.id=run.integration_config_id AND config.salon_id=run.salon_id
		JOIN technical_resource_versions version ON version.salon_id=run.salon_id
		 AND version.resource_type='integration_config' AND version.resource_id='openai'
		WHERE run.salon_id=$1 AND run.id=$2
	`, salonID, runID, openairuntime.DestinationPolicyVersion, openairuntime.VerificationContract).Scan(
		&status.ID, &status.SalonID, &storedStatus, &status.ConfigVersion, &status.CredentialRevision,
		&status.DestinationPolicyVersion, &status.VerificationContractVersion, &status.AttemptCount,
		&status.ErrorCode, &status.StartedAt, &status.CompletedAt, &status.CreatedAt, &status.UpdatedAt, &status.Fresh,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	status.Status = storedStatus
	if !status.Fresh && (storedStatus == "succeeded" || storedStatus == "failed") {
		status.Status = "stale"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT capability,required,status,latency_ms,COALESCE(provider_request_id,''),COALESCE(error_code,''),verified_at
		FROM openai_runtime_verification_capabilities
		WHERE salon_id=$1 AND run_id=$2 ORDER BY created_at,id
	`, salonID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item CapabilityStatus
		if err := rows.Scan(&item.Capability, &item.Required, &item.Status, &item.LatencyMS, &item.ProviderRequestID, &item.ErrorCode, &item.VerifiedAt); err != nil {
			return nil, err
		}
		if !status.Fresh && item.Status == "verified" {
			item.Status = "stale"
		}
		status.Capabilities = append(status.Capabilities, item)
	}
	return status, rows.Err()
}

func (r *Repository) Claim(ctx context.Context, limit int, lease time.Duration) ([]Claim, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT run_id::text,salon_id::text,claim_token::text
		FROM public.app_worker_claim_openai_runtime_verifications($1,$2)
	`, limit, lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var claims []Claim
	for rows.Next() {
		var claim Claim
		if err := rows.Scan(&claim.RunID, &claim.SalonID, &claim.ClaimToken); err != nil {
			return nil, err
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (r *Repository) LoadClaimed(ctx context.Context, claim Claim) (*claimedRun, error) {
	var run claimedRun
	err := r.db.QueryRowContext(ctx, `
		SELECT id::text,salon_id::text,integration_config_id::text,status,config_version,credential_revision,
		       destination_policy_version,verification_contract_version,attempt_count,claim_token::text,
		       COALESCE(error_code,''),started_at,completed_at,created_at,updated_at
		FROM openai_runtime_verification_runs
		WHERE id=$1 AND salon_id=$2 AND status='claimed' AND claim_token=$3 AND lease_expires_at>now()
	`, claim.RunID, claim.SalonID, claim.ClaimToken).Scan(
		&run.ID, &run.SalonID, &run.IntegrationConfigID, &run.Status, &run.ConfigVersion, &run.CredentialRevision,
		&run.DestinationPolicyVersion, &run.VerificationContractVersion, &run.AttemptCount, &run.ClaimToken,
		&run.ErrorCode, &run.StartedAt, &run.CompletedAt, &run.CreatedAt, &run.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT capability,required,status FROM openai_runtime_verification_capabilities
		WHERE salon_id=$1 AND run_id=$2 ORDER BY created_at,id
	`, claim.SalonID, claim.RunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var capability CapabilityStatus
		if err := rows.Scan(&capability.Capability, &capability.Required, &capability.Status); err != nil {
			return nil, err
		}
		run.Capabilities = append(run.Capabilities, capability)
	}
	return &run, rows.Err()
}

func (r *Repository) CompleteCapability(ctx context.Context, run *claimedRun, capability, status string, latency time.Duration, requestID, errorCode string) error {
	verifiedAt := any(nil)
	if status == "verified" {
		verifiedAt = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE openai_runtime_verification_capabilities capability
		SET status=$5,latency_ms=$6,provider_request_id=NULLIF($7,''),error_code=NULLIF($8,''),verified_at=$9,updated_at=now()
		FROM openai_runtime_verification_runs run
		WHERE capability.salon_id=$1 AND capability.run_id=$2 AND capability.capability=$3
		  AND run.id=capability.run_id AND run.salon_id=capability.salon_id
		  AND run.status='claimed' AND run.claim_token=$4 AND run.lease_expires_at>now()
	`, run.SalonID, run.ID, capability, run.ClaimToken, status, latency.Milliseconds(), safeValue(requestID), safeValue(errorCode), verifiedAt)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return r.InsertEvent(ctx, run.SalonID, run.ID, "capability_completed:"+run.ClaimToken+":"+capability, "capability_completed", run.Status, capability, errorCode)
}

func (r *Repository) Finish(ctx context.Context, run *claimedRun, status, errorCode string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE openai_runtime_verification_runs
		SET status=$4,error_code=NULLIF($5,''),claim_token=NULL,lease_expires_at=NULL,completed_at=now(),updated_at=now()
		WHERE id=$1 AND salon_id=$2 AND status='claimed' AND claim_token=$3
	`, run.ID, run.SalonID, run.ClaimToken, status, safeValue(errorCode))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return r.InsertEvent(ctx, run.SalonID, run.ID, status, status, status, "", errorCode)
}

func (r *Repository) InsertEvent(ctx context.Context, salonID, runID, eventKey, eventType, status, capability, errorCode string) error {
	fingerprint := eventFingerprint(eventType, status, capability, errorCode)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO openai_runtime_verification_events (
			salon_id,run_id,event_key,event_fingerprint,event_type,status,capability,error_code
		) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))
		ON CONFLICT (run_id,event_key) DO NOTHING
	`, salonID, runID, eventKey, fingerprint, eventType, status, capability, safeValue(errorCode))
	return err
}

func insertEvent(ctx context.Context, tx *sql.Tx, salonID, runID, eventType, status, capability, errorCode string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO openai_runtime_verification_events (
			salon_id,run_id,event_key,event_fingerprint,event_type,status,capability,error_code
		) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))
	`, salonID, runID, eventType, eventFingerprint(eventType, status, capability, errorCode), eventType, status, capability, safeValue(errorCode))
	return err
}

func eventFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func safeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:-", char) {
			continue
		}
		return ""
	}
	return value
}
