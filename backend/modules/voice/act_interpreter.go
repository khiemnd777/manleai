package voice

import (
	"context"
	"errors"
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
		return conversation.TurnUnderstanding{}, conversation.NewTurnInterpreterError(conversation.TurnInterpreterOutcomeProviderDisabled, ErrProviderDisabled)
	}
	reply, err := g.provider.InterpretTurn(ctx, TurnModelRequest{
		SalonID:             req.SalonID,
		SessionID:           req.SessionID,
		Channel:             req.Channel,
		CustomerMessage:     req.CustomerMessage,
		ExpectedInput:       req.ExpectedInput,
		SelectedServices:    append([]conversation.ConversationServiceRef(nil), req.SelectedServices...),
		CatalogServices:     append([]conversation.ConversationServiceRef(nil), req.CatalogServices...),
		SelectedStaff:       append([]conversation.ConversationStaffRef(nil), req.SelectedStaff...),
		CatalogStaff:        append([]conversation.ConversationStaffRef(nil), req.CatalogStaff...),
		Pending:             req.Pending,
		CurrentBookingStage: req.CurrentBookingStage,
		BookingAction:       req.BookingAction,
		CurrentDraft:        req.CurrentDraft,
		Consultation:        req.Consultation,
	})
	if err != nil {
		outcome := conversation.TurnInterpreterOutcomeProviderError
		switch {
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			outcome = conversation.TurnInterpreterOutcomeTimeout
		case errors.Is(err, ErrProviderDisabled):
			outcome = conversation.TurnInterpreterOutcomeProviderDisabled
		case errors.Is(err, ErrTurnModelEmptyOutput):
			outcome = conversation.TurnInterpreterOutcomeEmptyOutput
		case errors.Is(err, ErrTurnModelInvalidOutput):
			outcome = conversation.TurnInterpreterOutcomeSchemaInvalid
		}
		return conversation.TurnUnderstanding{}, conversation.NewTurnInterpreterError(outcome, err)
	}
	turn := conversation.TurnUnderstanding{
		Goal: reply.Goal, Confidence: reply.Confidence, Reason: strings.TrimSpace(reply.Reason), Source: "structured_ai", ModelInvoked: true,
		Consultation: conversation.ConsultationNeedProfile{
			CurrentSystem: strings.TrimSpace(reply.Consultation.CurrentSystem), DesiredOutcome: strings.TrimSpace(reply.Consultation.DesiredOutcome),
			LengthChange: strings.TrimSpace(reply.Consultation.LengthChange), Priorities: append([]string(nil), reply.Consultation.Priorities...),
			DesiredFinishes:    append([]string(nil), reply.Consultation.DesiredFinishes...),
			ComparedServiceIDs: append([]string(nil), reply.Consultation.ComparedServiceIDs...), BookingRequested: reply.Consultation.BookingRequested,
			ConversationComplete: reply.Consultation.ConversationComplete, Confidence: reply.Consultation.Confidence,
			Reason: strings.TrimSpace(reply.Consultation.Reason),
		},
		Safety: conversation.SafetyAssessment{
			Concern: reply.Safety.Concern, Category: strings.TrimSpace(reply.Safety.Category),
			Confidence: reply.Safety.Confidence, Reason: strings.TrimSpace(reply.Safety.Reason),
		},
	}
	for _, mutation := range reply.Consultation.Mutations {
		turn.ConsultationMutations = append(turn.ConsultationMutations, conversation.ConsultationNeedMutation{
			Field: strings.TrimSpace(mutation.Field), Operation: strings.TrimSpace(mutation.Operation),
			Values: append([]string(nil), mutation.Values...), Confidence: mutation.Confidence,
			Reason: strings.TrimSpace(mutation.Reason),
		})
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
		var timePreference *conversation.TimePreference
		direction := strings.TrimSpace(question.TimePreference.Direction)
		if direction != "" || question.TimePreference.Minutes != -1 {
			timePreference = &conversation.TimePreference{Direction: direction, Minutes: question.TimePreference.Minutes}
		}
		turn.Questions = append(turn.Questions, conversation.ConversationQuestion{
			Subject: strings.TrimSpace(question.Subject), ServiceIDs: append([]string(nil), question.ServiceIDs...),
			StaffIDs: append([]string(nil), question.StaffIDs...), TimePreference: timePreference,
			Confidence: question.Confidence, Reason: strings.TrimSpace(question.Reason),
		})
	}
	return turn, nil
}
