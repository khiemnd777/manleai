package voice

import (
	"context"
	"errors"
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

type fakeActModelProvider struct {
	configured bool
	reply      TurnModelReply
	request    TurnModelRequest
	err        error
}

func (f *fakeActModelProvider) Name() string { return ProviderOpenAI }

func (f *fakeActModelProvider) Configured(ctx context.Context, salonID string) bool {
	return f.configured
}

func (f *fakeActModelProvider) InterpretTurn(ctx context.Context, req TurnModelRequest) (TurnModelReply, error) {
	f.request = req
	return f.reply, f.err
}

func TestGuardedTurnInterpreterMapsStructuredResultWithoutPIIExpansion(t *testing.T) {
	provider := &fakeActModelProvider{configured: true, reply: TurnModelReply{
		Goal: "book_appointment", Confidence: 0.94, Acts: []ActModelReply{{
			Kind: conversation.ConversationActReplace, Entity: conversation.ConversationEntityService,
			SourceIDs: []string{"service_gel"}, TargetIDs: []string{"service_spa"},
			Scope: conversation.ConversationScopeOne, Confidence: 0.94, Reason: "caller requested a replacement",
		}},
	}}
	interpreter := NewGuardedTurnInterpreter(provider)
	turn, err := interpreter.InterpretTurn(context.Background(), conversation.TurnInterpretationRequest{
		SalonID:         "salon_1",
		SessionID:       "session_1",
		Channel:         conversation.ChannelPhone,
		CustomerMessage: "Make that a spa pedicure instead.",
		ExpectedInput:   conversation.ExpectedInputService,
		SelectedServices: []conversation.ConversationServiceRef{{
			ServiceID: "service_gel", ServiceName: "Gel Manicure",
		}},
		CatalogServices: []conversation.ConversationServiceRef{{
			ServiceID: "service_spa", ServiceName: "Spa Pedicure",
		}},
	})
	if err != nil {
		t.Fatalf("InterpretTurn: %v", err)
	}
	act := turn.Acts[0]
	if act.Kind != conversation.ConversationActReplace || act.Source != "structured_ai" || act.Confidence != 0.94 {
		t.Fatalf("act = %#v", act)
	}
	if provider.request.CustomerMessage != "Make that a spa pedicure instead." || provider.request.SalonID != "salon_1" || provider.request.ExpectedInput != conversation.ExpectedInputService {
		t.Fatalf("provider request = %#v", provider.request)
	}
}

func TestGuardedTurnInterpreterMapsStructuredAvailabilityTimePreference(t *testing.T) {
	provider := &fakeActModelProvider{configured: true, reply: TurnModelReply{
		Goal: "book_appointment", Confidence: 0.93,
		Questions: []QuestionModelReply{{
			Subject:        conversation.ConversationQuestionAvailability,
			TimePreference: TimePreferenceModelReply{Direction: conversation.TimePreferenceBefore, Minutes: 14*60 + 30},
			Confidence:     0.93, Reason: "caller requested an earlier time",
		}},
	}}
	interpreter := NewGuardedTurnInterpreter(provider)

	turn, err := interpreter.InterpretTurn(context.Background(), conversation.TurnInterpretationRequest{SalonID: "salon_1"})
	if err != nil {
		t.Fatalf("InterpretTurn: %v", err)
	}
	if len(turn.Questions) != 1 || turn.Questions[0].TimePreference == nil || turn.Questions[0].TimePreference.Direction != conversation.TimePreferenceBefore || turn.Questions[0].TimePreference.Minutes != 14*60+30 {
		t.Fatalf("turn = %#v", turn)
	}
}

func TestGuardedTurnInterpreterMapsConsultationNeedsWithoutRecommendationAuthority(t *testing.T) {
	provider := &fakeActModelProvider{configured: true, reply: TurnModelReply{
		Goal: "consultation", Confidence: 0.96, Reason: "caller described desired nail outcome",
		Consultation: ConsultationModelReply{
			CurrentSystem: conversation.ConsultationSystemAcrylic, DesiredOutcome: conversation.ConsultationOutcomeShorten,
			LengthChange: conversation.ConsultationLengthShorten, Priorities: []string{conversation.ConsultationPriorityLowerMaintenance},
			DesiredFinishes:    []string{conversation.ConsultationFinishMatte},
			ComparedServiceIDs: []string{"service_acrylic_fill"}, Confidence: 0.96, Reason: "structured needs only",
			Mutations: []ConsultationMutationModelReply{{
				Field: conversation.ConsultationNeedFieldCurrentSystem, Operation: conversation.ConsultationNeedOperationReplace,
				Values: []string{conversation.ConsultationSystemAcrylic}, Confidence: 0.96, Reason: "caller corrected current system",
			}},
		},
		Safety: SafetyModelReply{Concern: true, Category: conversation.SafetyCategoryAllergy, Confidence: 0.98, Reason: "caller asked about an allergic reaction"},
	}}
	interpreter := NewGuardedTurnInterpreter(provider)
	current := &conversation.ConsultationState{Status: conversation.ConsultationStatusCollectingNeeds}

	turn, err := interpreter.InterpretTurn(context.Background(), conversation.TurnInterpretationRequest{
		SalonID: "salon_1", CustomerMessage: "My acrylics are too long and I want less upkeep.", Consultation: current,
	})
	if err != nil {
		t.Fatalf("InterpretTurn: %v", err)
	}
	if turn.Goal != "consultation" || turn.Consultation.CurrentSystem != conversation.ConsultationSystemAcrylic ||
		turn.Consultation.DesiredOutcome != conversation.ConsultationOutcomeShorten || len(turn.Consultation.Priorities) != 1 ||
		len(turn.Consultation.DesiredFinishes) != 1 || turn.Consultation.DesiredFinishes[0] != conversation.ConsultationFinishMatte {
		t.Fatalf("turn = %#v", turn)
	}
	if len(turn.Acts) != 0 || len(turn.Questions) != 0 {
		t.Fatalf("consultation extraction gained action authority: %#v", turn)
	}
	if len(turn.ConsultationMutations) != 1 || turn.ConsultationMutations[0].Operation != conversation.ConsultationNeedOperationReplace ||
		!turn.Safety.Concern || turn.Safety.Category != conversation.SafetyCategoryAllergy {
		t.Fatalf("consultation mutation or safety extraction was lost: %#v", turn)
	}
	if provider.request.Consultation != current {
		t.Fatalf("current consultation state was not passed to provider: %#v", provider.request.Consultation)
	}
}

func TestGuardedTurnInterpreterRequiresConfiguredProvider(t *testing.T) {
	interpreter := NewGuardedTurnInterpreter(&fakeActModelProvider{})
	if _, err := interpreter.InterpretTurn(context.Background(), conversation.TurnInterpretationRequest{SalonID: "salon_1"}); !errors.Is(err, ErrProviderDisabled) {
		t.Fatalf("error = %v, want ErrProviderDisabled", err)
	}
}

func TestGuardedTurnInterpreterClassifiesProviderFailures(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		outcome string
	}{
		{name: "timeout", err: context.DeadlineExceeded, outcome: conversation.TurnInterpreterOutcomeTimeout},
		{name: "empty", err: ErrTurnModelEmptyOutput, outcome: conversation.TurnInterpreterOutcomeEmptyOutput},
		{name: "invalid", err: ErrTurnModelInvalidOutput, outcome: conversation.TurnInterpreterOutcomeSchemaInvalid},
		{name: "provider", err: errors.New("upstream unavailable"), outcome: conversation.TurnInterpreterOutcomeProviderError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			interpreter := NewGuardedTurnInterpreter(&fakeActModelProvider{configured: true, err: test.err})
			_, err := interpreter.InterpretTurn(context.Background(), conversation.TurnInterpretationRequest{SalonID: "salon_1"})
			var typed *conversation.TurnInterpreterError
			if !errors.As(err, &typed) || typed.Outcome != test.outcome {
				t.Fatalf("error = %#v, want outcome %q", err, test.outcome)
			}
		})
	}
}
