package customernotification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/manleai/ai-receptionist/internal/databasecontext"
	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type expiredCustomerNotificationLease struct {
	id, salonID, token string
	dispatched         bool
	attempts, requeues int
}

func (r *Repository) RecoverExpiredLeases(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	discoveryCtx := databasecontext.WithScope(ctx, databasecontext.ScopeWorker)
	rows, err := r.db.QueryContext(discoveryCtx, `
		SELECT delivery_id::text, salon_id::text, claim_token::text,
		       dispatch_started, attempt_number, requeue_count
		FROM public.app_worker_expired_customer_notification_leases($1)
	`, limit)
	if err != nil {
		return 0, err
	}
	items := make([]expiredCustomerNotificationLease, 0, limit)
	for rows.Next() {
		var item expiredCustomerNotificationLease
		if err := rows.Scan(&item.id, &item.salonID, &item.token, &item.dispatched, &item.attempts, &item.requeues); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	recovered := 0
	for _, item := range items {
		itemCtx := databasecontext.WithSystemSalon(ctx, databasecontext.ScopeWorker, item.salonID)
		changed, err := r.recoverExpiredLease(itemCtx, item)
		if err != nil {
			return recovered, err
		}
		if changed {
			recovered++
		}
	}
	return recovered, nil
}

func (r *Repository) recoverExpiredLease(ctx context.Context, item expiredCustomerNotificationLease) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `
		SELECT delivery_dispatch_started_at IS NOT NULL, delivery_attempts, requeue_count
		FROM customer_notification_deliveries
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering'
		  AND delivery_claim_token=$3::uuid AND delivery_lease_expires_at<=now()
		FOR UPDATE
	`, item.id, item.salonID, item.token).Scan(&item.dispatched, &item.attempts, &item.requeues)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	status, outcome, code, eventType := StatusFailed, "safe_retry", "DELIVERY_LEASE_EXPIRED", "safe_retry_scheduled"
	if item.dispatched {
		status, outcome, code, eventType = StatusDeadLetter, "outcome_unknown", "DELIVERY_OUTCOME_UNKNOWN", "dead_lettered"
	} else if item.attempts-item.requeues*MaxSafeDeliveryAttempts >= MaxSafeDeliveryAttempts {
		status, outcome, code, eventType = StatusDeadLetter, "dead_letter", "DELIVERY_LEASE_EXHAUSTED", "dead_lettered"
	}
	_, err = tx.ExecContext(ctx, `
			UPDATE customer_notification_deliveries
			SET delivery_status=$3,next_delivery_at=CASE WHEN $3='failed' THEN now() ELSE next_delivery_at END,
			    dead_lettered_at=CASE WHEN $3='dead_letter' THEN now() ELSE NULL END,
			    last_delivery_error_code=$4,delivery_claim_token=NULL,delivery_claimed_at=NULL,
			    delivery_lease_expires_at=NULL,delivery_dispatch_started_at=NULL,updated_at=now()
			WHERE id=$1 AND salon_id=$2 AND delivery_claim_token=$5::uuid
	`, item.id, item.salonID, status, code, item.token)
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
			UPDATE customer_notification_delivery_attempts
			SET outcome=$2,error_code=$3,finished_at=now()
			WHERE claim_token=$1::uuid AND salon_id=$4 AND outcome='leased'
	`, item.token, outcome, code, item.salonID); err != nil {
		return false, err
	}
	if err := insertDeliveryEvent(ctx, tx, item.salonID, item.id, "expired:"+item.token, eventType, status, "", code); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) ClaimBatch(ctx context.Context, limit int, lease time.Duration) ([]ClaimedDelivery, error) {
	if limit <= 0 || lease <= 0 {
		return nil, ErrValidation
	}
	discoveryCtx := databasecontext.WithScope(ctx, databasecontext.ScopeWorker)
	rows, err := r.db.QueryContext(discoveryCtx, `
		SELECT delivery_id::text, salon_id::text, consent_id::text, notification_type,
		       message_body, destination_e164, destination_masked, consent_version,
		       policy_version, claim_token::text, attempt_number, requeue_count
		FROM public.app_worker_claim_customer_notifications($1, $2, $3)
	`, MaxSafeDeliveryAttempts, limit, lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	items := make([]ClaimedDelivery, 0, limit)
	for rows.Next() {
		var item ClaimedDelivery
		if err := rows.Scan(&item.ID, &item.SalonID, &item.ConsentID, &item.NotificationType, &item.Body,
			&item.Destination, &item.DestinationMasked, &item.ConsentVersion, &item.PolicyVersion,
			&item.ClaimToken, &item.AttemptNumber, &item.RequeueCount); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) ResolveDispatchReadiness(ctx context.Context, item ClaimedDelivery) (DispatchReadiness, error) {
	var readiness DispatchReadiness
	var consentStatus, currentDestination string
	var currentConsentVersion int
	var currentPolicyVersion int64
	var sourceCurrent bool
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.customer_sms_enabled,consent.status,consent.normalized_destination,
		       consent.version,settings.customer_sms_policy_version,
		       salon.timezone,COALESCE(to_char(settings.customer_sms_quiet_start,'HH24:MI'),''),
		       COALESCE(to_char(settings.customer_sms_quiet_end,'HH24:MI'),''),now(),
		       CASE
		         WHEN delivery.notification_type='request_received' THEN EXISTS(
		           SELECT 1 FROM scheduling_requests request
		           WHERE request.salon_id=delivery.salon_id AND request.id=delivery.scheduling_request_id
		             AND request.status='pending' AND request.version=delivery.source_version)
		         ELSE EXISTS(
		           SELECT 1 FROM appointments appointment
		           WHERE appointment.salon_id=delivery.salon_id AND appointment.id=delivery.appointment_id
		             AND appointment.booking_attempt_id=delivery.booking_attempt_id
		             AND appointment.status=delivery.notification_type
		             AND COALESCE(appointment.authority_appointment_version,1)=delivery.source_version)
		       END
		FROM customer_notification_deliveries delivery
		JOIN customer_sms_consents consent
		  ON consent.salon_id=delivery.salon_id AND consent.id=delivery.customer_sms_consent_id
		JOIN salon_settings settings ON settings.salon_id=delivery.salon_id
		JOIN salons salon ON salon.id=delivery.salon_id
		WHERE delivery.salon_id=$1 AND delivery.id=$2 AND delivery.delivery_status='delivering'
		  AND delivery.delivery_claim_token=$3::uuid
	`, item.SalonID, item.ID, item.ClaimToken).Scan(&readiness.Eligible, &consentStatus, &currentDestination,
		&currentConsentVersion, &currentPolicyVersion,
		&readiness.Timezone, &readiness.QuietStart, &readiness.QuietEnd, &readiness.Now, &sourceCurrent)
	if errors.Is(err, sql.ErrNoRows) {
		return readiness, ErrClaimLost
	}
	if err != nil {
		return readiness, err
	}
	if !readiness.Eligible {
		readiness.ReasonCode = "CUSTOMER_SMS_POLICY_DISABLED"
	} else if consentStatus != ConsentConsented || currentDestination != item.Destination {
		readiness.Eligible = false
		readiness.ReasonCode = "CUSTOMER_SMS_CONSENT_INACTIVE"
	} else if currentConsentVersion != item.ConsentVersion {
		readiness.Eligible = false
		readiness.ReasonCode = "CUSTOMER_SMS_CONSENT_STALE"
	} else if currentPolicyVersion != item.PolicyVersion {
		readiness.Eligible = false
		readiness.ReasonCode = "CUSTOMER_SMS_POLICY_STALE"
	} else if !sourceCurrent {
		readiness.Eligible = false
		readiness.ReasonCode = "CUSTOMER_SMS_SOURCE_STALE"
	} else if readiness.Timezone == "" || readiness.QuietStart == "" || readiness.QuietEnd == "" {
		readiness.Eligible = false
		readiness.ReasonCode = "CUSTOMER_SMS_POLICY_NOT_READY"
	}
	return readiness, nil
}

func (r *Repository) RecordQuietHours(ctx context.Context, item ClaimedDelivery, next time.Time) error {
	return r.completeWithoutProvider(ctx, item, StatusQuietHours, "quiet_hours", "CUSTOMER_SMS_QUIET_HOURS", next, false)
}

func (r *Repository) RecordSuppressed(ctx context.Context, item ClaimedDelivery, code string) error {
	return r.completeWithoutProvider(ctx, item, StatusSuppressed, "suppressed", code, time.Time{}, false)
}

func (r *Repository) RecordSafeFailure(ctx context.Context, item ClaimedDelivery, code string, next time.Time) error {
	status, outcome := StatusFailed, "safe_retry"
	if attemptInCycle(item) >= MaxSafeDeliveryAttempts {
		status, outcome = StatusDeadLetter, "dead_letter"
	}
	return r.completeWithoutProvider(ctx, item, status, outcome, code, next, status == StatusDeadLetter)
}

func (r *Repository) RecordOutcomeUnknown(ctx context.Context, item ClaimedDelivery, code string) error {
	// Never persist provider-specific ambiguity as the delivery-level reason.
	// Requeue safety is based on durable attempt outcome evidence as well, but
	// this canonical code keeps every reader fail-closed.
	return r.completeWithoutProvider(ctx, item, StatusDeadLetter, "outcome_unknown", "DELIVERY_OUTCOME_UNKNOWN", time.Time{}, true)
}

func (r *Repository) RecordDefinitiveFailure(ctx context.Context, item ClaimedDelivery, code string) error {
	return r.completeWithoutProvider(ctx, item, StatusDeadLetter, "dead_letter", code, time.Time{}, true)
}

func (r *Repository) completeWithoutProvider(ctx context.Context, item ClaimedDelivery, status, outcome, code string, next time.Time, dead bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var nextArg any
	if !next.IsZero() {
		nextArg = next
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE customer_notification_deliveries
		SET delivery_status=$3,next_delivery_at=COALESCE($4::timestamptz,next_delivery_at),
		    dead_lettered_at=CASE WHEN $5 THEN now() ELSE NULL END,
		    suppressed_at=CASE WHEN $3='suppressed' THEN now() ELSE NULL END,
		    last_delivery_error_code=NULLIF($6,''),delivery_claim_token=NULL,delivery_claimed_at=NULL,
		    delivery_lease_expires_at=NULL,delivery_dispatch_started_at=NULL,updated_at=now()
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering' AND delivery_claim_token=$7::uuid
	`, item.ID, item.SalonID, status, nextArg, dead, code, item.ClaimToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrClaimLost
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE customer_notification_delivery_attempts
		SET outcome=$2,error_code=NULLIF($3,''),finished_at=now()
		WHERE claim_token=$1::uuid AND outcome='leased'
	`, item.ClaimToken, outcome, code); err != nil {
		return err
	}
	eventType := "safe_retry_scheduled"
	if status == StatusQuietHours {
		eventType = "quiet_hours_scheduled"
	}
	if status == StatusDeadLetter {
		eventType = "dead_lettered"
	}
	if status == StatusSuppressed {
		eventType = "suppressed"
	}
	if err := insertDeliveryEvent(ctx, tx, item.SalonID, item.ID, outcome+":"+item.ClaimToken, eventType, status, "", code); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) MarkDispatchStarted(ctx context.Context, item ClaimedDelivery) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var consentStatus, destination string
	var consentVersion int
	if err := tx.QueryRowContext(ctx, `
		SELECT status,normalized_destination,version
		FROM customer_sms_consents
		WHERE salon_id=$1 AND id=$2
		FOR UPDATE
	`, item.SalonID, item.ConsentID).Scan(&consentStatus, &destination, &consentVersion); errors.Is(err, sql.ErrNoRows) {
		return ErrDispatchBlocked
	} else if err != nil {
		return err
	}
	var policyEnabled bool
	var policyVersion int64
	var quietStart, quietEnd, timezone string
	if err := tx.QueryRowContext(ctx, `
		SELECT settings.customer_sms_enabled,settings.customer_sms_policy_version,
		       COALESCE(to_char(settings.customer_sms_quiet_start,'HH24:MI'),''),
		       COALESCE(to_char(settings.customer_sms_quiet_end,'HH24:MI'),''),salon.timezone
		FROM salon_settings settings JOIN salons salon ON salon.id=settings.salon_id
		WHERE settings.salon_id=$1
		FOR UPDATE OF settings
	`, item.SalonID).Scan(&policyEnabled, &policyVersion, &quietStart, &quietEnd, &timezone); errors.Is(err, sql.ErrNoRows) {
		return ErrDispatchBlocked
	} else if err != nil {
		return err
	}
	if !policyEnabled || policyVersion != item.PolicyVersion || consentStatus != ConsentConsented ||
		consentVersion != item.ConsentVersion || destination != item.Destination ||
		strings.TrimSpace(timezone) == "" || quietStart == "" || quietEnd == "" {
		return ErrDispatchBlocked
	}
	var notificationType string
	var sourceVersion int
	var schedulingRequestID, appointmentID, bookingAttemptID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT notification_type,source_version,scheduling_request_id::text,appointment_id::text,booking_attempt_id::text
		FROM customer_notification_deliveries
		WHERE salon_id=$1 AND id=$2 AND delivery_status='delivering' AND delivery_claim_token=$3::uuid
	`, item.SalonID, item.ID, item.ClaimToken).Scan(&notificationType, &sourceVersion, &schedulingRequestID, &appointmentID, &bookingAttemptID); errors.Is(err, sql.ErrNoRows) {
		return ErrClaimLost
	} else if err != nil {
		return err
	}
	sourceCurrent, err := lockAndValidateDeliverySource(ctx, tx, item.SalonID, notificationType, sourceVersion, schedulingRequestID, appointmentID, bookingAttemptID)
	if err != nil {
		return err
	}
	if !sourceCurrent {
		return ErrDispatchBlocked
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE customer_notification_deliveries SET delivery_dispatch_started_at=now(),updated_at=now()
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering'
		  AND delivery_claim_token=$3::uuid AND delivery_lease_expires_at>now()
	`, item.ID, item.SalonID, item.ClaimToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrClaimLost
	}
	if _, err := tx.ExecContext(ctx, `UPDATE customer_notification_delivery_attempts SET dispatch_started_at=now() WHERE claim_token=$1::uuid AND outcome='leased'`, item.ClaimToken); err != nil {
		return err
	}
	if err := insertDeliveryEvent(ctx, tx, item.SalonID, item.ID, "dispatch:"+item.ClaimToken, "dispatch_started", StatusDelivering, "", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) RecordProviderResult(ctx context.Context, item ClaimedDelivery, send notificationdelivery.SendResult) error {
	if strings.TrimSpace(send.ProviderMessageID) == "" || send.StatusRank <= 0 {
		return ErrValidation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	status := send.DeliveryStatus
	dead, delivered := status == StatusDeadLetter, status == StatusDelivered
	result, err := tx.ExecContext(ctx, `
		UPDATE customer_notification_deliveries
		SET delivery_status=$3,delivery_provider='twilio',provider_message_id=$4,provider_status=$5,
		    provider_status_rank=$6,last_provider_event_at=now(),
		    delivered_at=CASE WHEN $7 THEN now() ELSE delivered_at END,
		    dead_lettered_at=CASE WHEN $8 THEN now() ELSE NULL END,
		    last_delivery_error_code=CASE WHEN $8 THEN 'TWILIO_DELIVERY_FAILED' ELSE NULL END,
		    delivery_claim_token=NULL,delivery_claimed_at=NULL,delivery_lease_expires_at=NULL,
		    delivery_dispatch_started_at=NULL,updated_at=now()
		WHERE id=$1 AND salon_id=$2 AND delivery_status='delivering' AND delivery_claim_token=$9::uuid
	`, item.ID, item.SalonID, status, send.ProviderMessageID, send.ProviderStatus, send.StatusRank, delivered, dead, item.ClaimToken)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrClaimLost
	}
	outcome := "provider_accepted"
	if status == StatusSent {
		outcome = "sent"
	}
	if delivered {
		outcome = "delivered"
	}
	if dead {
		outcome = "provider_failed"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE customer_notification_delivery_attempts SET outcome=$2,provider_status=$3,provider_message_id=$4,finished_at=now() WHERE claim_token=$1::uuid AND outcome='leased'`, item.ClaimToken, outcome, send.ProviderStatus, send.ProviderMessageID); err != nil {
		return err
	}
	if err := insertDeliveryEvent(ctx, tx, item.SalonID, item.ID, "provider:"+item.ClaimToken, "provider_response", status, send.ProviderStatus, ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) SalonIDForProviderMessage(ctx context.Context, provider, providerMessageID string) (string, error) {
	var located sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT public.app_provider_customer_message_salon($1,$2)::text`, provider, providerMessageID).Scan(&located)
	if errors.Is(err, sql.ErrNoRows) {
		return "", notificationdelivery.ErrNotFound
	}
	if err == nil && (!located.Valid || located.String == "") {
		return "", notificationdelivery.ErrNotFound
	}
	return located.String, err
}

func (r *Repository) ApplyProviderCallback(ctx context.Context, callback notificationdelivery.ProviderCallback) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, salonID, currentStatus string
	var currentRank int
	err = tx.QueryRowContext(ctx, `
		SELECT id::text,salon_id::text,delivery_status,provider_status_rank FROM customer_notification_deliveries
		WHERE delivery_provider=$1 AND provider_message_id=$2 FOR UPDATE
	`, callback.Provider, callback.ProviderMessageID).Scan(&id, &salonID, &currentStatus, &currentRank)
	if errors.Is(err, sql.ErrNoRows) {
		return notificationdelivery.ErrNotFound
	}
	if err != nil {
		return err
	}
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT event_fingerprint FROM customer_notification_delivery_events WHERE salon_id=$1 AND event_key=$2`, salonID, callback.EventKey).Scan(&existingFingerprint)
	if err == nil {
		if existingFingerprint != callback.EventFingerprint {
			return notificationdelivery.ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	resultStatus := currentStatus
	if callback.StatusRank > currentRank {
		resultStatus = callback.DeliveryStatus
		_, err = tx.ExecContext(ctx, `
			UPDATE customer_notification_deliveries
			SET delivery_status=$2,provider_status=$3,provider_status_rank=$4,last_provider_event_at=$5,
			    delivered_at=CASE WHEN $2='delivered' THEN $5 ELSE delivered_at END,
			    dead_lettered_at=CASE WHEN $2='dead_letter' THEN $5 ELSE NULL END,
			    last_delivery_error_code=NULLIF($6,''),updated_at=now()
			WHERE id=$1
		`, id, resultStatus, callback.ProviderStatus, callback.StatusRank, callback.OccurredAt, callback.ErrorCode)
		if err != nil {
			return err
		}
		attemptOutcome := "provider_accepted"
		if resultStatus == StatusSent {
			attemptOutcome = "sent"
		}
		if resultStatus == StatusDelivered {
			attemptOutcome = "delivered"
		}
		if resultStatus == StatusDeadLetter {
			attemptOutcome = "provider_failed"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE customer_notification_delivery_attempts
			SET outcome=$2,provider_status=$3,error_code=NULLIF($4,''),finished_at=COALESCE(finished_at,now())
			WHERE customer_notification_delivery_id=$1 AND provider_message_id=$5
		`, id, attemptOutcome, callback.ProviderStatus, callback.ErrorCode, callback.ProviderMessageID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO customer_notification_delivery_events (
			salon_id,customer_notification_delivery_id,event_key,event_fingerprint,event_type,
			delivery_status,provider_status,error_code
		) VALUES ($1,$2,$3,$4,'status_callback',$5,$6,NULLIF($7,''))
	`, salonID, id, callback.EventKey, callback.EventFingerprint,
		resultStatus, callback.ProviderStatus, callback.ErrorCode); err != nil {
		return err
	}
	return tx.Commit()
}

func insertDeliveryEvent(ctx context.Context, tx *sql.Tx, salonID, deliveryID, key, eventType, status, providerStatus, code string) error {
	fingerprintRaw := strings.Join([]string{deliveryID, key, eventType, status, providerStatus, code}, "\x00")
	sum := sha256.Sum256([]byte(fingerprintRaw))
	_, err := tx.ExecContext(ctx, `
		INSERT INTO customer_notification_delivery_events (
			salon_id,customer_notification_delivery_id,event_key,event_fingerprint,event_type,
			delivery_status,provider_status,error_code
		) VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''))
	`, salonID, deliveryID, key, hex.EncodeToString(sum[:]), eventType, status, providerStatus, code)
	return err
}

func (r *Repository) DetailForAppointment(ctx context.Context, salonID, ownerUserID, appointmentID string) (*Detail, error) {
	var destination string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(normalize_customer_sms_destination(appointment.customer_phone),'')
		FROM appointments appointment
		JOIN salons salon ON salon.id=appointment.salon_id
		WHERE appointment.salon_id=$1 AND appointment.id=$2
		  AND (public.has_active_tenant_membership(salon.id, $3::uuid)
		       OR public.has_platform_salon_capability(salon.id, $3::uuid, 'operations.read'))
	`, salonID, appointmentID, ownerUserID).Scan(&destination)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.detailForSource(ctx, salonID, appointmentID, "appointment", destination)
}

func (r *Repository) DetailForRequest(ctx context.Context, salonID, ownerUserID, requestID string) (*Detail, error) {
	var destination string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(normalize_customer_sms_destination(request.customer_phone),'')
		FROM scheduling_requests request
		JOIN salons salon ON salon.id=request.salon_id
		WHERE request.salon_id=$1 AND request.id=$2
		  AND (public.has_active_tenant_membership(salon.id, $3::uuid)
		       OR public.has_platform_salon_capability(salon.id, $3::uuid, 'operations.read'))
	`, salonID, requestID, ownerUserID).Scan(&destination)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.detailForSource(ctx, salonID, requestID, "request", destination)
}

func (r *Repository) detailForSource(ctx context.Context, salonID, sourceID, sourceKind, destination string) (*Detail, error) {
	detail := &Detail{Deliveries: []Delivery{}}
	if destination != "" {
		consent, consentErr := r.ConsentForDestination(ctx, salonID, destination)
		if consentErr == nil {
			detail.Consent = consent
		} else if !errors.Is(consentErr, ErrNotFound) {
			return nil, consentErr
		}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT delivery.id::text,delivery.notification_type,delivery.delivery_status,COALESCE(delivery.destination_masked,''),delivery.delivery_attempts,delivery.requeue_count,
		       COALESCE(delivery.provider_status,''),COALESCE(delivery.last_delivery_error_code,''),delivery.next_delivery_at,
		       delivery.delivered_at,delivery.dead_lettered_at,delivery.redacted_at,COALESCE(delivery.redaction_version,0),delivery.created_at,
		       EXISTS(SELECT 1 FROM customer_notification_delivery_attempts attempt
		              WHERE attempt.customer_notification_delivery_id=delivery.id
		                AND attempt.outcome='outcome_unknown'),
		       consent.status='consented'
		         AND consent.version=delivery.consent_version
		         AND consent.normalized_destination=delivery.destination_e164,
		       settings.customer_sms_enabled
		         AND settings.customer_sms_policy_version=delivery.policy_version,
		       CASE
		         WHEN delivery.notification_type='request_received' THEN EXISTS(
		           SELECT 1 FROM scheduling_requests request
		           WHERE request.salon_id=delivery.salon_id
		             AND request.id=delivery.scheduling_request_id
		             AND request.status='pending' AND request.version=delivery.source_version)
		         ELSE EXISTS(
		           SELECT 1 FROM appointments appointment
		           WHERE appointment.salon_id=delivery.salon_id
		             AND appointment.id=delivery.appointment_id
		             AND appointment.booking_attempt_id=delivery.booking_attempt_id
		             AND appointment.status=delivery.notification_type
		             AND COALESCE(appointment.authority_appointment_version,1)=delivery.source_version)
		       END
		FROM customer_notification_deliveries delivery
		JOIN customer_sms_consents consent
		  ON consent.salon_id=delivery.salon_id
		 AND consent.id=delivery.customer_sms_consent_id
		JOIN salon_settings settings ON settings.salon_id=delivery.salon_id
		WHERE delivery.salon_id=$1
		  AND (($3='appointment' AND delivery.appointment_id=$2) OR ($3='request' AND delivery.scheduling_request_id=$2))
		ORDER BY delivery.created_at DESC,delivery.id DESC
	`, salonID, sourceID, sourceKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item Delivery
		var redactedAt sql.NullTime
		var requeueCount int
		var outcomeUnknown, consentCurrent, policyCurrent, sourceCurrent bool
		if err := rows.Scan(&item.ID, &item.NotificationType, &item.DeliveryStatus, &item.DestinationMasked,
			&item.DeliveryAttempts, &requeueCount, &item.ProviderStatus, &item.LastDeliveryErrorCode, &item.NextDeliveryAt,
			&item.DeliveredAt, &item.DeadLetteredAt, &redactedAt, &item.RedactionVersion, &item.CreatedAt,
			&outcomeUnknown, &consentCurrent, &policyCurrent, &sourceCurrent); err != nil {
			return nil, err
		}
		if redactedAt.Valid {
			item.Redacted = true
			value := redactedAt.Time
			item.RedactedAt = &value
		}
		item.CanRequeue = !item.Redacted && item.DeliveryStatus == StatusDeadLetter && !outcomeUnknown &&
			consentCurrent && policyCurrent && sourceCurrent && requeueCount < MaxOwnerRequeues
		if item.Redacted {
			item.RequeueBlockedReason = "delivery_content_redacted"
		} else if outcomeUnknown {
			item.RequeueBlockedReason = "delivery_outcome_unknown"
		} else if requeueCount >= MaxOwnerRequeues {
			item.RequeueBlockedReason = "requeue_limit_reached"
		} else if !consentCurrent {
			item.RequeueBlockedReason = "customer_sms_consent_inactive_or_stale"
		} else if !policyCurrent {
			item.RequeueBlockedReason = "customer_sms_policy_disabled_or_stale"
		} else if !sourceCurrent {
			item.RequeueBlockedReason = "source_snapshot_stale"
		} else if !item.CanRequeue {
			item.RequeueBlockedReason = "delivery_not_safely_requeueable"
		}
		detail.Deliveries = append(detail.Deliveries, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range detail.Deliveries {
		eventRows, err := r.db.QueryContext(ctx, `
			SELECT event_type,delivery_status,COALESCE(provider_status,''),COALESCE(error_code,''),created_at
			FROM customer_notification_delivery_events
			WHERE salon_id=$1 AND customer_notification_delivery_id=$2
			ORDER BY created_at,id
		`, salonID, detail.Deliveries[index].ID)
		if err != nil {
			return nil, err
		}
		for eventRows.Next() {
			var event DeliveryEvent
			if err := eventRows.Scan(&event.EventType, &event.DeliveryStatus, &event.ProviderStatus, &event.ErrorCode, &event.CreatedAt); err != nil {
				eventRows.Close()
				return nil, err
			}
			detail.Deliveries[index].Events = append(detail.Deliveries[index].Events, event)
		}
		if err := eventRows.Close(); err != nil {
			return nil, err
		}
	}
	return detail, nil
}

func (r *Repository) RequeueForOwner(ctx context.Context, salonID, ownerUserID, appointmentID, deliveryID, actionKey, fingerprint string) (bool, error) {
	return r.requeueForOwner(ctx, salonID, ownerUserID, appointmentID, "appointment", deliveryID, actionKey, fingerprint)
}

func (r *Repository) RequeueRequestForOwner(ctx context.Context, salonID, ownerUserID, requestID, deliveryID, actionKey, fingerprint string) (bool, error) {
	return r.requeueForOwner(ctx, salonID, ownerUserID, requestID, "request", deliveryID, actionKey, fingerprint)
}

func (r *Repository) requeueForOwner(ctx context.Context, salonID, ownerUserID, sourceID, sourceKind, deliveryID, actionKey, fingerprint string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT action_fingerprint FROM customer_notification_delivery_actions
		WHERE salon_id=$1 AND action_key=$2
	`, salonID, actionKey).Scan(&existingFingerprint)
	if err == nil {
		if existingFingerprint != fingerprint {
			return false, ErrConflict
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var status, code, consentStatus, currentDestination, notificationType string
	var deliveryDestination string
	var consentVersion, deliveryConsentVersion, requeueCount, sourceVersion int
	var policyVersion, deliveryPolicyVersion int64
	var redactedAt sql.NullTime
	var schedulingRequestID, sourceAppointmentID, bookingAttemptID sql.NullString
	var policyEnabled, owns, outcomeUnknown bool
	err = tx.QueryRowContext(ctx, `
		SELECT delivery.delivery_status,COALESCE(delivery.last_delivery_error_code,''),
		       consent.status,consent.normalized_destination,
		       delivery.destination_e164,consent.version,delivery.consent_version,
		       settings.customer_sms_policy_version,delivery.policy_version,
		       settings.customer_sms_enabled,
		       (public.has_active_tenant_membership(salon.id, $4::uuid)
		        OR public.has_platform_salon_capability(salon.id, $4::uuid, 'operations.write')),
		       delivery.redacted_at,delivery.requeue_count,
		       EXISTS(SELECT 1 FROM customer_notification_delivery_attempts attempt
		              WHERE attempt.customer_notification_delivery_id=delivery.id
		                AND attempt.outcome='outcome_unknown'),
		       delivery.notification_type,delivery.source_version,
		       delivery.scheduling_request_id::text,delivery.appointment_id::text,delivery.booking_attempt_id::text
		FROM customer_notification_deliveries delivery
		JOIN customer_sms_consents consent
		  ON consent.salon_id=delivery.salon_id AND consent.id=delivery.customer_sms_consent_id
		JOIN salon_settings settings ON settings.salon_id=delivery.salon_id
		JOIN salons salon ON salon.id=delivery.salon_id
		WHERE delivery.salon_id=$1 AND delivery.id=$2
		  AND (($5='appointment' AND delivery.appointment_id=$3) OR ($5='request' AND delivery.scheduling_request_id=$3))
		FOR UPDATE OF delivery,consent,settings
	`, salonID, deliveryID, sourceID, ownerUserID, sourceKind).Scan(
		&status, &code, &consentStatus, &currentDestination, &deliveryDestination,
		&consentVersion, &deliveryConsentVersion, &policyVersion, &deliveryPolicyVersion,
		&policyEnabled, &owns, &redactedAt, &requeueCount, &outcomeUnknown,
		&notificationType, &sourceVersion, &schedulingRequestID, &sourceAppointmentID, &bookingAttemptID,
	)
	if errors.Is(err, sql.ErrNoRows) || !owns {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	sourceCurrent, err := lockAndValidateDeliverySource(ctx, tx, salonID, notificationType, sourceVersion, schedulingRequestID, sourceAppointmentID, bookingAttemptID)
	if err != nil {
		return false, err
	}
	if redactedAt.Valid || !policyEnabled || consentStatus != ConsentConsented || currentDestination == "" ||
		currentDestination != deliveryDestination || consentVersion != deliveryConsentVersion ||
		policyVersion != deliveryPolicyVersion || status != StatusDeadLetter || outcomeUnknown ||
		requeueCount >= MaxOwnerRequeues || !sourceCurrent {
		return false, ErrRequeueBlocked
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE customer_notification_deliveries
		SET delivery_status='queued',next_delivery_at=now(),dead_lettered_at=NULL,suppressed_at=NULL,
		    requeue_count=requeue_count+1,last_delivery_error_code=NULL,updated_at=now()
		WHERE salon_id=$1 AND id=$2
	`, salonID, deliveryID)
	if err != nil {
		return false, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return false, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO customer_notification_delivery_actions (
			salon_id,customer_notification_delivery_id,action_key,action_fingerprint,
			action_type,actor_user_id,result_delivery_status
		) VALUES ($1,$2,$3,$4,'requeue',$5,'queued')
	`, salonID, deliveryID, actionKey, fingerprint, ownerUserID); err != nil {
		return false, err
	}
	if err := insertDeliveryEvent(ctx, tx, salonID, deliveryID, "owner-requeue:"+actionKey, "owner_requeued", StatusQueued, "", ""); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func lockAndValidateDeliverySource(
	ctx context.Context,
	tx *sql.Tx,
	salonID, notificationType string,
	sourceVersion int,
	schedulingRequestID, appointmentID, bookingAttemptID sql.NullString,
) (bool, error) {
	if notificationType == NotificationRequestReceived {
		if !schedulingRequestID.Valid {
			return false, nil
		}
		var version int
		var status string
		err := tx.QueryRowContext(ctx, `
			SELECT version,status FROM scheduling_requests
			WHERE salon_id=$1 AND id=$2 FOR UPDATE
		`, salonID, schedulingRequestID.String).Scan(&version, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return version == sourceVersion && status == "pending", err
	}
	if !appointmentID.Valid || !bookingAttemptID.Valid {
		return false, nil
	}
	var version int
	var status, currentAttemptID string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(authority_appointment_version,1),status,booking_attempt_id::text
		FROM appointments WHERE salon_id=$1 AND id=$2 FOR UPDATE
	`, salonID, appointmentID.String).Scan(&version, &status, &currentAttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return version == sourceVersion && status == notificationType && currentAttemptID == bookingAttemptID.String, nil
}
