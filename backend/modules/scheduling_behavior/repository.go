package scheduling_behavior

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"
	"github.com/manleai/ai-receptionist/modules/scheduling"
	"github.com/manleai/ai-receptionist/modules/scheduling/fence"
)

const (
	policyResourceType = "ai_receptionist"
	policyResourceID   = "policy"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (repository *Repository) Get(ctx context.Context, salonID string) (PersistedState, error) {
	var state PersistedState
	err := repository.db.QueryRowContext(ctx, `
		SELECT settings.scheduling_authority, settings.scheduling_authority_version,
		       settings.booking_mode, COALESCE(version.version, 0)
		FROM salon_settings settings
		JOIN salons salon ON salon.id=settings.salon_id
		LEFT JOIN technical_resource_versions version
		  ON version.salon_id=settings.salon_id
		 AND version.resource_type='ai_receptionist'
		 AND version.resource_id='policy'
		WHERE settings.salon_id=$1
	`, salonID).Scan(&state.SchedulingAuthority, &state.AuthorityVersion, &state.BookingMode, &state.PolicyVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return PersistedState{}, ErrNotFound
	}
	return state, err
}

func (repository *Repository) UpdateBookingMode(ctx context.Context, command UpdateBookingModeCommand) (BookingModeMutationResult, bool, error) {
	tx, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return BookingModeMutationResult{}, false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, fence.AdvisoryKey(command.SalonID)); err != nil {
		return BookingModeMutationResult{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_resource_versions (salon_id,resource_type,resource_id,version)
		SELECT id,'ai_receptionist','policy',0 FROM salons WHERE id=$1
		ON CONFLICT DO NOTHING
	`, command.SalonID); err != nil {
		return BookingModeMutationResult{}, false, err
	}

	var existingFingerprint, existingActionType, existingResourceType, existingResourceID string
	var existingVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint,action_type,resource_type,resource_id,result_version
		FROM technical_actions
		WHERE salon_id=$1 AND actor_user_id=$2 AND action_key=$3
	`, command.SalonID, command.ActorUserID, command.ActionKey).Scan(
		&existingFingerprint, &existingActionType, &existingResourceType, &existingResourceID, &existingVersion,
	)
	if err == nil {
		if existingFingerprint != command.RequestFingerprint || existingActionType != bookingModeActionType(command.BookingMode) || existingResourceType != policyResourceType || existingResourceID != policyResourceID {
			return BookingModeMutationResult{}, false, ErrActionConflict
		}
		if err := tx.Commit(); err != nil {
			return BookingModeMutationResult{}, false, err
		}
		return BookingModeMutationResult{BookingMode: command.BookingMode, Version: existingVersion}, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return BookingModeMutationResult{}, false, err
	}

	var currentVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM technical_resource_versions
		WHERE salon_id=$1 AND resource_type='ai_receptionist' AND resource_id='policy'
		FOR UPDATE
	`, command.SalonID).Scan(&currentVersion); errors.Is(err, sql.ErrNoRows) {
		return BookingModeMutationResult{}, false, ErrNotFound
	} else if err != nil {
		return BookingModeMutationResult{}, false, err
	}
	if currentVersion != command.ExpectedVersion {
		return BookingModeMutationResult{}, false, ErrVersionConflict
	}

	var authority string
	var currentMode scheduling.BookingMode
	if err := tx.QueryRowContext(ctx, `
		SELECT scheduling_authority,booking_mode
		FROM salon_settings
		WHERE salon_id=$1
		FOR UPDATE
	`, command.SalonID).Scan(&authority, &currentMode); errors.Is(err, sql.ErrNoRows) {
		return BookingModeMutationResult{}, false, ErrNotFound
	} else if err != nil {
		return BookingModeMutationResult{}, false, err
	}
	if _, err := scheduling.ConversationBehavior(scheduling.ConversationPolicyFence{BookingMode: command.BookingMode, SchedulingAuthority: authority}); err != nil {
		return BookingModeMutationResult{}, false, ErrIncompatibleMode
	}

	resultVersion := currentVersion + 1
	if currentMode != command.BookingMode {
		if _, err := tx.ExecContext(ctx, `UPDATE salon_settings SET booking_mode=$1,updated_at=now() WHERE salon_id=$2`, command.BookingMode, command.SalonID); err != nil {
			return BookingModeMutationResult{}, false, classifyConstraint(err)
		}
		var triggeredVersion int64
		if err := tx.QueryRowContext(ctx, `
			SELECT version FROM technical_resource_versions
			WHERE salon_id=$1 AND resource_type='ai_receptionist' AND resource_id='policy'
		`, command.SalonID).Scan(&triggeredVersion); err != nil {
			return BookingModeMutationResult{}, false, err
		}
		if triggeredVersion != resultVersion {
			return BookingModeMutationResult{}, false, fmt.Errorf("booking mode policy version advanced to %d, expected %d", triggeredVersion, resultVersion)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE technical_resource_versions SET updated_by_user_id=$1,updated_at=now()
			WHERE salon_id=$2 AND resource_type='ai_receptionist' AND resource_id='policy'
		`, command.ActorUserID, command.SalonID); err != nil {
			return BookingModeMutationResult{}, false, err
		}
	} else if _, err := tx.ExecContext(ctx, `
		UPDATE technical_resource_versions
		SET version=$1,updated_by_user_id=$2,updated_at=now()
		WHERE salon_id=$3 AND resource_type='ai_receptionist' AND resource_id='policy'
	`, resultVersion, command.ActorUserID, command.SalonID); err != nil {
		return BookingModeMutationResult{}, false, err
	}

	details := `{"changed_fields":["booking_mode"]}`
	actionType := bookingModeActionType(command.BookingMode)
	var actionID string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO technical_actions (
			salon_id,actor_user_id,action_key,action_type,request_fingerprint,
			resource_type,resource_id,previous_version,result_version,details
		) VALUES ($1,$2,$3,$4,$5,'ai_receptionist','policy',$6,$7,$8::jsonb)
		RETURNING id::text
	`, command.SalonID, command.ActorUserID, command.ActionKey, actionType, command.RequestFingerprint, currentVersion, resultVersion, details).Scan(&actionID); err != nil {
		return BookingModeMutationResult{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO technical_events (
			action_id,salon_id,actor_user_id,event_type,resource_type,resource_id,
			previous_version,result_version,details
		) VALUES ($1,$2,$3,$4,'ai_receptionist','policy',$5,$6,$7::jsonb)
	`, actionID, command.SalonID, command.ActorUserID, actionType, currentVersion, resultVersion, details); err != nil {
		return BookingModeMutationResult{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return BookingModeMutationResult{}, false, err
	}
	return BookingModeMutationResult{BookingMode: command.BookingMode, Version: resultVersion}, false, nil
}

func bookingModeActionType(mode scheduling.BookingMode) string {
	return "scheduling.booking_mode." + string(mode)
}

func classifyConstraint(err error) error {
	var postgresError *pq.Error
	if errors.As(err, &postgresError) && postgresError.Constraint == "salon_settings_owner_manual_booking_mode_guard" {
		return ErrIncompatibleMode
	}
	if strings.Contains(err.Error(), "salon_settings_owner_manual_booking_mode_guard") {
		return ErrIncompatibleMode
	}
	return err
}
