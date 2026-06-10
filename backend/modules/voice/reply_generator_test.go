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
	generator := NewGuardedReplyGenerator(&fakeLanguageModelProvider{
		reply: ModelReply{Message: "What phone number should we use?", Confidence: 0.9},
	})

	reply, err := generator.GenerateReply(context.Background(), conversation.ReplyGenerationRequest{
		Channel:   conversation.ChannelPhone,
		SafeReply: "What phone number should we use?",
	})
	if err != nil {
		t.Fatalf("GenerateReply returned error: %v", err)
	}
	if reply.Message != "What phone number should we use?" {
		t.Fatalf("message = %q", reply.Message)
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
}

func (f *fakeLanguageModelProvider) Name() string {
	return ProviderOpenAI
}

func (f *fakeLanguageModelProvider) Configured() bool {
	return true
}

func (f *fakeLanguageModelProvider) GenerateReply(ctx context.Context, req ModelRequest) (ModelReply, error) {
	return f.reply, nil
}
