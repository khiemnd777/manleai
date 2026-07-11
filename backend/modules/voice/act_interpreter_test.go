package voice

import (
	"context"
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

type fakeActModelProvider struct {
	configured bool
	reply      TurnModelReply
	request    TurnModelRequest
}

func (f *fakeActModelProvider) Name() string { return ProviderOpenAI }

func (f *fakeActModelProvider) Configured(ctx context.Context, salonID string) bool {
	return f.configured
}

func (f *fakeActModelProvider) InterpretTurn(ctx context.Context, req TurnModelRequest) (TurnModelReply, error) {
	f.request = req
	return f.reply, nil
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
	if provider.request.CustomerMessage != "Make that a spa pedicure instead." || provider.request.SalonID != "salon_1" {
		t.Fatalf("provider request = %#v", provider.request)
	}
}

func TestGuardedTurnInterpreterRequiresConfiguredProvider(t *testing.T) {
	interpreter := NewGuardedTurnInterpreter(&fakeActModelProvider{})
	if _, err := interpreter.InterpretTurn(context.Background(), conversation.TurnInterpretationRequest{SalonID: "salon_1"}); err != ErrProviderDisabled {
		t.Fatalf("error = %v, want ErrProviderDisabled", err)
	}
}
