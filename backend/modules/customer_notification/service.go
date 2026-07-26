package customernotification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	notificationdelivery "github.com/manleai/ai-receptionist/modules/notification_delivery"
)

type Service struct{ repo *Repository }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) GetPolicy(ctx context.Context, salonID, ownerUserID string) (*Policy, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrValidation
	}
	return s.repo.GetPolicyForOwner(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID))
}

func (s *Service) UpdatePolicy(ctx context.Context, salonID, ownerUserID string, req UpdatePolicyRequest) (*Policy, error) {
	if strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || req.ExpectedVersion < 1 {
		return nil, ErrValidation
	}
	start, startErr := parseLocalClock(req.QuietStart)
	end, endErr := parseLocalClock(req.QuietEnd)
	if startErr != nil || endErr != nil || start == end {
		return nil, ErrValidation
	}
	req.QuietStart, req.QuietEnd = formatLocalClock(start), formatLocalClock(end)
	return s.repo.UpdatePolicyForOwner(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), req)
}

func (s *Service) ConsentStatus(ctx context.Context, salonID, destination string) (*Consent, error) {
	normalized := NormalizeDestination(destination)
	if strings.TrimSpace(salonID) == "" || normalized == "" {
		return nil, ErrValidation
	}
	return s.repo.ConsentForDestination(ctx, strings.TrimSpace(salonID), normalized)
}

func (s *Service) RecordConsentRequested(ctx context.Context, salonID, callSessionID, destination, eventKey string) (*Consent, bool, error) {
	normalized := NormalizeDestination(destination)
	if normalized == "" || !validIdentity(salonID, callSessionID, eventKey) {
		return nil, false, ErrValidation
	}
	return s.repo.RecordConsentRequested(ctx, strings.TrimSpace(salonID), strings.TrimSpace(callSessionID), normalized,
		strings.TrimSpace(eventKey), strings.TrimSpace(callSessionID))
}

func (s *Service) RecordConversationConsent(ctx context.Context, req RecordConversationConsentRequest) (*Consent, bool, error) {
	req.SalonID, req.CallSessionID, req.EventKey = strings.TrimSpace(req.SalonID), strings.TrimSpace(req.CallSessionID), strings.TrimSpace(req.EventKey)
	req.Destination = NormalizeDestination(req.Destination)
	if !validIdentity(req.SalonID, req.CallSessionID, req.EventKey) || req.Destination == "" {
		return nil, false, ErrValidation
	}
	if strings.TrimSpace(req.EvidenceReference) == "" {
		req.EvidenceReference = req.CallSessionID
	}
	return s.repo.RecordConversationConsent(ctx, req)
}

func (s *Service) AttestConsent(ctx context.Context, salonID, ownerUserID string, req AttestConsentRequest) (*Consent, bool, error) {
	destination := NormalizeDestination(req.Destination)
	actionKey := strings.TrimSpace(req.ActionKey)
	if !req.Attested || destination == "" || strings.TrimSpace(salonID) == "" || strings.TrimSpace(ownerUserID) == "" || actionKey == "" || len(actionKey) > 256 {
		return nil, false, ErrValidation
	}
	return s.repo.RecordOwnerAttestation(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), destination,
		"owner-attest:"+actionKey, req.Attested)
}

func (s *Service) ApplyInboundOptOut(ctx context.Context, salonID, from, to, configuredSender, optOutType, providerMessageID, eventFingerprint string) error {
	event := InboundOptOut{
		SalonID: strings.TrimSpace(salonID), From: NormalizeDestination(from), To: NormalizeDestination(to),
		ConfiguredSender: NormalizeDestination(configuredSender), OptOutType: strings.ToUpper(strings.TrimSpace(optOutType)),
		ProviderMessageID: strings.TrimSpace(providerMessageID), EventFingerprint: strings.TrimSpace(eventFingerprint),
	}
	if event.SalonID == "" || event.From == "" || event.To == "" || event.ProviderMessageID == "" ||
		len(event.EventFingerprint) != 64 ||
		(event.OptOutType != "START" && event.OptOutType != "STOP" && event.OptOutType != "HELP") {
		return notificationdelivery.ErrValidation
	}
	if err := s.repo.ApplyInboundOptOut(ctx, event); err != nil {
		if err == ErrConflict {
			return notificationdelivery.ErrConflict
		}
		if err == ErrValidation {
			return notificationdelivery.ErrValidation
		}
		return err
	}
	return nil
}

func (s *Service) SalonIDForProviderMessage(ctx context.Context, provider, providerMessageID string) (string, error) {
	return s.repo.SalonIDForProviderMessage(ctx, provider, providerMessageID)
}

func (s *Service) ApplyProviderCallback(ctx context.Context, callback notificationdelivery.ProviderCallback) error {
	if strings.TrimSpace(callback.ProviderMessageID) == "" || strings.TrimSpace(callback.EventKey) == "" || len(callback.EventFingerprint) != 64 || callback.StatusRank <= 0 {
		return notificationdelivery.ErrValidation
	}
	return s.repo.ApplyProviderCallback(ctx, callback)
}

func (s *Service) DetailForAppointment(ctx context.Context, salonID, ownerUserID, appointmentID string) (*Detail, error) {
	if !validIdentity(salonID, ownerUserID, appointmentID) {
		return nil, ErrValidation
	}
	return s.repo.DetailForAppointment(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(appointmentID))
}

func (s *Service) DetailForRequest(ctx context.Context, salonID, ownerUserID, requestID string) (*Detail, error) {
	if !validIdentity(salonID, ownerUserID, requestID) {
		return nil, ErrValidation
	}
	return s.repo.DetailForRequest(ctx, strings.TrimSpace(salonID), strings.TrimSpace(ownerUserID), strings.TrimSpace(requestID))
}

func (s *Service) Requeue(ctx context.Context, salonID, ownerUserID, appointmentID, deliveryID string, req RequeueRequest) (*Detail, bool, error) {
	if !validIdentity(salonID, ownerUserID, appointmentID) || strings.TrimSpace(deliveryID) == "" || strings.TrimSpace(req.ActionKey) == "" || len(strings.TrimSpace(req.ActionKey)) > 256 {
		return nil, false, ErrValidation
	}
	fingerprint := sha256.Sum256([]byte(strings.Join([]string{appointmentID, deliveryID, strings.TrimSpace(req.ActionKey)}, "\x00")))
	replayed, err := s.repo.RequeueForOwner(ctx, salonID, ownerUserID, appointmentID, deliveryID,
		strings.TrimSpace(req.ActionKey), hex.EncodeToString(fingerprint[:]))
	if err != nil {
		return nil, false, err
	}
	detail, err := s.repo.DetailForAppointment(ctx, salonID, ownerUserID, appointmentID)
	return detail, replayed, err
}

func (s *Service) RequeueRequest(ctx context.Context, salonID, ownerUserID, requestID, deliveryID string, req RequeueRequest) (*Detail, bool, error) {
	if !validIdentity(salonID, ownerUserID, requestID) || strings.TrimSpace(deliveryID) == "" || strings.TrimSpace(req.ActionKey) == "" || len(strings.TrimSpace(req.ActionKey)) > 256 {
		return nil, false, ErrValidation
	}
	fingerprint := sha256.Sum256([]byte(strings.Join([]string{requestID, deliveryID, strings.TrimSpace(req.ActionKey)}, "\x00")))
	replayed, err := s.repo.RequeueRequestForOwner(ctx, salonID, ownerUserID, requestID, deliveryID,
		strings.TrimSpace(req.ActionKey), hex.EncodeToString(fingerprint[:]))
	if err != nil {
		return nil, false, err
	}
	detail, err := s.repo.DetailForRequest(ctx, salonID, ownerUserID, requestID)
	return detail, replayed, err
}

func validIdentity(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func NormalizeDestination(value string) string {
	trimmed := strings.TrimSpace(value)
	var digits strings.Builder
	plusCount := 0
	for index, char := range trimmed {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
			continue
		}
		switch char {
		case '+':
			plusCount++
			if index != 0 || plusCount > 1 {
				return ""
			}
		case ' ', '(', ')', '.', '-':
		default:
			return ""
		}
	}
	value = digits.String()
	switch {
	case strings.HasPrefix(trimmed, "+") && len(value) >= 8 && len(value) <= 15 && value[0] != '0':
		return "+" + value
	case len(value) == 10 && value[0] >= '2' && value[0] <= '9':
		return "+1" + value
	case len(value) == 11 && value[0] == '1' && value[1] >= '2' && value[1] <= '9':
		return "+" + value
	default:
		return ""
	}
}
