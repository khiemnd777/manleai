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
		ReplyPolicy:      conversation.ReplyPolicyStyleOnly,
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
		Channel:     conversation.ChannelPhone,
		SafeReply:   "What phone number should we use?",
		AITone:      "natural_human",
		ReplyPolicy: conversation.ReplyPolicyStyleOnly,
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
		ReplyPolicy:        conversation.ReplyPolicyStyleOnly,
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
		ReplyPolicy:        conversation.ReplyPolicyStyleOnly,
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
		ReplyPolicy:       conversation.ReplyPolicyStyleOnly,
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
		ReplyPolicy:          conversation.ReplyPolicyStyleOnly,
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
		Channel:     conversation.ChannelSimulator,
		SafeReply:   "What phone number should we use?",
		ReplyPolicy: conversation.ReplyPolicyStyleOnly,
	}); err == nil {
		t.Fatalf("GenerateReply should not run for simulator channel")
	}
}

func TestGuardedReplyGeneratorRejectsOperationalFactByDefault(t *testing.T) {
	provider := &fakeLanguageModelProvider{
		reply: ModelReply{Message: "I found noon.", Confidence: 0.9},
	}
	generator := NewGuardedReplyGenerator(provider)

	if _, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:   conversation.ChannelPhone,
		SafeReply: "I found noon.",
	}); err == nil {
		t.Fatalf("GenerateReply should reject an operational-fact turn without explicit style-only policy")
	}
	if provider.req.SafeReply != "" {
		t.Fatalf("provider should not be called for operational-fact turn")
	}
}

func TestGuardedReplyGeneratorPhrasesOnlyProfileBackedConsultationQuestion(t *testing.T) {
	provider := &fakeLanguageModelProvider{reply: ModelReply{
		Message: "Would you prefer a glossy or matte finish?", Confidence: 0.92,
	}}
	generator := NewGuardedReplyGenerator(provider)
	spec := conversation.ConsultationQuestionSpec{
		Field:               conversation.ConsultationNeedFieldDesiredFinishes,
		Options:             []string{conversation.ConsultationFinishGlossy, conversation.ConsultationFinishMatte},
		CandidateServiceIDs: []string{"service_dynamic_a", "service_dynamic_b"},
		ProfileRevisions:    map[string]int{"service_dynamic_a": 4, "service_dynamic_b": 9},
	}

	reply, err := generator.GenerateConsultationQuestion(context.Background(), conversation.ConsultationQuestionRequest{
		SalonID: "salon_1", SessionID: "session_1", Channel: conversation.ChannelPhone, AITone: "concise_calm", Question: spec,
	})
	if err != nil || reply.Message != "Would you prefer a glossy or matte finish?" {
		t.Fatalf("consultation question reply=%#v err=%v", reply, err)
	}
	if provider.req.ReplyPolicy != conversation.ReplyPolicyConsultationQuestion || provider.req.ConsultationQuestion == nil ||
		provider.req.ConsultationQuestion.Field != spec.Field || !sameStringValues(provider.req.ConsultationQuestion.Options, spec.Options) {
		t.Fatalf("consultation model request = %#v", provider.req)
	}
}

func TestGuardedReplyGeneratorRejectsUnsafeConsultationQuestionShape(t *testing.T) {
	for _, message := range []string{
		"Would you prefer glossy? Or would matte be better?",
		"Your appointment is confirmed; which finish would you prefer?",
		"Hey! Which finish would you prefer?",
	} {
		generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{reply: ModelReply{Message: message, Confidence: 0.9}})
		_, err := generator.GenerateConsultationQuestion(context.Background(), conversation.ConsultationQuestionRequest{
			SalonID: "salon_1", Question: conversation.ConsultationQuestionSpec{
				Field:   conversation.ConsultationNeedFieldDesiredFinishes,
				Options: []string{conversation.ConsultationFinishGlossy, conversation.ConsultationFinishMatte},
			},
		})
		if err == nil {
			t.Fatalf("unsafe consultation question accepted: %q", message)
		}
	}
}

func sameStringValues(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
