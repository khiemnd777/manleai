package voice

import (
	"context"
	"testing"

	"github.com/manleai/ai-receptionist/modules/conversation"
)

func TestGuardedReplyGeneratorRejectsUnsafeConfirmation(t *testing.T) {
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "You are booked for 3pm. See you then.", Confidence: 0.9},
	})

	_, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:          conversation.ChannelPhone,
		SafeReply:        "I could not confirm that in Square Appointments. This is not a confirmed appointment.",
		BookingConfirmed: false,
	})
	if err == nil {
		t.Fatalf("GenerateReply accepted unsafe confirmation wording")
	}
}

func TestGuardedReplyGeneratorAcceptsOneQuestionPhoneReply(t *testing.T) {
	provider := &fakeLanguageModelProvider{
		reply: ModelReply{Message: "What phone number should we use?", Confidence: 0.9},
	}
	generator := NewGuardedReplyGenerator(provider)

	reply, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:   conversation.ChannelPhone,
		SafeReply: "What phone number should we use?",
		AITone:    "natural_human",
	})
	if err != nil {
		t.Fatalf("GenerateReply returned error: %v", err)
	}
	if reply.Message != "What phone number should we use?" {
		t.Fatalf("message = %q", reply.Message)
	}
	if provider.req.AITone != "natural_human" {
		t.Fatalf("model request tone = %q, want natural_human", provider.req.AITone)
	}
}

func TestGuardedReplyGeneratorRejectsAskingKnownDateAgain(t *testing.T) {
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "What day would you like to book your appointment?", Confidence: 0.9},
	})

	_, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:            conversation.ChannelPhone,
		SafeReply:          "I have Thursday. What time works best?",
		KnownBookingFields: []string{"service", "requested_date"},
		NextRequiredField:  "requested_time",
	})
	if err == nil {
		t.Fatalf("GenerateReply accepted a reply that asks for a known date")
	}
}

func TestGuardedReplyGeneratorRejectsConfirmingKnownTimeAgain(t *testing.T) {
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "Does 1:00 PM work for you?", Confidence: 0.9},
	})

	_, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:            conversation.ChannelPhone,
		SafeReply:          "What name should I put on the appointment?",
		KnownBookingFields: []string{"service", "requested_date", "requested_time", "requested_start_time"},
		NextRequiredField:  "customer_name",
	})
	if err == nil {
		t.Fatalf("GenerateReply accepted a reply that confirms a known time again")
	}
}

func TestGuardedReplyGeneratorRejectsCasualOpeners(t *testing.T) {
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "Hey! Great choice. What name should I put on the appointment?", Confidence: 0.9},
	})

	_, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:           conversation.ChannelPhone,
		SafeReply:         "What name should I put on the appointment?",
		NextRequiredField: "customer_name",
	})
	if err == nil {
		t.Fatalf("GenerateReply accepted casual opener")
	}
}

func TestGuardedReplyGeneratorRejectsDroppedSelectedService(t *testing.T) {
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "For your Classic Manicure, I found Monday, June 15 at 1:00 PM. Which works?", Confidence: 0.9},
	})

	_, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:              conversation.ChannelPhone,
		SafeReply:            "For your Classic Manicure and Gel Removal, I found these openings: first: Monday, June 15 at 1:00 PM. Which works?",
		SelectedServiceNames: []string{"Classic Manicure", "Gel Removal"},
		NextRequiredField:    "requested_time",
	})
	if err == nil {
		t.Fatalf("GenerateReply accepted rewrite that dropped selected service")
	}
}

func TestGuardedReplyGeneratorSkipsSimulatorChannel(t *testing.T) {
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "What phone number should we use?", Confidence: 0.9},
	})

	if _, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:   conversation.ChannelSimulator,
		SafeReply: "What phone number should we use?",
	}); err == nil {
		t.Fatalf("GenerateReply should not run for simulator channel")
	}
}

type fakeLanguageModelProvider struct {
	reply ModelReply
	req   ModelRequest
}

func (f *fakeLanguageModelProvider) Name() string {
	return ProviderOpenAI
}

func (f *fakeLanguageModelProvider) Configured(ctx context.Context, salonID string) bool {
	return true
}

func (f *fakeLanguageModelProvider) GenerateReply(ctx context.Context, req ModelRequest) (ModelReply, error) {
	f.req = req
	return f.reply, nil
}
