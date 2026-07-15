package conversation

import (
	"context"
	"strings"

	"github.com/manleai/ai-receptionist/internal/validation"
)

func (s *Service) HandleUnintelligibleVoiceInput(ctx context.Context, salonID string, ownerUserID string, sessionID string, req VoiceInputHandoffRequest) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	req.EventKey = normalizeEventKey(req.EventKey)
	if salonID == "" || ownerUserID == "" || sessionID == "" || req.EventKey == "" {
		return nil, ErrValidation
	}
	return s.withSessionTurnSerialization(ctx, salonID, ownerUserID, sessionID, func(serializedCtx context.Context) (*Session, error) {
		return retrySessionStateConflict(serializedCtx, func() (*Session, error) {
			return s.handleUnintelligibleVoiceInputOnce(serializedCtx, salonID, ownerUserID, sessionID, req)
		})
	})
}

func (s *Service) handleUnintelligibleVoiceInputOnce(ctx context.Context, salonID string, ownerUserID string, sessionID string, req VoiceInputHandoffRequest) (*Session, error) {
	salonID = strings.TrimSpace(salonID)
	ownerUserID = strings.TrimSpace(ownerUserID)
	sessionID = strings.TrimSpace(sessionID)
	eventKey := normalizeEventKey(req.EventKey)
	if salonID == "" || ownerUserID == "" || sessionID == "" || eventKey == "" {
		return nil, ErrValidation
	}
	if processed, ok, err := s.store.GetSessionByTurnEventKey(ctx, salonID, ownerUserID, sessionID, eventKey); err != nil {
		return nil, err
	} else if ok {
		return processed, nil
	}
	session, err := s.store.GetSessionForOwner(ctx, salonID, ownerUserID, sessionID)
	if err != nil {
		return nil, err
	}
	if session.Status != StatusActive {
		return nil, ErrSessionClosed
	}
	cfg, err := s.store.GetRuntimeConfig(ctx, salonID, ownerUserID)
	if err != nil {
		return nil, err
	}
	before := cloneSessionForTurn(*session)
	next := cloneSessionForTurn(*session)
	callbackPhone := callableCallbackPhone(next)
	if next.CustomerPhone == "" && callbackPhone != "" {
		next.CustomerPhone = callbackPhone
	}
	turn := newTurnRecord(salonID, ownerUserID, before, next, "", eventKey, nil, nil, cfg)
	turn.AIMetadata = mergeMetadata(turn.AIMetadata, map[string]any{
		"event_key":       eventKey,
		"recovery_action": "voice_input_unintelligible",
	})
	if callbackPhone != "" {
		reply := "I'm sorry, the background noise is making it hard to get your details right. I'll ask the salon to call you back at this number. " + voiceInputSafetySuffix(next)
		return s.saveHandoffTurn(ctx, turn, next, HandoffReasonVoiceInputUnintelligible, reply, nil, nil, cfg)
	}

	turn.AIMessage = "I'm sorry, the background noise is making it hard to get your details right. Please call again from a quieter place. " + voiceInputSafetySuffix(next)
	turn.Update.Status = StatusCompleted
	turn.Update.Outcome = OutcomeFailed
	turn.Update.EndSession = true
	turn.Update.Summary = summaryFor(next, nil, nil, cfg)
	finalizeTurnMetadata(&turn, before, next, "", "", "voice_input_unintelligible_no_callback")
	return s.store.SaveTurn(ctx, turn)
}

func callableCallbackPhone(session Session) string {
	for _, candidate := range []string{session.InboundPhone, session.CustomerPhone} {
		phone := validation.NormalizePhone(candidate)
		digits := strings.TrimPrefix(phone, "+")
		if len(digits) < 10 || len(digits) > 15 {
			continue
		}
		valid := true
		for _, value := range digits {
			if value < '0' || value > '9' {
				valid = false
				break
			}
		}
		if valid {
			return phone
		}
	}
	return ""
}

func voiceInputSafetySuffix(session Session) string {
	switch bookingActionForSession(session) {
	case BookingActionReschedule:
		return "Your appointment has not been rescheduled."
	case BookingActionCancel:
		return "Your appointment has not been cancelled."
	default:
		return "This is not a confirmed appointment."
	}
}
