package voice

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

var errUnsafeReply = errors.New("voice reply failed guardrails")

var replyClockReferencePattern = regexp.MustCompile(`(?i)\b(?:\d{1,2}(?::\d{2})?\s*(?:a\.?\s*m\.?|p\.?\s*m\.?)|(?:one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve)\s*(?:a\.?\s*m\.?|p\.?\s*m\.?))(?:$|[^a-z0-9])`)

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
	if req.ReplyPolicy != conversation.ReplyPolicyStyleOnly {
		return conversation.ReplyGenerationResult{}, errUnsafeReply
	}
	reply, err := g.provider.GenerateReply(ctx, ModelRequest{
		SalonID:              req.SalonID,
		SessionID:            req.SessionID,
		Channel:              req.Channel,
		Intent:               req.Intent,
		Outcome:              req.Outcome,
		CustomerMessage:      req.CustomerMessage,
		SafeReply:            req.SafeReply,
		SalonName:            req.SalonName,
		AITone:               req.AITone,
		BookingConfirmed:     req.BookingConfirmed,
		FallbackOrHandoff:    req.FallbackOrHandoff,
		MissingBookingField:  req.MissingBookingField,
		KnownBookingFields:   append([]string(nil), req.KnownBookingFields...),
		NextRequiredField:    req.NextRequiredField,
		SelectedServiceNames: append([]string(nil), req.SelectedServiceNames...),
		Summary:              req.Summary,
		KnowledgeContext:     req.KnowledgeContext,
		ReplyPolicy:          req.ReplyPolicy,
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
	if hasCasualOpener(message) {
		return false
	}
	if asksForKnownBookingField(req, message) {
		return false
	}
	if !preservesSelectedServiceNames(req, message) {
		return false
	}
	return true
}

func preservesSelectedServiceNames(req conversation.ReplyGenerationRequest, message string) bool {
	if len(req.SelectedServiceNames) == 0 {
		return true
	}
	safe := normalizeServiceGuardText(req.SafeReply)
	reply := normalizeServiceGuardText(message)
	if safe == "" || reply == "" {
		return true
	}
	for _, name := range req.SelectedServiceNames {
		normalizedName := normalizeServiceGuardText(name)
		if normalizedName == "" || !strings.Contains(safe, normalizedName) {
			continue
		}
		if !strings.Contains(reply, normalizedName) {
			return false
		}
	}
	return true
}

func normalizeServiceGuardText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		".", " ",
		",", " ",
		"!", " ",
		"?", " ",
		":", " ",
		";", " ",
		"-", " ",
		"_", " ",
		"&", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func hasCasualOpener(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	rejectedPrefixes := []string{
		"hey",
		"hi",
		"hi there",
		"hello there",
		"awesome choice",
		"great choice",
	}
	for _, prefix := range rejectedPrefixes {
		if lower == prefix ||
			strings.HasPrefix(lower, prefix+" ") ||
			strings.HasPrefix(lower, prefix+",") ||
			strings.HasPrefix(lower, prefix+"!") ||
			strings.HasPrefix(lower, prefix+".") {
			return true
		}
	}
	return false
}

func asksForKnownBookingField(req conversation.ReplyGenerationRequest, message string) bool {
	for _, field := range req.KnownBookingFields {
		if asksForBookingField(message, field) {
			return true
		}
	}
	switch req.NextRequiredField {
	case "requested_time":
		return asksForBookingField(message, "requested_date")
	default:
		return false
	}
}

func asksForBookingField(message string, field string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch field {
	case "service":
		return strings.Contains(lower, "what service") ||
			strings.Contains(lower, "which service") ||
			strings.Contains(lower, "what nail service")
	case "requested_date":
		return strings.Contains(lower, "what day") ||
			strings.Contains(lower, "which day") ||
			strings.Contains(lower, "what date") ||
			strings.Contains(lower, "which date") ||
			strings.Contains(lower, "when would you like") ||
			strings.Contains(lower, "when do you want")
	case "requested_start_time", "requested_time":
		return strings.Contains(lower, "what time") ||
			strings.Contains(lower, "which time") ||
			asksToConfirmClock(lower)
	case "customer_name":
		return strings.Contains(lower, "what name") ||
			strings.Contains(lower, "name should")
	case "customer_phone":
		return strings.Contains(lower, "what phone") ||
			strings.Contains(lower, "phone number")
	case "staff":
		return strings.Contains(lower, "which technician") ||
			strings.Contains(lower, "what technician") ||
			strings.Contains(lower, "which nail tech")
	default:
		return false
	}
}

func asksToConfirmClock(message string) bool {
	if !replyClockReferencePattern.MatchString(message) {
		return false
	}
	return (strings.Contains(message, "does") && strings.Contains(message, "work")) ||
		strings.Contains(message, "would you like") ||
		strings.Contains(message, "do you want") ||
		strings.Contains(message, "should i book")
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
