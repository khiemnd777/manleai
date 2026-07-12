package voice_openai

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/manleai/ai-receptionist/modules/voice"
)

const (
	twilioSampleRate       = 8000
	twilioFrameBytes       = 160 // 20 ms of 8 kHz mu-law audio.
	maxSpeechResponseBytes = 10 * 1024 * 1024
)

func (a *Adapter) StreamSpeech(ctx context.Context, salonID string, input voice.SpeechStreamRequest, onChunk func(voice.SpeechChunk) error) (voice.SpeechStreamResult, error) {
	cfg, enabled, err := a.configFor(ctx, salonID)
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
		"response_format": "wav",
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
		return voice.SpeechStreamResult{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return voice.SpeechStreamResult{}, fmt.Errorf("openai speech failed with status %d", res.StatusCode)
	}

	limited := &io.LimitedReader{R: res.Body, N: maxSpeechResponseBytes + 1}
	result, err := streamWAVAsTwilioMulaw(ctx, limited, onChunk)
	result.ProviderRequestID = strings.TrimSpace(res.Header.Get("x-request-id"))
	if limited.N == 0 {
		return result, errors.New("openai speech response exceeded the audio size limit")
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

type wavFormat struct {
	audioFormat   uint16
	channels      uint16
	sampleRate    uint32
	bitsPerSample uint16
}

func streamWAVAsTwilioMulaw(ctx context.Context, reader io.Reader, onChunk func(voice.SpeechChunk) error) (voice.SpeechStreamResult, error) {
	result := voice.SpeechStreamResult{Encoding: "audio/x-mulaw", SampleRate: twilioSampleRate}
	header := make([]byte, 12)
	if _, err := io.ReadFull(reader, header); err != nil {
		return result, fmt.Errorf("read wav header: %w", err)
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return result, errors.New("speech response is not a RIFF/WAVE stream")
	}

	var format wavFormat
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(reader, chunkHeader); err != nil {
			return result, fmt.Errorf("read wav chunk header: %w", err)
		}
		chunkID := string(chunkHeader[:4])
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:])
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 || chunkSize > 4096 {
				return result, fmt.Errorf("unsupported wav fmt chunk size %d", chunkSize)
			}
			data := make([]byte, chunkSize)
			if _, err := io.ReadFull(reader, data); err != nil {
				return result, fmt.Errorf("read wav fmt chunk: %w", err)
			}
			format = wavFormat{
				audioFormat:   binary.LittleEndian.Uint16(data[0:2]),
				channels:      binary.LittleEndian.Uint16(data[2:4]),
				sampleRate:    binary.LittleEndian.Uint32(data[4:8]),
				bitsPerSample: binary.LittleEndian.Uint16(data[14:16]),
			}
			if chunkSize%2 == 1 {
				if _, err := io.CopyN(io.Discard, reader, 1); err != nil {
					return result, err
				}
			}
		case "data":
			if format.audioFormat != 1 || format.bitsPerSample != 16 || format.channels == 0 || format.sampleRate == 0 {
				return result, fmt.Errorf("unsupported wav format: encoding=%d channels=%d sample_rate=%d bits=%d", format.audioFormat, format.channels, format.sampleRate, format.bitsPerSample)
			}
			dataReader := reader
			if chunkSize != 0 && chunkSize != ^uint32(0) {
				dataReader = io.LimitReader(reader, int64(chunkSize))
			}
			return streamPCM16AsMulaw(ctx, dataReader, format, result, onChunk)
		default:
			if chunkSize > maxSpeechResponseBytes {
				return result, fmt.Errorf("wav metadata chunk %q is too large", chunkID)
			}
			if _, err := io.CopyN(io.Discard, reader, int64(chunkSize)+(int64(chunkSize)&1)); err != nil {
				return result, fmt.Errorf("skip wav chunk %q: %w", chunkID, err)
			}
		}
	}
}

func streamPCM16AsMulaw(ctx context.Context, reader io.Reader, format wavFormat, result voice.SpeechStreamResult, onChunk func(voice.SpeechChunk) error) (voice.SpeechStreamResult, error) {
	frameBytes := int(format.channels) * 2
	readBuffer := make([]byte, 4096)
	pendingPCM := make([]byte, 0, 4096+frameBytes)
	pendingMulaw := make([]byte, 0, twilioFrameBytes*2)
	var inputIndex uint64
	var outputIndex uint64

	emit := func(final bool) error {
		for len(pendingMulaw) >= twilioFrameBytes || final && len(pendingMulaw) > 0 {
			size := twilioFrameBytes
			if len(pendingMulaw) < size {
				size = len(pendingMulaw)
			}
			audio := append([]byte(nil), pendingMulaw[:size]...)
			pendingMulaw = pendingMulaw[size:]
			chunk := voice.SpeechChunk{Sequence: result.ChunkCount, Audio: audio}
			if err := onChunk(chunk); err != nil {
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
			completeBytes := len(pendingPCM) - len(pendingPCM)%frameBytes
			for offset := 0; offset < completeBytes; offset += frameBytes {
				var mixed int32
				for channel := 0; channel < int(format.channels); channel++ {
					start := offset + channel*2
					mixed += int32(int16(binary.LittleEndian.Uint16(pendingPCM[start : start+2])))
				}
				sample := int16(mixed / int32(format.channels))
				if inputIndex*twilioSampleRate >= outputIndex*uint64(format.sampleRate) {
					pendingMulaw = append(pendingMulaw, linearPCMToMulaw(sample))
					outputIndex++
				}
				inputIndex++
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
				return result, errors.New("wav data ended mid-sample")
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
