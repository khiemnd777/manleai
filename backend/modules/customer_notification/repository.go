package customernotification

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) GetPolicyForOwner(ctx context.Context, salonID, ownerUserID string) (*Policy, error) {
	var policy Policy
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.customer_sms_enabled,
		       COALESCE(to_char(settings.customer_sms_quiet_start,'HH24:MI'),''),
		       COALESCE(to_char(settings.customer_sms_quiet_end,'HH24:MI'),''),
		       salon.timezone, settings.customer_sms_policy_version
		FROM salon_settings settings
		JOIN salons salon ON salon.id=settings.salon_id
		WHERE settings.salon_id=$1 AND public.has_active_tenant_membership(salon.id, $2::uuid)
	`, salonID, ownerUserID).Scan(&policy.Enabled, &policy.QuietStart, &policy.QuietEnd, &policy.Timezone, &policy.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	policy.Ready = strings.TrimSpace(policy.Timezone) != "" && policy.QuietStart != "" && policy.QuietEnd != "" && policy.QuietStart != policy.QuietEnd
	return &policy, nil
}

func (r *Repository) UpdatePolicyForOwner(ctx context.Context, salonID, ownerUserID string, req UpdatePolicyRequest) (*Policy, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var timezone string
	if err := tx.QueryRowContext(ctx, `SELECT timezone FROM salons WHERE id=$1 AND public.has_active_tenant_membership(id, $2::uuid) FOR UPDATE`, salonID, ownerUserID).Scan(&timezone); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var policy Policy
	err = tx.QueryRowContext(ctx, `
		UPDATE salon_settings
		SET customer_sms_enabled=$3,
		    customer_sms_quiet_start=NULLIF($4,'')::time,
		    customer_sms_quiet_end=NULLIF($5,'')::time,
		    updated_at=now()
		WHERE salon_id=$1 AND customer_sms_policy_version=$2
		RETURNING customer_sms_enabled,
		          COALESCE(to_char(customer_sms_quiet_start,'HH24:MI'),''),
		          COALESCE(to_char(customer_sms_quiet_end,'HH24:MI'),''),
		          customer_sms_policy_version
	`, salonID, req.ExpectedVersion, req.Enabled, req.QuietStart, req.QuietEnd).
		Scan(&policy.Enabled, &policy.QuietStart, &policy.QuietEnd, &policy.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	policy.Timezone = timezone
	policy.Ready = timezone != "" && policy.QuietStart != "" && policy.QuietEnd != "" && policy.QuietStart != policy.QuietEnd
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (r *Repository) ConsentForDestination(ctx context.Context, salonID, destination string) (*Consent, error) {
	return scanConsent(r.db.QueryRowContext(ctx, `
		SELECT id::text,status,destination_masked,version,source,requested_at,consented_at,
		       declined_at,opted_out_at,updated_at
		FROM customer_sms_consents WHERE salon_id=$1 AND normalized_destination=$2
	`, salonID, destination))
}

func (r *Repository) RecordConsentRequested(ctx context.Context, salonID, callSessionID, destination, eventKey, evidenceReference string) (*Consent, bool, error) {
	return r.mutateConsent(ctx, consentMutation{
		SalonID: salonID, Destination: destination, Status: ConsentPending,
		Source: ConsentSourceConversation, EventType: ConsentEventRequested,
		EventKey: eventKey, EvidenceType: "call_session_prompt", EvidenceReference: evidenceReference,
		CallSessionID: callSessionID,
	})
}

func (r *Repository) RecordConversationConsent(ctx context.Context, req RecordConversationConsentRequest) (*Consent, bool, error) {
	status, eventType := ConsentDeclined, ConsentEventDeclined
	if req.Granted {
		status, eventType = ConsentConsented, ConsentEventConsented
	}
	return r.mutateConsent(ctx, consentMutation{
		SalonID: req.SalonID, Destination: req.Destination, Status: status,
		Source: ConsentSourceConversation, EventType: eventType,
		EventKey: req.EventKey, EvidenceType: "call_session_response", EvidenceReference: req.EvidenceReference,
		CallSessionID: req.CallSessionID,
	})
}

func (r *Repository) RecordOwnerAttestation(ctx context.Context, salonID, ownerUserID, destination, eventKey string, attested bool) (*Consent, bool, error) {
	if !attested {
		return nil, false, ErrValidation
	}
	return r.mutateConsent(ctx, consentMutation{
		SalonID: salonID, Destination: destination, Status: ConsentConsented,
		Source: ConsentSourceOwner, EventType: ConsentEventConsented, EventKey: eventKey,
		EvidenceType: "owner_attestation", EvidenceReference: eventKey, ActorUserID: ownerUserID,
		RequireOwner: true,
	})
}

type consentMutation struct {
	SalonID, Destination, Status, Source, EventType, EventKey   string
	EvidenceType, EvidenceReference, CallSessionID, ActorUserID string
	ProviderMessageID, ExternalFingerprint                      string
	RequireOwner                                                bool
}

func (r *Repository) mutateConsent(ctx context.Context, mutation consentMutation) (*Consent, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if mutation.RequireOwner {
		var owner bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM salons WHERE id=$1 AND public.has_active_tenant_membership(id, $2::uuid))`, mutation.SalonID, mutation.ActorUserID).Scan(&owner); err != nil {
			return nil, false, err
		}
		if !owner {
			return nil, false, ErrNotFound
		}
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1 || ':' || $2, 0))`, mutation.SalonID, mutation.Destination); err != nil {
		return nil, false, err
	}
	fingerprint := consentFingerprint(mutation)
	var existingFingerprint, consentID string
	err = tx.QueryRowContext(ctx, `
		SELECT event_fingerprint, customer_sms_consent_id::text
		FROM customer_sms_consent_events WHERE salon_id=$1 AND event_key=$2
	`, mutation.SalonID, mutation.EventKey).Scan(&existingFingerprint, &consentID)
	if err == nil {
		if existingFingerprint != fingerprint {
			return nil, false, ErrConflict
		}
		item, scanErr := scanConsent(tx.QueryRowContext(ctx, `
			SELECT id::text,status,destination_masked,version,source,requested_at,consented_at,
			       declined_at,opted_out_at,updated_at
			FROM customer_sms_consents WHERE salon_id=$1 AND id=$2
		`, mutation.SalonID, consentID))
		return item, true, scanErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	var currentVersion int
	var currentStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT id::text,version,status FROM customer_sms_consents
		WHERE salon_id=$1 AND normalized_destination=$2 FOR UPDATE
	`, mutation.SalonID, mutation.Destination).Scan(&consentID, &currentVersion, &currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		consentID = ""
		currentVersion = 0
	} else if err != nil {
		return nil, false, err
	}
	if currentStatus == ConsentOptedOut && mutation.Source != ConsentSourceTwilio {
		return nil, false, ErrConflict
	}
	if !legalConsentTransition(currentStatus, mutation.Status, mutation.Source, mutation.EventType) {
		return nil, false, ErrConflict
	}
	if mutation.CallSessionID != "" {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_sessions WHERE salon_id=$1 AND id=$2)`, mutation.SalonID, mutation.CallSessionID).Scan(&exists); err != nil {
			return nil, false, err
		}
		if !exists {
			return nil, false, ErrNotFound
		}
	}
	nextVersion := currentVersion + 1
	requestedAt, consentedAt, declinedAt, optedOutAt := consentTimestamps(mutation.Status)
	if consentID == "" {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO customer_sms_consents (
				salon_id,normalized_destination,destination_masked,status,version,source,
				evidence_type,evidence_reference,call_session_id,actor_user_id,
				requested_at,consented_at,declined_at,opted_out_at
			) VALUES ($1,$2,mask_customer_sms_destination($2),$3,$4,$5,$6,$7,
			          NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10,$11,$12,$13)
			RETURNING id::text
		`, mutation.SalonID, mutation.Destination, mutation.Status, nextVersion, mutation.Source,
			mutation.EvidenceType, mutation.EvidenceReference, mutation.CallSessionID, mutation.ActorUserID,
			requestedAt, consentedAt, declinedAt, optedOutAt).Scan(&consentID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE customer_sms_consents
			SET status=$3,version=$4,source=$5,evidence_type=$6,evidence_reference=$7,
			    call_session_id=COALESCE(NULLIF($8,'')::uuid,call_session_id),
			    actor_user_id=NULLIF($9,'')::uuid,
			    requested_at=CASE WHEN $3='pending' THEN now() ELSE requested_at END,
			    consented_at=CASE WHEN $3='consented' THEN now() ELSE NULL END,
			    declined_at=CASE WHEN $3='declined' THEN now() ELSE NULL END,
			    opted_out_at=CASE WHEN $3='opted_out' THEN now() ELSE NULL END,
			    updated_at=now()
			WHERE salon_id=$1 AND id=$2
		`, mutation.SalonID, consentID, mutation.Status, nextVersion, mutation.Source,
			mutation.EvidenceType, mutation.EvidenceReference, mutation.CallSessionID, mutation.ActorUserID)
	}
	if err != nil {
		return nil, false, err
	}
	var eventSequence int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(max(event_sequence),0)+1
		FROM customer_sms_consent_events WHERE customer_sms_consent_id=$1
	`, consentID).Scan(&eventSequence); err != nil {
		return nil, false, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_sms_consent_events (
			salon_id,customer_sms_consent_id,event_sequence,consent_version,event_key,event_fingerprint,
			event_type,source,evidence_type,evidence_reference,actor_user_id,provider_message_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')::uuid,NULLIF($12,''))
	`, mutation.SalonID, consentID, eventSequence, nextVersion, mutation.EventKey, fingerprint,
		mutation.EventType, mutation.Source, mutation.EvidenceType, mutation.EvidenceReference,
		mutation.ActorUserID, mutation.ProviderMessageID)
	if err != nil {
		return nil, false, err
	}
	item, err := scanConsent(tx.QueryRowContext(ctx, `
		SELECT id::text,status,destination_masked,version,source,requested_at,consented_at,
		       declined_at,opted_out_at,updated_at
		FROM customer_sms_consents WHERE salon_id=$1 AND id=$2
	`, mutation.SalonID, consentID))
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return item, false, nil
}

// legalConsentTransition is deliberately state-based. Presentation wording,
// caller ID, and message body never decide consent. A provider-authenticated
// STOP is the only local path into opted_out, and only a provider-authenticated
// START can lift that terminal local guard.
func legalConsentTransition(current, next, source, eventType string) bool {
	if next == ConsentOptedOut {
		return source == ConsentSourceTwilio && eventType == ConsentEventOptOut
	}
	if current == ConsentOptedOut {
		return next == ConsentConsented && source == ConsentSourceTwilio && eventType == ConsentEventOptIn
	}
	switch current {
	case "":
		return next == ConsentPending || next == ConsentConsented || next == ConsentDeclined
	case ConsentPending:
		return next == ConsentPending || next == ConsentConsented || next == ConsentDeclined
	case ConsentDeclined:
		return next == ConsentPending || next == ConsentConsented || next == ConsentDeclined
	case ConsentConsented:
		return next == ConsentConsented
	default:
		return false
	}
}

func consentTimestamps(status string) (requestedAt, consentedAt, declinedAt, optedOutAt any) {
	now := time.Now().UTC()
	switch status {
	case ConsentPending:
		requestedAt = now
	case ConsentConsented:
		consentedAt = now
	case ConsentDeclined:
		declinedAt = now
	case ConsentOptedOut:
		optedOutAt = now
	}
	return
}

type rowScanner interface{ Scan(...any) error }

func scanConsent(row rowScanner) (*Consent, error) {
	var item Consent
	err := row.Scan(&item.ID, &item.Status, &item.DestinationMasked, &item.Version, &item.Source,
		&item.RequestedAt, &item.ConsentedAt, &item.DeclinedAt, &item.OptedOutAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func consentFingerprint(m consentMutation) string {
	raw := strings.Join([]string{m.Destination, m.Status, m.Source, m.EventType, m.EvidenceType,
		m.EvidenceReference, m.CallSessionID, m.ActorUserID, m.ProviderMessageID, m.ExternalFingerprint}, "\x00")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) ApplyInboundOptOut(ctx context.Context, event InboundOptOut) error {
	if event.ConfiguredSender != "" && event.To != event.ConfiguredSender {
		return ErrValidation
	}
	eventKey := "twilio-optout:" + event.ProviderMessageID + ":" + strings.ToLower(event.OptOutType)
	existing, err := r.ConsentForDestination(ctx, event.SalonID, event.From)
	if event.OptOutType == "STOP" {
		_, _, err = r.mutateConsent(ctx, consentMutation{
			SalonID: event.SalonID, Destination: event.From, Status: ConsentOptedOut,
			Source: ConsentSourceTwilio, EventType: ConsentEventOptOut, EventKey: eventKey,
			EvidenceType: "twilio_opt_out_type", EvidenceReference: event.OptOutType,
			ProviderMessageID: event.ProviderMessageID, ExternalFingerprint: event.EventFingerprint,
		})
		return err
	}
	if errors.Is(err, ErrNotFound) {
		if event.OptOutType != "START" {
			return nil
		}
		ready, readyErr := r.customerSMSPolicyReady(ctx, event.SalonID)
		if readyErr != nil || !ready {
			return readyErr
		}
		_, _, err = r.mutateConsent(ctx, consentMutation{
			SalonID: event.SalonID, Destination: event.From, Status: ConsentConsented,
			Source: ConsentSourceTwilio, EventType: ConsentEventOptIn, EventKey: eventKey,
			EvidenceType: "twilio_opt_out_type", EvidenceReference: event.OptOutType,
			ProviderMessageID: event.ProviderMessageID, ExternalFingerprint: event.EventFingerprint,
		})
		return err
	}
	if err != nil {
		return err
	}
	status, eventType := existing.Status, ConsentEventHelp
	if event.OptOutType == "START" {
		eventType = ConsentEventOptIn
		ready, readyErr := r.customerSMSPolicyReady(ctx, event.SalonID)
		if readyErr != nil {
			return readyErr
		}
		if ready {
			status = ConsentConsented
		}
	}
	if eventType == ConsentEventHelp || status == existing.Status {
		return r.appendConsentEvent(ctx, event.SalonID, existing.ID, eventKey, event.EventFingerprint,
			eventType, event.OptOutType, event.ProviderMessageID)
	}
	_, _, err = r.mutateConsent(ctx, consentMutation{
		SalonID: event.SalonID, Destination: event.From, Status: status,
		Source: ConsentSourceTwilio, EventType: eventType, EventKey: eventKey,
		EvidenceType: "twilio_opt_out_type", EvidenceReference: event.OptOutType,
		ProviderMessageID: event.ProviderMessageID, ExternalFingerprint: event.EventFingerprint,
	})
	return err
}

func (r *Repository) customerSMSPolicyReady(ctx context.Context, salonID string) (bool, error) {
	var ready bool
	err := r.db.QueryRowContext(ctx, `
		SELECT settings.customer_sms_enabled
		       AND settings.customer_sms_quiet_start IS NOT NULL
		       AND settings.customer_sms_quiet_end IS NOT NULL
		       AND settings.customer_sms_quiet_start<>settings.customer_sms_quiet_end
		       AND btrim(salon.timezone)<>''
		FROM salon_settings settings JOIN salons salon ON salon.id=settings.salon_id
		WHERE settings.salon_id=$1
	`, salonID).Scan(&ready)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return ready, err
}

func (r *Repository) appendConsentEvent(ctx context.Context, salonID, consentID, eventKey, eventFingerprint, eventType, optOutType, providerMessageID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existingFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT event_fingerprint FROM customer_sms_consent_events WHERE salon_id=$1 AND event_key=$2`, salonID, eventKey).Scan(&existingFingerprint)
	if err == nil {
		if existingFingerprint != eventFingerprint {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var version, eventSequence int
	if err := tx.QueryRowContext(ctx, `
		SELECT version FROM customer_sms_consents
		WHERE salon_id=$1 AND id=$2 FOR UPDATE
	`, salonID, consentID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	// Recheck after the consent-row lock so concurrent first delivery of the
	// same provider event becomes deterministic replay instead of a unique-key
	// failure.
	err = tx.QueryRowContext(ctx, `SELECT event_fingerprint FROM customer_sms_consent_events WHERE salon_id=$1 AND event_key=$2`, salonID, eventKey).Scan(&existingFingerprint)
	if err == nil {
		if existingFingerprint != eventFingerprint {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(max(event_sequence),0)+1
		FROM customer_sms_consent_events WHERE customer_sms_consent_id=$1
	`, consentID).Scan(&eventSequence); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO customer_sms_consent_events (
			salon_id,customer_sms_consent_id,event_sequence,consent_version,event_key,event_fingerprint,
			event_type,source,evidence_type,evidence_reference,provider_message_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,'twilio_advanced_opt_out','twilio_opt_out_type',$8,$9)
	`, salonID, consentID, eventSequence, version, eventKey, eventFingerprint, eventType, optOutType, providerMessageID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
