package voice

import (
	"context"
	"strings"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

type GuardedTurnInterpreter struct {
	provider TurnModelProvider
}

func NewGuardedTurnInterpreter(provider TurnModelProvider) *GuardedTurnInterpreter {
	return &GuardedTurnInterpreter{provider: provider}
}

func (g *GuardedTurnInterpreter) InterpretTurn(ctx context.Context, req conversation.TurnInterpretationRequest) (conversation.TurnUnderstanding, error) {
	if g == nil || g.provider == nil || !g.provider.Configured(ctx, req.SalonID) {
		return conversation.TurnUnderstanding{}, ErrProviderDisabled
	}
	reply, err := g.provider.InterpretTurn(ctx, TurnModelRequest{
		SalonID:             req.SalonID,
		SessionID:           req.SessionID,
		Channel:             req.Channel,
		CustomerMessage:     req.CustomerMessage,
		SelectedServices:    append([]conversation.ConversationServiceRef(nil), req.SelectedServices...),
		CatalogServices:     append([]conversation.ConversationServiceRef(nil), req.CatalogServices...),
		SelectedStaff:       append([]conversation.ConversationStaffRef(nil), req.SelectedStaff...),
		CatalogStaff:        append([]conversation.ConversationStaffRef(nil), req.CatalogStaff...),
		Pending:             req.Pending,
		CurrentBookingStage: req.CurrentBookingStage,
		BookingAction:       req.BookingAction,
		CurrentDraft:        req.CurrentDraft,
	})
	if err != nil {
		return conversation.TurnUnderstanding{}, err
	}
	turn := conversation.TurnUnderstanding{
		Goal: reply.Goal, Confidence: reply.Confidence, Reason: strings.TrimSpace(reply.Reason), Source: "structured_ai", ModelInvoked: true,
	}
	for _, act := range reply.Acts {
		turn.Acts = append(turn.Acts, conversation.ConversationAct{
			Kind: strings.TrimSpace(act.Kind), Entity: strings.TrimSpace(act.Entity),
			SourceServiceIDs: append([]string(nil), act.SourceIDs...), TargetServiceIDs: append([]string(nil), act.TargetIDs...),
			SourceCategoryID: strings.TrimSpace(act.SourceCategoryID), SourceCategoryName: strings.TrimSpace(act.SourceCategoryName),
			TargetCategoryID: strings.TrimSpace(act.TargetCategoryID), TargetCategoryName: strings.TrimSpace(act.TargetCategoryName),
			Scope: strings.TrimSpace(act.Scope), GuestScope: strings.TrimSpace(act.GuestScope), GuestRef: strings.TrimSpace(act.GuestRef), Subject: strings.TrimSpace(act.Subject),
			Value: strings.TrimSpace(act.Value), Count: act.Count,
			Confidence: act.Confidence, Reason: strings.TrimSpace(act.Reason), Source: "structured_ai",
		})
	}
	for _, question := range reply.Questions {
		turn.Questions = append(turn.Questions, conversation.ConversationQuestion{
			Subject: strings.TrimSpace(question.Subject), ServiceIDs: append([]string(nil), question.ServiceIDs...),
			StaffIDs: append([]string(nil), question.StaffIDs...), Confidence: question.Confidence, Reason: strings.TrimSpace(question.Reason),
		})
	}
	return turn, nil
}
