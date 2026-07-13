package voice_openai

import "math"

const (
	speechResamplerFactor = openAISpeechSampleRate / twilioSampleRate
	speechFIRTapCount     = 255
	speechFIRCutoffHz     = 3700.0
)

var speechLowPassFIR = designSpeechLowPassFIR()

// speechResampler is a fixed 24 kHz to 8 kHz low-pass FIR resampler. It keeps
// filter history and decimation phase across arbitrary HTTP response chunks.
type speechResampler struct {
	coefficients []float64
	history      []float64
	position     int
	inputIndex   int64
	realSamples  int64
	flushed      bool
}

func newSpeechResampler() *speechResampler {
	return &speechResampler{
		coefficients: speechLowPassFIR,
		history:      make([]float64, len(speechLowPassFIR)),
		position:     -1,
	}
}

func (r *speechResampler) Push(sample int16) (int16, bool) {
	if r.flushed {
		return 0, false
	}
	r.realSamples++
	return r.push(float64(sample))
}

func (r *speechResampler) Flush() []int16 {
	if r.flushed {
		return nil
	}
	r.flushed = true
	groupDelay := int64(len(r.coefficients) / 2)
	outputs := make([]int16, 0, int(groupDelay/int64(speechResamplerFactor))+1)
	for padding := int64(0); padding < groupDelay; padding++ {
		if sample, ok := r.push(0); ok {
			originalIndex := r.inputIndex - 1 - groupDelay
			if originalIndex < r.realSamples {
				outputs = append(outputs, sample)
			}
		}
	}
	return outputs
}

func (r *speechResampler) push(sample float64) (int16, bool) {
	r.position++
	if r.position == len(r.history) {
		r.position = 0
	}
	r.history[r.position] = sample
	currentIndex := r.inputIndex
	r.inputIndex++
	groupDelay := int64(len(r.coefficients) / 2)
	if currentIndex < groupDelay || (currentIndex-groupDelay)%int64(speechResamplerFactor) != 0 {
		return 0, false
	}

	value := 0.0
	historyIndex := r.position
	for coefficientIndex, coefficient := range r.coefficients {
		if coefficientIndex > 0 {
			historyIndex--
			if historyIndex < 0 {
				historyIndex = len(r.history) - 1
			}
		}
		value += coefficient * r.history[historyIndex]
	}
	if value > math.MaxInt16 {
		value = math.MaxInt16
	} else if value < math.MinInt16 {
		value = math.MinInt16
	}
	return int16(math.Round(value)), true
}

func designSpeechLowPassFIR() []float64 {
	taps := make([]float64, speechFIRTapCount)
	middle := float64(speechFIRTapCount-1) / 2
	cutoff := speechFIRCutoffHz / float64(openAISpeechSampleRate)
	sum := 0.0
	for index := range taps {
		offset := float64(index) - middle
		ideal := 2 * cutoff
		if offset != 0 {
			ideal = math.Sin(2*math.Pi*cutoff*offset) / (math.Pi * offset)
		}
		window := 0.42 - 0.5*math.Cos(2*math.Pi*float64(index)/float64(speechFIRTapCount-1)) + 0.08*math.Cos(4*math.Pi*float64(index)/float64(speechFIRTapCount-1))
		taps[index] = ideal * window
		sum += taps[index]
	}
	for index := range taps {
		taps[index] /= sum
	}
	return taps
}
