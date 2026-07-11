package voice

import (
	"context"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

type GuardedConversationActInterpreter struct {
	provider ConversationActModelProvider
}

func NewGuardedConversationActInterpreter(provider ConversationActModelProvider) *GuardedConversationActInterpreter {
	return &GuardedConversationActInterpreter{provider: provider}
}

func (g *GuardedConversationActInterpreter) InterpretConversationAct(ctx context.Context, req conversation.ConversationActInterpretationRequest) (conversation.ConversationAct, error) {
	if g == nil || g.provider == nil || !g.provider.Configured(ctx, req.SalonID) {
		return conversation.ConversationAct{}, ErrProviderDisabled
	}
	reply, err := g.provider.ClassifyConversationAct(ctx, ActModelRequest{
		SalonID:             req.SalonID,
		SessionID:           req.SessionID,
		Channel:             req.Channel,
		CustomerMessage:     req.CustomerMessage,
		SelectedServices:    append([]conversation.ConversationServiceRef(nil), req.SelectedServices...),
		CatalogServices:     append([]conversation.ConversationServiceRef(nil), req.CatalogServices...),
		Pending:             req.Pending,
		CurrentBookingStage: req.CurrentBookingStage,
	})
	if err != nil {
		return conversation.ConversationAct{}, err
	}
	return conversation.ConversationAct{
		Kind:               strings.TrimSpace(reply.Kind),
		SourceServiceIDs:   append([]string(nil), reply.SourceServiceIDs...),
		TargetServiceIDs:   append([]string(nil), reply.TargetServiceIDs...),
		SourceCategoryID:   strings.TrimSpace(reply.SourceCategoryID),
		SourceCategoryName: strings.TrimSpace(reply.SourceCategoryName),
		TargetCategoryID:   strings.TrimSpace(reply.TargetCategoryID),
		TargetCategoryName: strings.TrimSpace(reply.TargetCategoryName),
		Scope:              strings.TrimSpace(reply.Scope),
		GuestScope:         strings.TrimSpace(reply.GuestScope),
		Subject:            strings.TrimSpace(reply.Subject),
		Confidence:         reply.Confidence,
		Reason:             strings.TrimSpace(reply.Reason),
		Source:             "structured_ai",
	}, nil
}
