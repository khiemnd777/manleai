package voice_twilio

import "time"

const (
	streamingSpeechInputSampleRate  = 24000
	streamingSpeechSampleRate       = 8000
	streamingSpeechFrameBytes       = 160
	streamingSpeechFrameDuration    = 20 * time.Millisecond
	streamingSpeechQueueFrameLimit  = 250 // Five seconds; producer backpressure applies at this boundary.
	streamingSpeechEventBufferLimit = 64
)

type streamingSpeechTicker interface {
	C() <-chan time.Time
	Stop()
}

type realtimeSpeechTicker struct {
	ticker *time.Ticker
}

func (t *realtimeSpeechTicker) C() <-chan time.Time { return t.ticker.C }
func (t *realtimeSpeechTicker) Stop()               { t.ticker.Stop() }

func newRealtimeSpeechTicker(interval time.Duration) streamingSpeechTicker {
	return &realtimeSpeechTicker{ticker: time.NewTicker(interval)}
}

type streamingSpeechPlayout struct {
	startup           []byte
	pending           []byte
	frames            [][]byte
	head              int
	started           bool
	maxObservedFrames int
}

func newStreamingSpeechPlayout() *streamingSpeechPlayout {
	return &streamingSpeechPlayout{
		startup: make([]byte, 0, streamingSpeechStartupBytes),
		pending: make([]byte, 0, streamingSpeechFrameBytes),
		frames:  make([][]byte, 0, streamingSpeechQueueFrameLimit),
	}
}

func (p *streamingSpeechPlayout) Add(audio []byte) (startup []byte) {
	if len(audio) == 0 {
		return nil
	}
	if !p.started {
		needed := streamingSpeechStartupBytes - len(p.startup)
		if len(audio) <= needed {
			p.startup = append(p.startup, audio...)
			if len(p.startup) < streamingSpeechStartupBytes {
				return nil
			}
			p.started = true
			startup = append([]byte(nil), p.startup...)
			p.startup = p.startup[:0]
			return startup
		}
		p.startup = append(p.startup, audio[:needed]...)
		p.started = true
		startup = append([]byte(nil), p.startup...)
		p.startup = p.startup[:0]
		audio = audio[needed:]
	}
	p.appendFrames(audio)
	return startup
}

func (p *streamingSpeechPlayout) Finish() (startup []byte) {
	if !p.started {
		if len(p.startup) == 0 {
			return nil
		}
		p.started = true
		startup = append([]byte(nil), p.startup...)
		p.startup = p.startup[:0]
	}
	if len(p.pending) > 0 {
		p.frames = append(p.frames, append([]byte(nil), p.pending...))
		p.pending = p.pending[:0]
		p.observeDepth()
	}
	return startup
}

func (p *streamingSpeechPlayout) appendFrames(audio []byte) {
	p.pending = append(p.pending, audio...)
	for len(p.pending) >= streamingSpeechFrameBytes {
		p.frames = append(p.frames, append([]byte(nil), p.pending[:streamingSpeechFrameBytes]...))
		p.pending = p.pending[streamingSpeechFrameBytes:]
	}
	if len(p.pending) == 0 {
		p.pending = p.pending[:0]
	}
	p.observeDepth()
}

func (p *streamingSpeechPlayout) Pop() []byte {
	if p.head >= len(p.frames) {
		return nil
	}
	frame := p.frames[p.head]
	p.frames[p.head] = nil
	p.head++
	if p.head == len(p.frames) {
		p.frames = p.frames[:0]
		p.head = 0
	} else if p.head >= 64 && p.head*2 >= len(p.frames) {
		p.frames = append(p.frames[:0], p.frames[p.head:]...)
		p.head = 0
	}
	return frame
}

func (p *streamingSpeechPlayout) QueueFrames() int {
	return len(p.frames) - p.head
}

func (p *streamingSpeechPlayout) Full() bool {
	return p.QueueFrames() >= streamingSpeechQueueFrameLimit
}

func (p *streamingSpeechPlayout) Empty() bool {
	return p.QueueFrames() == 0 && len(p.pending) == 0
}

func (p *streamingSpeechPlayout) Started() bool {
	return p.started
}

func (p *streamingSpeechPlayout) MaxObservedFrames() int {
	return p.maxObservedFrames
}

func (p *streamingSpeechPlayout) Clear() {
	p.startup = p.startup[:0]
	p.pending = p.pending[:0]
	for index := p.head; index < len(p.frames); index++ {
		p.frames[index] = nil
	}
	p.frames = p.frames[:0]
	p.head = 0
}

func (p *streamingSpeechPlayout) observeDepth() {
	if depth := p.QueueFrames(); depth > p.maxObservedFrames {
		p.maxObservedFrames = depth
	}
}
