package pos_square

import "testing"

func TestSquareStateRoundTrip(t *testing.T) {
	state := encodeState("salon-123")
	salonID, err := decodeState(state)
	if err != nil {
		t.Fatalf("decode state failed: %v", err)
	}
	if salonID != "salon-123" {
		t.Fatalf("unexpected salon id: %s", salonID)
	}
}

func TestSquareStateRejectsWrongProvider(t *testing.T) {
	_, err := decodeState("bm90LXNxdWFyZToxMjM")
	if err == nil {
		t.Fatalf("expected invalid state")
	}
}
