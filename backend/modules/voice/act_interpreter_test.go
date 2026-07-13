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
