package scheduling_authority_switch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

const operationLockPrefix = "scheduling-authority-switch-preview:"

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) FindByOperationKey(ctx context.Context, salonID string, ownerUserID string, operationKey string) (*SwitchRun, error) {
	return scanSwitchRun(r.db.QueryRowContext(ctx, switchRunSelect+`
		WHERE run.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
		  AND run.operation_key = $3
	`, salonID, ownerUserID, operationKey))
}

func (r *Repository) CurrentAuthority(ctx context.Context, salonID string, ownerUserID string) (authorityState, error) {
	var state authorityState
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority, settings.scheduling_authority_version
		FROM salon_settings settings
		JOIN salons salon ON salon.id = settings.salon_id
		WHERE settings.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
	`, salonID, ownerUserID).Scan(&state.Authority, &state.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return state, ErrNotFound
	}
	return state, err
}

func (r *Repository) EligibleServiceCount(ctx context.Context, salonID string, ownerUserID string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM services service
		JOIN salons salon ON salon.id = service.salon_id
		WHERE service.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
		  AND service.active
		  AND service.ai_bookable
		  AND service.archived_at IS NULL
		  AND service.duration_minutes > 0
	`, salonID, ownerUserID).Scan(&count)
	return count, err
}

func (r *Repository) BookingMode(ctx context.Context, salonID string, ownerUserID string) (string, error) {
	var mode string
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.booking_mode
		FROM salon_settings settings
		JOIN salons salon ON salon.id=settings.salon_id
		WHERE settings.salon_id::text=$1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
	`, salonID, ownerUserID).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return mode, err
}

func (r *Repository) CreateOrReplayPreview(ctx context.Context, input persistPreviewInput) (*SwitchRun, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, classifySwitchConstraint(err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, operationLockPrefix+input.SalonID+":"+input.OperationKey); err != nil {
		return nil, false, err
	}
	existing, err := scanSwitchRun(tx.QueryRowContext(ctx, switchRunSelect+`
		WHERE run.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
		  AND run.operation_key = $3
		FOR UPDATE OF run
	`, input.SalonID, input.OwnerUserID, input.OperationKey))
	if err == nil {
		if existing.payloadFingerprint != input.PayloadFingerprint {
			return nil, false, ErrOperationConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}

	var current authorityState
	err = tx.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority, settings.scheduling_authority_version
		FROM salon_settings settings
		JOIN salons salon ON salon.id = settings.salon_id
		WHERE settings.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
		FOR SHARE OF settings, salon
	`, input.SalonID, input.OwnerUserID).Scan(&current.Authority, &current.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, classifySwitchConstraint(err)
	}
	if current.Authority != input.SourceSchedulingAuthority || current.Version != input.ExpectedSourceAuthorityVersion {
		return nil, false, ErrVersionConflict
	}

	readinessJSON, err := json.Marshal(input.ReadinessSnapshot)
	if err != nil {
		return nil, false, err
	}
	blockersJSON, err := json.Marshal(input.Blockers)
	if err != nil {
		return nil, false, err
	}
	var runID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO scheduling_authority_switch_runs (
			salon_id, source_scheduling_authority, target_scheduling_authority,
			expected_source_authority_version, operation_key, payload_fingerprint,
			readiness_snapshot, blockers, actor_user_id, status, blocked_at, rollback_of_switch_run_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10,
		        CASE WHEN $10 = 'preview_blocked' THEN now() ELSE NULL END, NULLIF($11, '')::uuid)
		RETURNING id::text
	`, input.SalonID, input.SourceSchedulingAuthority, input.TargetSchedulingAuthority,
		input.ExpectedSourceAuthorityVersion, input.OperationKey, input.PayloadFingerprint,
		string(readinessJSON), string(blockersJSON), input.OwnerUserID, input.Status, input.RollbackOfSwitchRunID).Scan(&runID)
	if err != nil {
		return nil, false, classifySwitchConstraint(err)
	}
	eventPayload, err := json.Marshal(map[string]any{
		"status":                      input.Status,
		"target_scheduling_authority": input.TargetSchedulingAuthority,
		"ready":                       input.ReadinessSnapshot.Ready,
		"blocker_count":               len(input.Blockers),
	})
	if err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO scheduling_authority_switch_events (
			salon_id, switch_run_id, action_key, action_fingerprint,
			event_type, actor_user_id, payload
		)
		VALUES ($1, $2, $3, $4, 'preview', $5, $6::jsonb)
	`, input.SalonID, runID, input.OperationKey, input.PayloadFingerprint, input.OwnerUserID, string(eventPayload)); err != nil {
		return nil, false, classifySwitchConstraint(err)
	}
	run, err := getSwitchRunTx(ctx, tx, input.SalonID, input.OwnerUserID, runID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, classifySwitchConstraint(err)
	}
	return run, false, nil
}

func (r *Repository) Latest(ctx context.Context, salonID string, ownerUserID string) (*SwitchRun, error) {
	return scanSwitchRun(r.db.QueryRowContext(ctx, switchRunSelect+`
		WHERE run.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.read')
		  )
		ORDER BY run.created_at DESC, run.id DESC
		LIMIT 1
	`, salonID, ownerUserID))
}

func (r *Repository) Get(ctx context.Context, salonID string, ownerUserID string, runID string) (*SwitchRun, error) {
	return scanSwitchRun(r.db.QueryRowContext(ctx, switchRunSelect+`
		WHERE run.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.read')
		  )
		  AND run.id::text = $3
	`, salonID, ownerUserID, runID))
}

func (r *Repository) ReplayCommit(ctx context.Context, salonID string, ownerUserID string, runID string, actionKey string, actionFingerprint string) (*SwitchRun, bool, error) {
	var storedFingerprint string
	err := r.db.QueryRowContext(ctx, `
		SELECT event.action_fingerprint
		FROM scheduling_authority_switch_events event
		JOIN scheduling_authority_switch_runs run ON run.id = event.switch_run_id AND run.salon_id = event.salon_id
		JOIN salons salon ON salon.id = run.salon_id
		WHERE run.id::text = $1 AND run.salon_id::text = $2
		  AND (
		      public.has_active_tenant_membership(salon.id, $3::uuid)
		      OR public.has_platform_salon_capability(salon.id, $3::uuid, 'technical.write')
		  )
		  AND event.action_key = $4 AND event.event_type = 'commit'
	`, runID, salonID, ownerUserID, actionKey).Scan(&storedFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedFingerprint != actionFingerprint {
		return nil, false, ErrOperationConflict
	}
	run, err := r.Get(ctx, salonID, ownerUserID, runID)
	return run, true, err
}

func (r *Repository) Commit(ctx context.Context, input commitInput) (*SwitchRun, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(input.SalonID)); err != nil {
		return nil, false, err
	}
	run, err := scanSwitchRun(tx.QueryRowContext(ctx, switchRunSelect+`
		WHERE run.id::text = $1 AND run.salon_id::text = $2
		  AND (
		      public.has_active_tenant_membership(salon.id, $3::uuid)
		      OR public.has_platform_salon_capability(salon.id, $3::uuid, 'technical.write')
		  )
		FOR UPDATE OF run
	`, input.RunID, input.SalonID, input.OwnerUserID))
	if err != nil {
		return nil, false, err
	}
	var storedFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT action_fingerprint FROM scheduling_authority_switch_events WHERE switch_run_id = $1 AND action_key = $2 AND event_type = 'commit'`, input.RunID, input.ActionKey).Scan(&storedFingerprint)
	if err == nil {
		if storedFingerprint != input.ActionFingerprint {
			return nil, false, ErrOperationConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, false, err
		}
		return run, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if run.Status != StatusPreviewReady {
		return nil, false, ErrStateConflict
	}
	var current authorityState
	err = tx.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority, settings.scheduling_authority_version
		FROM salon_settings settings JOIN salons salon ON salon.id = settings.salon_id
		WHERE settings.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
		FOR UPDATE OF settings, salon
	`, input.SalonID, input.OwnerUserID).Scan(&current.Authority, &current.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if current.Authority != run.SourceSchedulingAuthority || current.Version != run.ExpectedSourceAuthorityVersion {
		return nil, false, ErrVersionConflict
	}
	var liveExternal bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM booking_attempts attempt
			WHERE attempt.salon_id::text = $1 AND attempt.scheduling_authority = 'external_provider'
			  AND attempt.status = 'pos_pending' AND attempt.superseded_at IS NULL
			  AND attempt.provider_outcome IN ('not_started','in_flight')
			  AND (NULLIF(BTRIM(attempt.processing_token), '') IS NOT NULL OR attempt.processing_lease_expires_at IS NOT NULL)
			UNION ALL
			SELECT 1 FROM external_slot_claims claim
			WHERE claim.salon_id::text = $1
			  AND claim.released_at IS NULL
			  AND claim.state IN ('claimed_pre_dispatch','dispatch_started','dispatched_unknown','reconciliation_required')
		)
	`, input.SalonID).Scan(&liveExternal); err != nil {
		return nil, false, err
	}
	if liveExternal {
		return nil, false, ErrLiveExecution
	}
	if input.ValidateTargetReadiness != nil {
		if err := input.ValidateTargetReadiness(ctx, tx); err != nil {
			return nil, false, err
		}
	} else if err := validateTargetReadinessFenceTx(ctx, tx, run.TargetSchedulingAuthority, input.SalonID, input.ExpectedReadiness); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE salon_settings SET scheduling_authority = $1, updated_at = now() WHERE salon_id::text = $2`, run.TargetSchedulingAuthority, input.SalonID); err != nil {
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE scheduling_authority_switch_runs SET status = 'committed', committed_at = now(), updated_at = now() WHERE id::text = $1`, input.RunID); err != nil {
		return nil, false, err
	}
	payload, _ := json.Marshal(map[string]any{"source_scheduling_authority": run.SourceSchedulingAuthority, "target_scheduling_authority": run.TargetSchedulingAuthority, "source_authority_version": run.ExpectedSourceAuthorityVersion, "target_authority_version": run.ExpectedSourceAuthorityVersion + 1})
	if _, err := tx.ExecContext(ctx, `INSERT INTO scheduling_authority_switch_events (salon_id,switch_run_id,action_key,action_fingerprint,event_type,actor_user_id,payload) VALUES ($1,$2,$3,$4,'commit',$5,$6::jsonb)`, input.SalonID, input.RunID, input.ActionKey, input.ActionFingerprint, input.OwnerUserID, string(payload)); err != nil {
		return nil, false, err
	}
	committed, err := getSwitchRunTx(ctx, tx, input.SalonID, input.OwnerUserID, input.RunID)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return committed, false, nil
}

func validateTargetReadinessFenceTx(ctx context.Context, tx *sql.Tx, target string, salonID string, expected ReadinessSnapshot) error {
	switch target {
	case TargetOwnerManual:
		var bookingMode string
		if err := tx.QueryRowContext(ctx, `SELECT booking_mode FROM salon_settings WHERE salon_id::text=$1 FOR SHARE`, salonID).Scan(&bookingMode); err != nil {
			return err
		}
		if bookingMode != "pending_approval" && bookingMode != "disabled" {
			return ErrReadinessConflict
		}
		rows, err := tx.QueryContext(ctx, `SELECT id FROM services WHERE salon_id::text=$1 AND active AND ai_bookable AND archived_at IS NULL AND duration_minutes > 0 FOR SHARE`, salonID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != expected.EligibleServiceCount || count == 0 {
			return ErrReadinessConflict
		}
	case TargetManleAICalendar:
		var version int64
		var activated sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT version, activated_version FROM manleai_calendar_configs WHERE salon_id::text=$1 FOR SHARE`, salonID).Scan(&version, &activated)
		if err != nil || !activated.Valid || version != expected.ConfigVersion || activated.Int64 != version {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrReadinessConflict
			}
			if err != nil {
				return err
			}
			return ErrReadinessConflict
		}
	}
	return nil
}

func classifySwitchConstraint(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && (pqErr.Constraint == "scheduling_authority_switch_runs_rollback_state_guard" || pqErr.Constraint == "scheduling_authority_switch_runs_rollback_tenant_fk") {
		return ErrStateConflict
	}
	return err
}

func getSwitchRunTx(ctx context.Context, tx *sql.Tx, salonID string, ownerUserID string, runID string) (*SwitchRun, error) {
	return scanSwitchRun(tx.QueryRowContext(ctx, switchRunSelect+`
		WHERE run.salon_id::text = $1
		  AND (
		      public.has_active_tenant_membership(salon.id, $2::uuid)
		      OR public.has_platform_salon_capability(salon.id, $2::uuid, 'technical.write')
		  )
		  AND run.id::text = $3
	`, salonID, ownerUserID, runID))
}

const switchRunSelect = `
	SELECT run.id::text, run.salon_id::text,
	       run.source_scheduling_authority, run.target_scheduling_authority,
	       run.expected_source_authority_version, run.operation_key,
	       run.actor_user_id::text, run.payload_fingerprint, run.readiness_snapshot, run.blockers,
	       run.status, run.previewed_at, run.blocked_at, run.committed_at,
	       COALESCE(run.rollback_of_switch_run_id::text, ''),
	       run.created_at, run.updated_at
	FROM scheduling_authority_switch_runs run
	JOIN salons salon ON salon.id = run.salon_id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSwitchRun(row rowScanner) (*SwitchRun, error) {
	var run SwitchRun
	var readinessJSON []byte
	var blockersJSON []byte
	err := row.Scan(
		&run.ID, &run.SalonID,
		&run.SourceSchedulingAuthority, &run.TargetSchedulingAuthority,
		&run.ExpectedSourceAuthorityVersion, &run.OperationKey,
		&run.ActorUserID, &run.payloadFingerprint, &readinessJSON, &blockersJSON,
		&run.Status, &run.PreviewedAt, &run.BlockedAt, &run.CommittedAt, &run.RollbackOfSwitchRunID,
		&run.CreatedAt, &run.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(readinessJSON, &run.ReadinessSnapshot); err != nil {
		return nil, fmt.Errorf("decode scheduling authority switch readiness: %w", err)
	}
	if err := json.Unmarshal(blockersJSON, &run.Blockers); err != nil {
		return nil, fmt.Errorf("decode scheduling authority switch blockers: %w", err)
	}
	if run.ReadinessSnapshot.Checks == nil {
		run.ReadinessSnapshot.Checks = make([]ReadinessCheck, 0)
	}
	if run.Blockers == nil {
		run.Blockers = make([]ReadinessBlocker, 0)
	}
	return &run, nil
}
