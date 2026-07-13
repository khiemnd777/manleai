package voice_openai

import (
	"math"
	"testing"
)

func TestLinearPCMToMulawKnownVectors(t *testing.T) {
	tests := []struct {
		sample int16
		want   byte
	}{
		{sample: 0, want: 0xff},
		{sample: 1, want: 0xff},
		{sample: -1, want: 0x7f},
		{sample: 1000, want: 0xce},
		{sample: -1000, want: 0x4e},
		{sample: 32124, want: 0x80},
		{sample: -32124, want: 0x00},
	}
	for _, test := range tests {
		if got := linearPCMToMulaw(test.sample); got != test.want {
			t.Fatalf("linearPCMToMulaw(%d) = %#02x, want %#02x", test.sample, got, test.want)
		}
	}
}

func TestSpeechResamplerPreservesVoiceBandAndRejectsAliasing(t *testing.T) {
	passband := resampleSine(1000, 12000, openAISpeechSampleRate)
	upperVoiceBand := resampleSine(3400, 12000, openAISpeechSampleRate)
	stopband := resampleSine(7000, 12000, openAISpeechSampleRate)
	if len(passband) != twilioSampleRate || len(upperVoiceBand) != twilioSampleRate || len(stopband) != twilioSampleRate {
		t.Fatalf("resampled lengths = %d/%d/%d, want %d", len(passband), len(upperVoiceBand), len(stopband), twilioSampleRate)
	}
	passbandRMS := sampleRMS(passband[200 : len(passband)-200])
	upperVoiceBandRMS := sampleRMS(upperVoiceBand[200 : len(upperVoiceBand)-200])
	stopbandRMS := sampleRMS(stopband[200 : len(stopband)-200])
	if passbandRMS < 7800 || passbandRMS > 9000 {
		t.Fatalf("1 kHz passband RMS = %.2f, want preserved near %.2f", passbandRMS, 12000/math.Sqrt2)
	}
	if upperVoiceBandRMS < passbandRMS*0.8 {
		t.Fatalf("3.4 kHz upper voice-band RMS = %.2f, want at least 80%% of passband %.2f", upperVoiceBandRMS, passbandRMS)
	}
	if stopbandRMS > passbandRMS*0.02 {
		t.Fatalf("7 kHz aliased RMS = %.2f, want below 2%% of passband %.2f", stopbandRMS, passbandRMS)
	}
}

func TestSpeechResamplerKeepsPhaseAcrossInputBoundaries(t *testing.T) {
	samples := make([]int16, 2401)
	for index := range samples {
		samples[index] = int16((index%301 - 150) * 100)
	}
	onePass := resampleSamples(samples, len(samples))
	chunked := resampleSamples(samples, 17)
	if len(onePass) != 801 || len(chunked) != len(onePass) {
		t.Fatalf("resampled lengths = %d/%d, want 801", len(onePass), len(chunked))
	}
	for index := range onePass {
		if onePass[index] != chunked[index] {
			t.Fatalf("sample %d differs across boundaries: %d != %d", index, onePass[index], chunked[index])
		}
	}
}

func resampleSine(frequency float64, amplitude float64, sampleCount int) []int16 {
	samples := make([]int16, sampleCount)
	for index := range samples {
		value := amplitude * math.Sin(2*math.Pi*frequency*float64(index)/openAISpeechSampleRate)
		samples[index] = int16(math.Round(value))
	}
	return resampleSamples(samples, len(samples))
}

func resampleSamples(samples []int16, chunkSize int) []int16 {
	resampler := newSpeechResampler()
	outputs := make([]int16, 0, (len(samples)+speechResamplerFactor-1)/speechResamplerFactor)
	for start := 0; start < len(samples); start += chunkSize {
		end := start + chunkSize
		if end > len(samples) {
			end = len(samples)
		}
		for _, sample := range samples[start:end] {
			if output, ok := resampler.Push(sample); ok {
				outputs = append(outputs, output)
			}
		}
	}
	return append(outputs, resampler.Flush()...)
}

func sampleRMS(samples []int16) float64 {
	sum := 0.0
	for _, sample := range samples {
		value := float64(sample)
		sum += value * value
	}
	return math.Sqrt(sum / float64(len(samples)))
}
