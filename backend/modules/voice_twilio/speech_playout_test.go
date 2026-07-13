package voice_twilio

import (
	"bytes"
	"testing"
)

func TestStreamingSpeechPlayoutPreservesOrderAndBoundsQueue(t *testing.T) {
	playout := newStreamingSpeechPlayout()
	startupInput := bytes.Repeat([]byte{0x11}, streamingSpeechStartupBytes)
	startup := playout.Add(startupInput)
	if !bytes.Equal(startup, startupInput) || !playout.Started() {
		t.Fatalf("startup = %d bytes, started=%v", len(startup), playout.Started())
	}
	for frame := 0; frame < streamingSpeechQueueFrameLimit; frame++ {
		value := byte(frame % 251)
		playout.Add(bytes.Repeat([]byte{value}, streamingSpeechFrameBytes))
	}
	if !playout.Full() || playout.QueueFrames() != streamingSpeechQueueFrameLimit {
		t.Fatalf("queue frames = %d, full=%v", playout.QueueFrames(), playout.Full())
	}
	if playout.MaxObservedFrames() != streamingSpeechQueueFrameLimit {
		t.Fatalf("max queue frames = %d", playout.MaxObservedFrames())
	}
	for frame := 0; frame < streamingSpeechQueueFrameLimit; frame++ {
		got := playout.Pop()
		want := byte(frame % 251)
		if len(got) != streamingSpeechFrameBytes || got[0] != want || got[len(got)-1] != want {
			t.Fatalf("frame %d was reordered or truncated", frame)
		}
	}
	if !playout.Empty() {
		t.Fatalf("playout should be empty after all frames are popped")
	}
}

func TestStreamingSpeechPlayoutFlushesFinalPartialFrame(t *testing.T) {
	playout := newStreamingSpeechPlayout()
	startup := playout.Add(bytes.Repeat([]byte{0x21}, streamingSpeechStartupBytes))
	if len(startup) != streamingSpeechStartupBytes {
		t.Fatalf("startup bytes = %d", len(startup))
	}
	playout.Add(bytes.Repeat([]byte{0x42}, 73))
	if frame := playout.Pop(); frame != nil {
		t.Fatalf("partial frame emitted before provider completion: %d bytes", len(frame))
	}
	playout.Finish()
	frame := playout.Pop()
	if len(frame) != 73 || !bytes.Equal(frame, bytes.Repeat([]byte{0x42}, 73)) {
		t.Fatalf("final partial frame = %d bytes", len(frame))
	}
}
