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
	return g.generateReply(ctx, req, false)
}

// GenerateEvaluationReply applies the exact production reply prompt and
// guardrails while permitting simulator-channel evaluation. Production callers
// continue through GenerateReply, which remains phone-only.
func (g *GuardedReplyGenerator) GenerateEvaluationReply(ctx context.Context, req conversation.ReplyGenerationRequest) (conversation.ReplyGenerationResult, error) {
	return g.generateReply(ctx, req, true)
}

func (g *GuardedReplyGenerator) generateReply(ctx context.Context, req conversation.ReplyGenerationRequest, allowSimulator bool) (conversation.ReplyGenerationResult, error) {
	if g == nil || g.provider == nil || !g.provider.Configured(ctx, req.SalonID) || (!allowSimulator && req.Channel != conversation.ChannelPhone) {
		return conversation.ReplyGenerationResult{}, ErrProviderDisabled
	}
	if req.Channel != conversation.ChannelPhone && req.Channel != conversation.ChannelSimulator {
		return conversation.ReplyGenerationResult{}, ErrValidation
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
		ConsultationQuestion: req.ConsultationQuestion,
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

func (g *GuardedReplyGenerator) GenerateConsultationQuestion(ctx context.Context, req conversation.ConsultationQuestionRequest) (conversation.ReplyGenerationResult, error) {
	if g == nil || g.provider == nil || !g.provider.Configured(ctx, req.SalonID) {
		return conversation.ReplyGenerationResult{}, ErrProviderDisabled
	}
	if strings.TrimSpace(req.Question.Field) == "" || len(req.Question.Options) == 0 {
		return conversation.ReplyGenerationResult{}, errUnsafeReply
	}
	reply, err := g.provider.GenerateReply(ctx, ModelRequest{
		SalonID: req.SalonID, SessionID: req.SessionID, Channel: req.Channel, AITone: req.AITone,
		ReplyPolicy: conversation.ReplyPolicyConsultationQuestion, ConsultationQuestion: &req.Question,
	})
	if err != nil {
		return conversation.ReplyGenerationResult{}, err
	}
	message := strings.TrimSpace(reply.Message)
	if !consultationQuestionAllowed(message, reply.Confidence) {
		if fallback := structuredConsultationQuestion(req.Question); fallback != "" {
			return conversation.ReplyGenerationResult{
				Message: fallback, Confidence: 1, Reason: "structured_consultation_question_fallback",
			}, nil
		}
		return conversation.ReplyGenerationResult{}, errUnsafeReply
	}
	return conversation.ReplyGenerationResult{Message: message, Confidence: reply.Confidence, Handoff: reply.Handoff, Reason: reply.Reason}, nil
}

func structuredConsultationQuestion(spec conversation.ConsultationQuestionSpec) string {
	labels := make([]string, 0, len(spec.Options))
	seen := map[string]bool{}
	for _, option := range spec.Options {
		label := consultationOptionSpokenLabel(spec.Field, option)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	options := joinSpokenOptions(labels)
	if options == "" {
		return ""
	}
	switch strings.TrimSpace(spec.Field) {
	case conversation.ConsultationNeedFieldCurrentSystem:
		return "I can help with that. What do you currently have on your nails: " + options + "?"
	case conversation.ConsultationNeedFieldDesiredOutcome:
		return "I can help with that. What result are you looking for: " + options + "?"
	case conversation.ConsultationNeedFieldLengthChange:
		return "Would you like to " + options + "?"
	case conversation.ConsultationNeedFieldPriorities:
		return "What matters most to you: " + options + "?"
	case conversation.ConsultationNeedFieldDesiredFinishes:
		return "What finish would you prefer: " + options + "?"
	default:
		return ""
	}
}

func consultationOptionSpokenLabel(field string, option string) string {
	field = strings.TrimSpace(field)
	option = strings.TrimSpace(option)
	switch field {
	case conversation.ConsultationNeedFieldCurrentSystem:
		switch option {
		case conversation.ConsultationSystemNatural:
			return "natural nails"
		case conversation.ConsultationSystemRegularPolish:
			return "regular polish"
		case conversation.ConsultationSystemGel:
			return "gel"
		case conversation.ConsultationSystemDip:
			return "dip powder"
		case conversation.ConsultationSystemAcrylic:
			return "acrylic"
		case conversation.ConsultationSystemExtension:
			return "extensions"
		}
	case conversation.ConsultationNeedFieldDesiredOutcome:
		switch option {
		case conversation.ConsultationOutcomeMaintain:
			return "maintain what you have"
		case conversation.ConsultationOutcomeShorten:
			return "shorter nails"
		case conversation.ConsultationOutcomeAddLength:
			return "add length"
		case conversation.ConsultationOutcomeAddStrength:
			return "more strength"
		case conversation.ConsultationOutcomeRepair:
			return "repair a nail"
		case conversation.ConsultationOutcomeRemoval:
			return "removal"
		case conversation.ConsultationOutcomeColorRefresh:
			return "refresh the color"
		case conversation.ConsultationOutcomeCompare:
			return "compare options"
		}
	case conversation.ConsultationNeedFieldLengthChange:
		switch option {
		case conversation.ConsultationLengthKeep:
			return "keep your current length"
		case conversation.ConsultationLengthShorten:
			return "shorten your nails"
		case conversation.ConsultationLengthAddLength:
			return "add length"
		}
	case conversation.ConsultationNeedFieldPriorities:
		switch option {
		case conversation.ConsultationPriorityDurability:
			return "durability"
		case conversation.ConsultationPriorityLowerMaintenance:
			return "lower maintenance"
		case conversation.ConsultationPriorityLowerCost:
			return "lower cost"
		case conversation.ConsultationPriorityShorterVisit:
			return "a shorter visit"
		}
	case conversation.ConsultationNeedFieldDesiredFinishes:
		switch option {
		case conversation.ConsultationFinishNatural:
			return "a natural finish"
		case conversation.ConsultationFinishRegularPolish:
			return "regular polish"
		case conversation.ConsultationFinishGelPolish:
			return "gel polish"
		case conversation.ConsultationFinishGlossy:
			return "glossy"
		case conversation.ConsultationFinishMatte:
			return "matte"
		case conversation.ConsultationFinishNailArt:
			return "nail art"
		}
	}
	return ""
}

func joinSpokenOptions(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	default:
		return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
	}
}

func consultationQuestionAllowed(message string, confidence float64) bool {
	if message == "" || strings.Count(message, "?") != 1 || len(strings.Fields(message)) > 35 {
		return false
	}
	if confidence > 0 && confidence < 0.55 {
		return false
	}
	return !hasUnsafeConfirmation(message) && !hasCasualOpener(message)
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
