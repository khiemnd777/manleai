package voice_openai

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/manleai/ai-receptionist/modules/voice"
)

const (
	openAISpeechSampleRate = 24000
	twilioSampleRate       = 8000
	twilioFrameBytes       = 160 // 20 ms of 8 kHz mu-law audio.
	maxSpeechResponseBytes = 10 * 1024 * 1024
)

func (a *Adapter) StreamSpeech(ctx context.Context, salonID string, input voice.SpeechStreamRequest, onChunk func(voice.SpeechChunk) error) (voice.SpeechStreamResult, error) {
	cfg, enabled, _, err := a.configFor(ctx, salonID)
	if err != nil {
		return voice.SpeechStreamResult{}, err
	}
	if !enabled || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.SpeechModel) == "" {
		return voice.SpeechStreamResult{}, voice.ErrProviderDisabled
	}
	text := strings.TrimSpace(input.Text)
	voiceName := strings.TrimSpace(input.Voice)
	if voiceName == "" {
		voiceName = strings.TrimSpace(cfg.SpeechVoice)
	}
	if text == "" || voiceName == "" || onChunk == nil {
		return voice.SpeechStreamResult{}, voice.ErrValidation
	}

	payload := map[string]any{
		"model":           strings.TrimSpace(cfg.SpeechModel),
		"voice":           voiceName,
		"input":           text,
		"response_format": "pcm",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return voice.SpeechStreamResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(cfg, "/audio/speech"), bytes.NewReader(raw))
	if err != nil {
		return voice.SpeechStreamResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if requestID := strings.TrimSpace(input.RequestID); requestID != "" {
		req.Header.Set("X-Client-Request-Id", requestID)
	}
	a.authorize(req, cfg)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return voice.SpeechStreamResult{}, &voice.ProviderRequestError{Provider: voice.ProviderOpenAI, Stage: "speech_stream_response", Err: err}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return voice.SpeechStreamResult{}, providerResponseError(res, "speech_stream_response")
	}

	limited := &io.LimitedReader{R: res.Body, N: maxSpeechResponseBytes + 1}
	result, err := streamPCM24kAsTwilioMulaw(ctx, limited, onChunk)
	result.ProviderRequestID = strings.TrimSpace(res.Header.Get("x-request-id"))
	if limited.N == 0 {
		return result, errors.New("openai speech response exceeded the audio size limit")
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func streamPCM24kAsTwilioMulaw(ctx context.Context, reader io.Reader, onChunk func(voice.SpeechChunk) error) (voice.SpeechStreamResult, error) {
	result := voice.SpeechStreamResult{Encoding: "audio/x-mulaw", SampleRate: twilioSampleRate}
	resampler := newSpeechResampler()
	readBuffer := make([]byte, 4096)
	pendingPCM := make([]byte, 0, len(readBuffer)+1)
	pendingMulaw := make([]byte, 0, twilioFrameBytes*2)

	emit := func(final bool) error {
		for len(pendingMulaw) >= twilioFrameBytes || final && len(pendingMulaw) > 0 {
			size := twilioFrameBytes
			if len(pendingMulaw) < size {
				size = len(pendingMulaw)
			}
			audio := append([]byte(nil), pendingMulaw[:size]...)
			pendingMulaw = pendingMulaw[size:]
			if err := onChunk(voice.SpeechChunk{Sequence: result.ChunkCount, Audio: audio}); err != nil {
				return err
			}
			result.ChunkCount++
			result.AudioBytes += len(audio)
		}
		return nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n, readErr := reader.Read(readBuffer)
		if n > 0 {
			pendingPCM = append(pendingPCM, readBuffer[:n]...)
			completeBytes := len(pendingPCM) - len(pendingPCM)%2
			for offset := 0; offset < completeBytes; offset += 2 {
				sample := int16(binary.LittleEndian.Uint16(pendingPCM[offset : offset+2]))
				if output, ok := resampler.Push(sample); ok {
					pendingMulaw = append(pendingMulaw, linearPCMToMulaw(output))
				}
			}
			pendingPCM = append(pendingPCM[:0], pendingPCM[completeBytes:]...)
			if err := emit(false); err != nil {
				return result, err
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return result, readErr
			}
			if len(pendingPCM) != 0 {
				return result, errors.New("PCM data ended mid-sample")
			}
			for _, output := range resampler.Flush() {
				pendingMulaw = append(pendingMulaw, linearPCMToMulaw(output))
			}
			if err := emit(true); err != nil {
				return result, err
			}
			if result.AudioBytes == 0 {
				return result, errors.New("speech response contained no audio")
			}
			return result, nil
		}
	}
}

func linearPCMToMulaw(sample int16) byte {
	const bias = 0x84
	const clip = 32635

	pcm := int(sample)
	sign := byte(0)
	if pcm < 0 {
		sign = 0x80
		pcm = -pcm
	}
	if pcm > clip {
		pcm = clip
	}
	pcm += bias
	exponent := 7
	for mask := 0x4000; exponent > 0 && pcm&mask == 0; mask >>= 1 {
		exponent--
	}
	mantissa := (pcm >> (exponent + 3)) & 0x0f
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}
