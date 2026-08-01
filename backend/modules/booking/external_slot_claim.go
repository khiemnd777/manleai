package booking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

type externalSlotClaimRequest struct {
	Safety    ExternalSchedulingSafety
	Intervals []ExternalSlotClaimIntervalRecord
}

type externalSlotClaimIntervalFragment struct {
	StartTime         time.Time
	EndTime           time.Time
	ActivationPending bool
}

type externalOccupiedRange struct {
	StartTime time.Time
	EndTime   time.Time
}

func validateExternalSlotClaimRequest(attempt BookingAttempt, request *externalSlotClaimRequest) error {
	if request == nil || strings.TrimSpace(request.Safety.EvidenceID) == "" || request.Safety.ConfigVersion <= 0 || len(request.Intervals) == 0 {
		return ErrSchedulingAuthorityNotReady
	}
	if !request.Safety.ConcreteStaffAssignment {
		return ErrSchedulingAuthorityNotReady
	}
	switch attempt.OperationType {
	case BookingActionBook:
		if !request.Safety.AtomicCreateNoOverlap {
			return ErrSchedulingAuthorityNotReady
		}
	case BookingActionReschedule:
		if !request.Safety.AtomicRescheduleNoOverlap {
			return ErrSchedulingAuthorityNotReady
		}
	default:
		return ErrValidation
	}
	for _, interval := range request.Intervals {
		if strings.TrimSpace(interval.ResourceKind) == "" || strings.TrimSpace(interval.ResourceID) == "" ||
			interval.OccupiedStartTime.IsZero() || !interval.OccupiedEndTime.After(interval.OccupiedStartTime) ||
			len(interval.SourceSegmentIndexes) == 0 {
			return ErrValidation
		}
	}
	return nil
}

func reacquireExternalSlotClaimTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, processingToken string, leaseExpiresAt time.Time) error {
	required, err := externalSlotClaimRequiredTx(ctx, tx, salonID, attemptID)
	if err != nil || !required {
		return err
	}
	var claimID string
	err = tx.QueryRowContext(ctx, `
		UPDATE external_slot_claims claim
		SET processing_token=$1,lease_expires_at=$2,version=claim.version+1,updated_at=now()
		FROM external_provider_scheduling_capability_evidence evidence
		JOIN salon_integration_configs config
		  ON config.salon_id=evidence.salon_id
		 AND config.id=evidence.integration_config_id
		 AND config.provider=evidence.provider
		 AND config.enabled=true
		JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id
		 AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		 AND version.version=evidence.config_version
		JOIN booking_attempts attempt ON attempt.salon_id=evidence.salon_id
		JOIN pos_connections connection
		  ON connection.salon_id=attempt.salon_id
		 AND connection.provider=evidence.provider
		 AND (
		      evidence.verification_contract_version='external-slot-commit-v1'
		      OR (
		          connection.id=evidence.connection_id
		          AND connection.booking_write_capability_version=evidence.connection_capability_version
		      )
		 )
		 AND connection.status='active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id=evidence.provider_location_id
		 AND connection.snapshot_generation=attempt.provider_snapshot_generation
		JOIN salons salon
		  ON salon.id=attempt.salon_id
		 AND salon.active_pos_provider=evidence.provider
		WHERE claim.salon_id=$3 AND claim.booking_attempt_id=$4
		  AND claim.state='claimed_pre_dispatch' AND claim.released_at IS NULL
		  AND claim.provider_capability_evidence_id=evidence.id
		  AND claim.provider_config_version=evidence.config_version
		  AND attempt.id=claim.booking_attempt_id
		  AND attempt.provider_location_id=claim.provider_location_id
		  AND evidence.verified_at <= now() AND evidence.expires_at > now()
		  AND (
		      evidence.verification_contract_version='external-slot-commit-v1'
		      OR (
		          evidence.verification_contract_version='square-buyer-single-create-v1'
		          AND evidence.write_permission_mode='buyer_write'
		          AND evidence.provider_api_version=config.settings->>'api_version'
		          AND evidence.oauth_scope_fingerprint=public.square_oauth_scope_fingerprint(connection.scopes)
		      )
		  )
		  AND evidence.concrete_staff_assignment=true
		  AND ((claim.operation_type='book' AND evidence.atomic_create_no_overlap=true)
		       OR (claim.operation_type='reschedule' AND evidence.atomic_reschedule_no_overlap=true))
		RETURNING claim.id::text
	`, processingToken, leaseExpiresAt, salonID, attemptID).Scan(&claimID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSchedulingAuthorityNotReady
	}
	return err
}

// insertExternalSlotClaimTx runs after the booking attempt and exact quote have
// been persisted in the same transaction. The savepoint allows a PostgreSQL
// exclusion violation to become a durable terminal conflict attempt without
// losing the winner's active claim or dispatching a provider request.
func insertExternalSlotClaimTx(ctx context.Context, tx *sql.Tx, attempt *BookingAttempt, request *externalSlotClaimRequest) (bool, error) {
	if attempt == nil {
		return false, ErrValidation
	}
	if err := validateExternalSlotClaimRequest(*attempt, request); err != nil {
		return false, err
	}
	if err := lockExternalSlotClaimResourcesTx(ctx, tx, *attempt, request.Intervals); err != nil {
		return false, err
	}
	replacesClaimID := ""
	if attempt.OperationType == BookingActionReschedule {
		err := tx.QueryRowContext(ctx, `
			SELECT claim.id::text
			FROM appointments appointment
			JOIN external_slot_claims claim
			  ON claim.salon_id=appointment.salon_id
			 AND (
			      claim.target_appointment_id=appointment.id
			      OR (claim.operation_type='book' AND claim.booking_attempt_id=appointment.booking_attempt_id)
			 )
			 AND claim.state='confirmed'
			 AND claim.released_at IS NULL
			WHERE appointment.salon_id=$1
			  AND appointment.id=$2
			  AND appointment.authority_appointment_version=$3
			ORDER BY CASE WHEN claim.target_appointment_id=appointment.id THEN 0 ELSE 1 END,
			         claim.created_at DESC, claim.id
			LIMIT 1
			FOR UPDATE OF claim
		`, attempt.SalonID, attempt.TargetAppointmentID, attempt.TargetAuthorityAppointmentVersion).Scan(&replacesClaimID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
	}

	var claimID string
	err := tx.QueryRowContext(ctx, `
		INSERT INTO external_slot_claims (
			salon_id, booking_attempt_id, provider, provider_location_id,
			operation_type, target_appointment_id, expected_target_authority_version, replaces_claim_id,
			state, provider_capability_evidence_id, provider_config_version,
			processing_token, lease_expires_at
		)
		SELECT $1, $2, $3, $4, $5, NULLIF($6::text,'')::uuid, NULLIF($7,0), NULLIF($12::text,'')::uuid,
		       'claimed_pre_dispatch', evidence.id, evidence.config_version, $8, $9
		FROM external_provider_scheduling_capability_evidence evidence
		JOIN salon_integration_configs config
		  ON config.salon_id=evidence.salon_id
		 AND config.id=evidence.integration_config_id
		 AND config.provider=evidence.provider
		 AND config.enabled=true
		JOIN technical_resource_versions version
		  ON version.salon_id=config.salon_id
		 AND version.resource_type='integration_config'
		 AND version.resource_id=config.provider
		 AND version.version=evidence.config_version
		JOIN pos_connections connection
		  ON connection.salon_id=evidence.salon_id
		 AND connection.provider=evidence.provider
		 AND connection.id=evidence.connection_id
		 AND connection.booking_write_capability_version=evidence.connection_capability_version
		 AND connection.status='active'
		 AND connection.last_sync_at IS NOT NULL
		 AND connection.location_id=evidence.provider_location_id
		 AND connection.snapshot_generation=$13
		WHERE evidence.id::text=$10
		  AND evidence.salon_id=$1
		  AND evidence.provider=$3
		  AND evidence.provider_location_id=$4
		  AND evidence.config_version=$11
		  AND evidence.verification_contract_version='square-buyer-single-create-v1'
		  AND evidence.write_permission_mode='buyer_write'
		  AND evidence.provider_api_version=config.settings->>'api_version'
		  AND evidence.oauth_scope_fingerprint=public.square_oauth_scope_fingerprint(connection.scopes)
		  AND evidence.verified_at <= now()
		  AND evidence.expires_at > now()
		  AND evidence.concrete_staff_assignment=true
		  AND (($5='book' AND evidence.atomic_create_no_overlap=true)
		       OR ($5='reschedule' AND evidence.atomic_reschedule_no_overlap=true))
		RETURNING id::text
	`, attempt.SalonID, attempt.ID, attempt.POSProvider, attempt.ProviderFence.LocationID,
		attempt.OperationType, attempt.TargetAppointmentID, attempt.TargetAuthorityAppointmentVersion,
		attempt.ProcessingToken, attempt.ProcessingLeaseEnds, request.Safety.EvidenceID,
		request.Safety.ConfigVersion, replacesClaimID, attempt.ProviderFence.SnapshotGeneration).Scan(&claimID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrSchedulingAuthorityNotReady
	}
	if err != nil {
		return false, err
	}
	// Keep the root claim outside the savepoint so a losing contender can retain
	// one durable terminal claim_conflict event while only its intervals roll
	// back. The surrounding transaction still owns the attempt and quote.
	if _, err := tx.ExecContext(ctx, `SAVEPOINT external_slot_claim_intervals`); err != nil {
		return false, err
	}

	for _, interval := range request.Intervals {
		fragments := []externalSlotClaimIntervalFragment{{
			StartTime: interval.OccupiedStartTime.UTC(), EndTime: interval.OccupiedEndTime.UTC(),
		}}
		if replacesClaimID != "" {
			var conflictsOutsideTarget bool
			if err := tx.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM external_slot_claim_intervals existing
					WHERE existing.salon_id=$1
					  AND existing.provider=$2
					  AND existing.provider_location_id=$3
					  AND existing.resource_kind=$4
					  AND existing.resource_id=$5
					  AND existing.released_at IS NULL
					  AND existing.activation_pending=false
					  AND existing.claim_id<>$6
					  AND tstzrange(existing.occupied_start_time,existing.occupied_end_time,'[)')
					      && tstzrange($7,$8,'[)')
				)
			`, attempt.SalonID, attempt.POSProvider, attempt.ProviderFence.LocationID,
				interval.ResourceKind, interval.ResourceID, replacesClaimID,
				interval.OccupiedStartTime.UTC(), interval.OccupiedEndTime.UTC()).Scan(&conflictsOutsideTarget); err != nil {
				_ = rollbackExternalSlotClaimSavepoint(ctx, tx)
				return false, err
			}
			if conflictsOutsideTarget {
				return persistExternalSlotConflictTx(ctx, tx, attempt, claimID)
			}
			fragments, err = replacementExternalSlotClaimFragmentsTx(ctx, tx, attempt, replacesClaimID, interval)
			if err != nil {
				_ = rollbackExternalSlotClaimSavepoint(ctx, tx)
				return false, err
			}
		}
		for _, fragment := range fragments {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO external_slot_claim_intervals (
					salon_id, claim_id, provider, provider_location_id,
					resource_kind, resource_id, source_segment_indexes,
					occupied_start_time, occupied_end_time, resource_capacity_version, activation_pending
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,0),$11)
			`, attempt.SalonID, claimID, attempt.POSProvider, attempt.ProviderFence.LocationID,
				interval.ResourceKind, interval.ResourceID, pq.Array(interval.SourceSegmentIndexes),
				fragment.StartTime, fragment.EndTime, interval.ResourceCapacityVersion, fragment.ActivationPending)
			if err != nil {
				var postgresError *pq.Error
				if errors.As(err, &postgresError) && string(postgresError.Code) == "23P01" && postgresError.Constraint == "external_slot_claim_intervals_no_overlap" {
					return persistExternalSlotConflictTx(ctx, tx, attempt, claimID)
				}
				_ = rollbackExternalSlotClaimSavepoint(ctx, tx)
				return false, err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO external_slot_claim_events (
			salon_id, claim_id, booking_attempt_id, action_key,
			event_type, from_state, to_state, payload
		) VALUES ($1,$2,$3,'claim_acquired','claim_acquired',NULL,'claimed_pre_dispatch','{}'::jsonb)
	`, attempt.SalonID, claimID, attempt.ID); err != nil {
		_ = rollbackExternalSlotClaimSavepoint(ctx, tx)
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT external_slot_claim_intervals`); err != nil {
		return false, err
	}
	return false, nil
}

func lockExternalSlotClaimResourcesTx(ctx context.Context, tx *sql.Tx, attempt BookingAttempt, intervals []ExternalSlotClaimIntervalRecord) error {
	keys := make([]string, 0, len(intervals))
	seen := make(map[string]struct{}, len(intervals))
	for _, interval := range intervals {
		key := strings.Join([]string{
			"external-slot-claim", attempt.SalonID, attempt.POSProvider,
			attempt.ProviderFence.LocationID, interval.ResourceKind, interval.ResourceID,
		}, ":")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key); err != nil {
			return err
		}
	}
	return nil
}

func replacementExternalSlotClaimFragmentsTx(ctx context.Context, tx *sql.Tx, attempt *BookingAttempt, replacesClaimID string, interval ExternalSlotClaimIntervalRecord) ([]externalSlotClaimIntervalFragment, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT GREATEST(existing.occupied_start_time,$7),
		       LEAST(existing.occupied_end_time,$8)
		FROM external_slot_claim_intervals existing
		WHERE existing.salon_id=$1 AND existing.claim_id=$2
		  AND existing.provider=$3 AND existing.provider_location_id=$4
		  AND existing.resource_kind=$5 AND existing.resource_id=$6
		  AND existing.released_at IS NULL
		  AND tstzrange(existing.occupied_start_time,existing.occupied_end_time,'[)')
		      && tstzrange($7,$8,'[)')
		ORDER BY existing.occupied_start_time,existing.id
	`, attempt.SalonID, replacesClaimID, attempt.POSProvider, attempt.ProviderFence.LocationID,
		interval.ResourceKind, interval.ResourceID, interval.OccupiedStartTime.UTC(), interval.OccupiedEndTime.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ranges := make([]externalOccupiedRange, 0)
	for rows.Next() {
		var item externalOccupiedRange
		if err := rows.Scan(&item.StartTime, &item.EndTime); err != nil {
			return nil, err
		}
		ranges = append(ranges, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return externalSlotClaimFragments(interval.OccupiedStartTime.UTC(), interval.OccupiedEndTime.UTC(), ranges), nil
}

func externalSlotClaimFragments(startTime, endTime time.Time, ranges []externalOccupiedRange) []externalSlotClaimIntervalFragment {
	cursor := startTime
	fragments := make([]externalSlotClaimIntervalFragment, 0, len(ranges)*2+1)
	appendFragment := func(startTime, fragmentEnd time.Time, pending bool) {
		if !startTime.Before(fragmentEnd) {
			return
		}
		if len(fragments) > 0 {
			last := &fragments[len(fragments)-1]
			if last.ActivationPending == pending && last.EndTime.Equal(startTime) {
				last.EndTime = fragmentEnd
				return
			}
		}
		fragments = append(fragments, externalSlotClaimIntervalFragment{StartTime: startTime, EndTime: fragmentEnd, ActivationPending: pending})
	}
	for _, item := range ranges {
		if item.EndTime.Before(cursor) || item.EndTime.Equal(cursor) {
			continue
		}
		if cursor.Before(item.StartTime) {
			appendFragment(cursor, item.StartTime, false)
			cursor = item.StartTime
		}
		coveredStart := cursor
		if item.StartTime.After(coveredStart) {
			coveredStart = item.StartTime
		}
		appendFragment(coveredStart, item.EndTime, true)
		if item.EndTime.After(cursor) {
			cursor = item.EndTime
		}
	}
	appendFragment(cursor, endTime, false)
	return fragments
}

func persistExternalSlotConflictTx(ctx context.Context, tx *sql.Tx, attempt *BookingAttempt, claimID string) (bool, error) {
	if rollbackErr := rollbackExternalSlotClaimSavepoint(ctx, tx); rollbackErr != nil {
		return false, rollbackErr
	}
	if _, updateErr := tx.ExecContext(ctx, `
		UPDATE booking_attempts
		SET status='failed', provider_outcome='not_started', retry_policy='none',
		    reconciliation_status='not_required', error_code='SLOT_COMMIT_CONFLICT',
		    error_message='The selected scheduling resources changed before provider dispatch.',
		    processing_token=NULL, processing_lease_expires_at=NULL, updated_at=now()
		WHERE id=$1 AND salon_id=$2 AND provider_outcome='not_started'
	`, attempt.ID, attempt.SalonID); updateErr != nil {
		return false, updateErr
	}
	if _, updateErr := tx.ExecContext(ctx, `
		UPDATE external_slot_claims
		SET state='definite_failure', released_at=now(), release_reason='slot_commit_conflict',
		    processing_token=NULL, lease_expires_at=NULL, version=version+1, updated_at=now()
		WHERE salon_id=$1 AND id=$2 AND state='claimed_pre_dispatch' AND released_at IS NULL
	`, attempt.SalonID, claimID); updateErr != nil {
		return false, updateErr
	}
	if _, eventErr := tx.ExecContext(ctx, `
		INSERT INTO external_slot_claim_events (
			salon_id,claim_id,booking_attempt_id,action_key,event_type,from_state,to_state,payload
		) VALUES ($1,$2,$3,'claim_conflict','claim_conflict','claimed_pre_dispatch','definite_failure',
		          jsonb_build_object('error_code','SLOT_COMMIT_CONFLICT'))
	`, attempt.SalonID, claimID, attempt.ID); eventErr != nil {
		return false, eventErr
	}
	attempt.Status = "failed"
	attempt.ErrorCode = "SLOT_COMMIT_CONFLICT"
	attempt.ErrorMessage = "The selected scheduling resources changed before provider dispatch."
	attempt.ProcessingToken = ""
	attempt.ProcessingLeaseEnds = nil
	return true, nil
}

func rollbackExternalSlotClaimSavepoint(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT external_slot_claim_intervals`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT external_slot_claim_intervals`); err != nil {
		return fmt.Errorf("release external slot claim savepoint: %w", err)
	}
	return nil
}

func confirmExternalSlotClaimTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, providerBookingID string, providerBookingVersion int, eventType string) error {
	required, err := externalSlotClaimRequiredTx(ctx, tx, salonID, attemptID)
	if err != nil || !required {
		return err
	}
	var claimID string
	var fromState string
	err = tx.QueryRowContext(ctx, `
		WITH prior AS (
			SELECT id,state
			FROM external_slot_claims
			WHERE salon_id=$1 AND booking_attempt_id=$2 AND released_at IS NULL
			  AND state IN ('dispatch_started','dispatched_unknown','reconciliation_required')
			FOR UPDATE
		), updated AS (
		UPDATE external_slot_claims claim
		SET state='confirmed', provider_booking_id=$3, provider_booking_version=$4,
		    processing_token=NULL, lease_expires_at=NULL,
		    version=claim.version+1, updated_at=now()
		FROM prior
		WHERE claim.id=prior.id
		RETURNING claim.id::text
		)
		SELECT updated.id,prior.state FROM updated JOIN prior ON prior.id=updated.id::uuid
	`, salonID, attemptID, providerBookingID, providerBookingVersion).Scan(&claimID, &fromState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOperationConflict
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(eventType) == "" {
		eventType = "provider_confirmed"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO external_slot_claim_events (
			salon_id,claim_id,booking_attempt_id,action_key,event_type,from_state,to_state,payload
		) VALUES ($1,$2,$3,$4,$4,$5,'confirmed',jsonb_build_object('provider_booking_version',$6::integer))
		ON CONFLICT (claim_id,action_key) DO NOTHING
	`, salonID, claimID, attemptID, eventType, fromState, providerBookingVersion)
	return err
}

func recordExternalSlotClaimFallbackTx(ctx context.Context, tx *sql.Tx, attempt BookingAttempt) error {
	required, err := externalSlotClaimRequiredTx(ctx, tx, attempt.SalonID, attempt.ID)
	if err != nil || !required {
		return err
	}
	if attempt.ProviderOutcome == ProviderOutcomeFailed && attempt.Reconciliation != ReconciliationRequired {
		return releaseExternalSlotClaimTx(ctx, tx, attempt.SalonID, attempt.ID, ExternalSlotClaimDefiniteFailure, "provider_definite_failure", "provider_definite_failure")
	}
	nextState := ExternalSlotClaimDispatchedUnknown
	eventType := "provider_outcome_unknown"
	if attempt.Reconciliation == ReconciliationRequired {
		nextState = ExternalSlotClaimReconciliationRequired
		eventType = "reconciliation_required"
	}
	var claimID string
	var fromState string
	err = tx.QueryRowContext(ctx, `
		WITH prior AS (
			SELECT id,state
			FROM external_slot_claims
			WHERE salon_id=$1 AND booking_attempt_id=$2 AND released_at IS NULL
			  AND state IN ('dispatch_started','dispatched_unknown','reconciliation_required')
			FOR UPDATE
		), updated AS (
		UPDATE external_slot_claims claim
		SET state=$3, provider_booking_id=NULLIF($4,''),
		    provider_booking_version=CASE WHEN NULLIF($4,'') IS NULL THEN provider_booking_version ELSE $5 END,
		    processing_token=NULL, lease_expires_at=NULL,
		    version=claim.version+1, updated_at=now()
		FROM prior
		WHERE claim.id=prior.id
		RETURNING claim.id
		)
		SELECT updated.id::text,prior.state FROM updated JOIN prior ON prior.id=updated.id
	`, attempt.SalonID, attempt.ID, nextState, attempt.POSBookingID, attempt.POSBookingVersion).Scan(&claimID, &fromState)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOperationConflict
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO external_slot_claim_events (
			salon_id,claim_id,booking_attempt_id,action_key,event_type,from_state,to_state,payload
		) VALUES ($1,$2,$3,$4,$4,$5,$6,jsonb_build_object('provider_outcome',$7::text,'error_code',$8::text))
		ON CONFLICT (claim_id,action_key) DO NOTHING
	`, attempt.SalonID, claimID, attempt.ID, eventType, fromState, nextState, attempt.ProviderOutcome, attempt.ErrorCode)
	return err
}

func releaseExternalSlotClaimTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string, terminalState string, releaseReason string, eventType string) error {
	required, err := externalSlotClaimRequiredTx(ctx, tx, salonID, attemptID)
	if err != nil || !required {
		return err
	}
	var claimID string
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text
		FROM external_slot_claims
		WHERE salon_id=$1 AND booking_attempt_id=$2 AND released_at IS NULL
		FOR UPDATE
	`, salonID, attemptID).Scan(&claimID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOperationConflict
		}
		return err
	}
	return releaseExternalSlotClaimByIDTx(ctx, tx, salonID, claimID, terminalState, releaseReason, eventType)
}

func releaseExternalSlotClaimByIDTx(ctx context.Context, tx *sql.Tx, salonID string, claimID string, terminalState string, releaseReason string, eventType string) error {
	var attemptID string
	var fromState string
	if err := tx.QueryRowContext(ctx, `
		SELECT booking_attempt_id::text,state
		FROM external_slot_claims
		WHERE salon_id=$1 AND id=$2 AND released_at IS NULL
		FOR UPDATE
	`, salonID, claimID).Scan(&attemptID, &fromState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOperationConflict
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE external_slot_claim_intervals
		SET released_at=now()
		WHERE salon_id=$1 AND claim_id=$2 AND released_at IS NULL
	`, salonID, claimID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE external_slot_claims
		SET state=$3,released_at=now(),release_reason=$4,
		    processing_token=NULL,lease_expires_at=NULL,
		    version=version+1,updated_at=now()
		WHERE salon_id=$1 AND id=$2 AND released_at IS NULL
	`, salonID, claimID, terminalState, releaseReason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO external_slot_claim_events (
			salon_id,claim_id,booking_attempt_id,action_key,event_type,from_state,to_state,payload
		) VALUES ($1,$2,$3,$4,$4,$5,$6,jsonb_build_object('release_reason',$7::text))
		ON CONFLICT (claim_id,action_key) DO NOTHING
	`, salonID, claimID, attemptID, eventType, fromState, terminalState, releaseReason)
	return err
}

func releaseExternalAppointmentSlotClaimTx(ctx context.Context, tx *sql.Tx, salonID string, appointmentID string, originAttemptID string, terminalState string, releaseReason string, eventType string) error {
	var claimID string
	err := tx.QueryRowContext(ctx, `
		SELECT claim.id::text
		FROM external_slot_claims claim
		WHERE claim.salon_id=$1
		  AND claim.state='confirmed'
		  AND claim.released_at IS NULL
		  AND (
		      claim.target_appointment_id=$2
		      OR (claim.operation_type='book' AND claim.booking_attempt_id=$3)
		  )
		ORDER BY CASE WHEN claim.target_appointment_id=$2 THEN 0 ELSE 1 END,
		         claim.created_at DESC,claim.id
		LIMIT 1
		FOR UPDATE
	`, salonID, appointmentID, originAttemptID).Scan(&claimID)
	if errors.Is(err, sql.ErrNoRows) {
		required, requiredErr := externalSlotClaimRequiredTx(ctx, tx, salonID, originAttemptID)
		if requiredErr != nil || !required {
			return requiredErr
		}
		return ErrOperationConflict
	}
	if err != nil {
		return err
	}
	return releaseExternalSlotClaimByIDTx(ctx, tx, salonID, claimID, terminalState, releaseReason, eventType)
}

func externalSlotClaimRequiredTx(ctx context.Context, tx *sql.Tx, salonID string, attemptID string) (bool, error) {
	var required bool
	err := tx.QueryRowContext(ctx, `
		SELECT external_slot_claim_required
		FROM booking_attempts
		WHERE salon_id=$1 AND id=$2
	`, salonID, attemptID).Scan(&required)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrOperationConflict
	}
	return required, err
}

func confirmExternalRescheduleSlotClaimTx(ctx context.Context, tx *sql.Tx, salonID string, newAttemptID string, providerBookingID string, providerBookingVersion int) error {
	required, err := externalSlotClaimRequiredTx(ctx, tx, salonID, newAttemptID)
	if err != nil || !required {
		return err
	}
	var newClaimID string
	var replacesClaimID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT id::text,replaces_claim_id::text
		FROM external_slot_claims
		WHERE salon_id=$1 AND booking_attempt_id=$2
		  AND state IN ('dispatch_started','dispatched_unknown','reconciliation_required')
		  AND released_at IS NULL
		FOR UPDATE
	`, salonID, newAttemptID).Scan(&newClaimID, &replacesClaimID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrOperationConflict
		}
		return err
	}
	if replacesClaimID.Valid {
		if err := releaseExternalSlotClaimByIDTx(ctx, tx, salonID, replacesClaimID.String, ExternalSlotClaimReleased, "reschedule_replaced", "reschedule_replaced"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE external_slot_claim_intervals
			SET activation_pending=false
			WHERE salon_id=$1 AND claim_id=$2 AND released_at IS NULL AND activation_pending=true
		`, salonID, newClaimID); err != nil {
			return err
		}
	}
	return confirmExternalSlotClaimTx(ctx, tx, salonID, newAttemptID, providerBookingID, providerBookingVersion, "provider_confirmed")
}
