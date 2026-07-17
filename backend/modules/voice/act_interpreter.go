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
		SalonID:                     req.SalonID,
		SessionID:                   req.SessionID,
		Channel:                     req.Channel,
		CustomerMessage:             req.CustomerMessage,
		ExpectedInput:               req.ExpectedInput,
		SemanticContract:            req.SemanticContract,
		RecognizableGuidanceActions: append([]string(nil), req.RecognizableGuidanceActions...),
		SelectedServices:            append([]conversation.ConversationServiceRef(nil), req.SelectedServices...),
		CatalogServices:             append([]conversation.ConversationServiceRef(nil), req.CatalogServices...),
		CatalogServiceAliases:       append([]conversation.ConversationServiceAliasRef(nil), req.CatalogServiceAliases...),
		CatalogCategories:           append([]conversation.ConversationCategoryRef(nil), req.CatalogCategories...),
		SelectedStaff:               append([]conversation.ConversationStaffRef(nil), req.SelectedStaff...),
		CatalogStaff:                append([]conversation.ConversationStaffRef(nil), req.CatalogStaff...),
		Pending:                     req.Pending,
		CurrentBookingStage:         req.CurrentBookingStage,
		BookingAction:               req.BookingAction,
		CurrentDraft:                req.CurrentDraft,
		Consultation:                req.Consultation,
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
		return conversation.TurnUnderstanding{}, conversation.NewTurnInterpreterErrorWithDiagnostics(outcome, err, safeTurnProviderDiagnostics(err))
	}
	return TurnUnderstandingFromModelReply(reply), nil
}

// TurnUnderstandingFromModelReply is the single provider-neutral conversion
// used by production interpretation and the no-side-effect evaluator after a
// retained structured model result has already been obtained.
func TurnUnderstandingFromModelReply(reply TurnModelReply) conversation.TurnUnderstanding {
	turn := conversation.TurnUnderstanding{
		Goal: reply.Goal, GuidanceAction: strings.TrimSpace(reply.GuidanceAction), GuidanceCatalogMode: strings.TrimSpace(reply.GuidanceCatalogMode), GuidanceQuestionSubject: strings.TrimSpace(reply.GuidanceQuestionSubject), GuidancePartySize: reply.GuidancePartySize, Confidence: reply.Confidence, Reason: strings.TrimSpace(reply.Reason), Source: "structured_ai", ModelInvoked: true,
		InterpreterDiagnostics: cloneStringMap(reply.Diagnostics),
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
	droppedConsultationMutations := 0
	for _, mutation := range reply.Consultation.Mutations {
		if !modelConsultationMutationMatchesSnapshot(mutation, reply.Consultation) {
			droppedConsultationMutations++
			continue
		}
		turn.ConsultationMutations = append(turn.ConsultationMutations, conversation.ConsultationNeedMutation{
			Field: strings.TrimSpace(mutation.Field), Operation: strings.TrimSpace(mutation.Operation),
			Values: append([]string(nil), mutation.Values...), Confidence: mutation.Confidence,
			Reason: strings.TrimSpace(mutation.Reason),
		})
	}
	if droppedConsultationMutations > 0 {
		if turn.InterpreterDiagnostics == nil {
			turn.InterpreterDiagnostics = map[string]string{}
		}
		turn.InterpreterDiagnostics["consultation_mutation_snapshot_mismatch"] = "dropped"
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
			timePreference = &conversation.TimePreference{
				Direction: direction,
				Minutes:   normalizedModelTimePreferenceMinutes(direction, question.TimePreference.Minutes),
			}
		}
		turn.Questions = append(turn.Questions, conversation.ConversationQuestion{
			Subject: strings.TrimSpace(question.Subject), Mode: strings.TrimSpace(question.Mode), ServiceIDs: append([]string(nil), question.ServiceIDs...),
			StaffIDs: append([]string(nil), question.StaffIDs...), TimePreference: timePreference,
			Confidence: question.Confidence, Reason: strings.TrimSpace(question.Reason),
		})
	}
	return turn
}

func modelConsultationMutationMatchesSnapshot(mutation ConsultationMutationModelReply, snapshot ConsultationModelReply) bool {
	operation := strings.TrimSpace(mutation.Operation)
	if operation == conversation.ConsultationNeedOperationRemove || operation == conversation.ConsultationNeedOperationClear {
		return true
	}
	if operation != conversation.ConsultationNeedOperationSet && operation != conversation.ConsultationNeedOperationReplace && operation != conversation.ConsultationNeedOperationAdd {
		return true
	}
	values := trimmedNonEmptyStrings(mutation.Values)
	if len(values) == 0 {
		return true
	}
	field := strings.TrimSpace(mutation.Field)
	switch field {
	case conversation.ConsultationNeedFieldCurrentSystem:
		return len(values) == 1 && strings.TrimSpace(snapshot.CurrentSystem) == values[0]
	case conversation.ConsultationNeedFieldDesiredOutcome:
		return len(values) == 1 && strings.TrimSpace(snapshot.DesiredOutcome) == values[0]
	case conversation.ConsultationNeedFieldLengthChange:
		return len(values) == 1 && strings.TrimSpace(snapshot.LengthChange) == values[0]
	case conversation.ConsultationNeedFieldPriorities:
		return containsAllTrimmed(snapshot.Priorities, values)
	case conversation.ConsultationNeedFieldDesiredFinishes:
		return containsAllTrimmed(snapshot.DesiredFinishes, values)
	case conversation.ConsultationNeedFieldComparedServiceIDs:
		return containsAllTrimmed(snapshot.ComparedServiceIDs, values)
	default:
		return true
	}
}

func trimmedNonEmptyStrings(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}

func containsAllTrimmed(actual []string, expected []string) bool {
	values := map[string]bool{}
	for _, value := range actual {
		values[strings.TrimSpace(value)] = true
	}
	for _, value := range expected {
		if !values[value] {
			return false
		}
	}
	return true
}

// normalizedModelTimePreferenceMinutes accepts the typed protocol's canonical
// minutes-after-midnight value and the bounded clock-hour shorthand models can
// emit for a directional time-of-day constraint. It does not inspect caller
// wording or infer a time from conversation text.
func normalizedModelTimePreferenceMinutes(direction string, minutes int) int {
	switch strings.TrimSpace(direction) {
	case conversation.TimePreferenceBefore, conversation.TimePreferenceAfter, conversation.TimePreferenceExact:
		if minutes >= 0 && minutes <= 23 {
			return minutes * 60
		}
	}
	return minutes
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func safeTurnProviderDiagnostics(err error) map[string]string {
	type safeDiagnosticError interface {
		SafeDiagnostics() map[string]string
	}
	var diagnosticErr safeDiagnosticError
	if errors.As(err, &diagnosticErr) {
		return diagnosticErr.SafeDiagnostics()
	}
	return nil
}
