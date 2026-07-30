package voice_openai

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"

	"github.com/manleai/ai-receptionist/internal/openairuntime"
	"github.com/manleai/ai-receptionist/modules/conversation"
	"github.com/manleai/ai-receptionist/modules/voice"
)

// VerifyCapability exercises the same tenant-bound adapter path used by live
// calls. Callers must invoke it only from an explicit, durable verification
// run; it can incur provider usage.
func (a *Adapter) VerifyCapability(ctx context.Context, salonID, capability string) (string, error) {
	var err error
	switch strings.TrimSpace(capability) {
	case openairuntime.CapabilityTranscription:
		_, err = a.Transcribe(ctx, salonID, voice.SpeechToTextRequest{Audio: verificationWAV(), ContentType: "audio/wav"})
	case openairuntime.CapabilitySemanticFull:
		_, err = a.interpretTurn(ctx, voice.TurnModelRequest{
			SalonID: salonID, Channel: "runtime_verification", CustomerMessage: "I need an appointment.",
			ExpectedInput: "contract_validation", SemanticContract: conversation.TurnSemanticContractFull,
		}, true)
	case openairuntime.CapabilitySemanticGuide:
		_, err = a.interpretTurn(ctx, voice.TurnModelRequest{
			SalonID: salonID, Channel: "runtime_verification", CustomerMessage: "Help me choose a service.",
			ExpectedInput: "contract_validation", SemanticContract: conversation.TurnSemanticContractGuidance,
			RecognizableGuidanceActions: conversation.GuidanceActionValues(),
		}, true)
	case openairuntime.CapabilityReply:
		_, err = a.GenerateReply(ctx, voice.ModelRequest{
			SalonID: salonID, Channel: "runtime_verification", SafeReply: "How can I help with your appointment?",
		})
	case openairuntime.CapabilitySpeech:
		_, err = a.Synthesize(ctx, salonID, "OpenAI voice verification.", "")
	case openairuntime.CapabilitySpeechStream:
		_, err = a.StreamSpeech(ctx, salonID, voice.SpeechStreamRequest{
			RequestID: "runtime-verification", Text: "OpenAI streaming voice verification.",
		}, func(voice.SpeechChunk) error { return nil })
	case openairuntime.CapabilityRealtime:
		var session voice.RealtimeSession
		session, err = a.ConnectRealtime(ctx, salonID, voice.RealtimeSessionOptions{SessionID: "runtime-verification"})
		if err == nil {
			err = session.Close()
		}
	default:
		err = openairuntime.ErrInvalidConfig
	}
	if err == nil {
		return "", nil
	}
	var providerErr *voice.ProviderRequestError
	if errors.As(err, &providerErr) {
		return strings.TrimSpace(providerErr.RequestID), err
	}
	return "", err
}

func verificationWAV() []byte {
	const sampleRate = 8000
	const sampleCount = 800
	dataSize := sampleCount * 2
	result := make([]byte, 44+dataSize)
	copy(result[0:4], "RIFF")
	binary.LittleEndian.PutUint32(result[4:8], uint32(36+dataSize))
	copy(result[8:12], "WAVE")
	copy(result[12:16], "fmt ")
	binary.LittleEndian.PutUint32(result[16:20], 16)
	binary.LittleEndian.PutUint16(result[20:22], 1)
	binary.LittleEndian.PutUint16(result[22:24], 1)
	binary.LittleEndian.PutUint32(result[24:28], sampleRate)
	binary.LittleEndian.PutUint32(result[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(result[32:34], 2)
	binary.LittleEndian.PutUint16(result[34:36], 16)
	copy(result[36:40], "data")
	binary.LittleEndian.PutUint32(result[40:44], uint32(dataSize))
	return result
}
