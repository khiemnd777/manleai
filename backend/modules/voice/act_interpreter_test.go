package voice

import (
	"context"
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

type fakeActModelProvider struct {
	configured bool
	reply      ActModelReply
	request    ActModelRequest
}

func (f *fakeActModelProvider) Name() string { return ProviderOpenAI }

func (f *fakeActModelProvider) Configured(ctx context.Context, salonID string) bool {
	return f.configured
}

func (f *fakeActModelProvider) ClassifyConversationAct(ctx context.Context, req ActModelRequest) (ActModelReply, error) {
	f.request = req
	return f.reply, nil
}

func TestGuardedConversationActInterpreterMapsStructuredResultWithoutPIIExpansion(t *testing.T) {
	provider := &fakeActModelProvider{configured: true, reply: ActModelReply{
		Kind:             conversation.ConversationActReplace,
		SourceServiceIDs: []string{"service_gel"},
		TargetServiceIDs: []string{"service_spa"},
		Scope:            conversation.ConversationScopeOne,
		Confidence:       0.94,
		Reason:           "caller requested a replacement",
	}}
	interpreter := NewGuardedConversationActInterpreter(provider)
	act, err := interpreter.InterpretConversationAct(context.Background(), conversation.ConversationActInterpretationRequest{
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
		t.Fatalf("InterpretConversationAct: %v", err)
	}
	if act.Kind != conversation.ConversationActReplace || act.Source != "structured_ai" || act.Confidence != 0.94 {
		t.Fatalf("act = %#v", act)
	}
	if provider.request.CustomerMessage != "Make that a spa pedicure instead." || provider.request.SalonID != "salon_1" {
		t.Fatalf("provider request = %#v", provider.request)
	}
}

func TestGuardedConversationActInterpreterRequiresConfiguredProvider(t *testing.T) {
	interpreter := NewGuardedConversationActInterpreter(&fakeActModelProvider{})
	if _, err := interpreter.InterpretConversationAct(context.Background(), conversation.ConversationActInterpretationRequest{SalonID: "salon_1"}); err != ErrProviderDisabled {
		t.Fatalf("error = %v, want ErrProviderDisabled", err)
	}
}
