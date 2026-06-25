package voice

import (
	"context"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

var errUnsafeReply = errors.New("voice reply failed guardrails")

type GuardedReplyGenerator struct {
	provider LanguageModelProvider
}

func NewGuardedReplyGenerator(provider LanguageModelProvider) *GuardedReplyGenerator {
	return &GuardedReplyGenerator{provider: provider}
}

func (g *GuardedReplyGenerator) GenerateReply(ctx context.Context, req conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	if g == nil || g.provider == nil || !g.provider.Configured(ctx, req.SalonID) || req.Channel != conversation.ChannelPhone {
		return conversation.ReplyGenerationResult{}, ErrProviderDisabled
	}
	reply, err := g.provider.GenerateReply(ctx, ModelRequest{
		SalonID:             req.SalonID,
		SessionID:           req.SessionID,
		Channel:             req.Channel,
		Intent:              req.Intent,
		Outcome:             req.Outcome,
		CustomerMessage:     req.CustomerMessage,
		SafeReply:           req.SafeReply,
		SalonName:           req.SalonName,
		BookingConfirmed:    req.BookingConfirmed,
		FallbackOrHandoff:   req.FallbackOrHandoff,
		MissingBookingField: req.MissingBookingField,
		Summary:             req.Summary,
		KnowledgeContext:    req.KnowledgeContext,
	})
	if err != nil {
		return conversation.ReplyGenerationResult{}, err
	}
	if !replyAllowed(req, reply) {
		return conversation.ReplyGenerationResult{}, errUnsafeReply
	}
	return conversation.ReplyGenerationResult{
		Message:    strings.TrimSpace(reply.Message),
		Confidence: reply.Confidence,
		Handoff:    reply.Handoff,
		Reason:     reply.Reason,
	}, nil
}

func replyAllowed(req conversation.ReplyGenerationRequest, reply ModelReply) bool {
	message := strings.TrimSpace(reply.Message)
	if message == "" {
		return false
	}
	if reply.Confidence > 0 && reply.Confidence < 0.55 {
		return false
	}
	if strings.Count(message, "?") > 1 {
		return false
	}
	if len(strings.Fields(message)) > 45 {
		return false
	}
	if !req.BookingConfirmed && hasUnsafeConfirmation(message) {
		return false
	}
	return true
}

func hasUnsafeConfirmation(message string) bool {
	lower := strings.ToLower(message)
	unsafeAlways := []string{
		"you are booked",
		"you're booked",
		"appointment is set",
		"all set for",
		"see you at",
	}
	for _, phrase := range unsafeAlways {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if !strings.Contains(lower, "confirmed") {
		return false
	}
	safeNegations := []string{
		"not confirmed",
		"not a confirmed",
		"cannot confirm",
		"could not confirm",
		"not yet confirmed",
	}
	for _, phrase := range safeNegations {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	return true
}
