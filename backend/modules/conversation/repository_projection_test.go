package conversation

import "testing"

func TestDecodeTranscriptMetadataNormalizesUnsupportedLegacyShapes(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantWarning bool
		wantValue   string
	}{
		{name: "object", raw: `{"event_key":"turn-1"}`, wantValue: "turn-1"},
		{name: "null", raw: `null`},
		{name: "array", raw: `[{"event_key":"turn-1"}]`, wantWarning: true},
		{name: "string", raw: `"legacy"`, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metadata, warning, err := decodeTranscriptMetadata([]byte(test.raw))
			if err != nil {
				t.Fatalf("decode metadata: %v", err)
			}
			if warning != test.wantWarning {
				t.Fatalf("warning = %v, want %v", warning, test.wantWarning)
			}
			if got, _ := metadata["event_key"].(string); got != test.wantValue {
				t.Fatalf("event_key = %q, want %q", got, test.wantValue)
			}
		})
	}
}

func TestDecodePartyGuestServiceRequestsNormalizesUnsupportedLegacyShapes(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantWarning bool
		wantCount   int
	}{
		{name: "valid array", raw: `[{"guest_reference":"Guest 2","service_id":"service-2"}]`, wantCount: 1},
		{name: "null", raw: `null`},
		{name: "object", raw: `{"guest_reference":"Guest 2"}`, wantWarning: true},
		{name: "wrong field type", raw: `[{"quantity":"two"}]`, wantWarning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests, warning, err := decodePartyGuestServiceRequests([]byte(test.raw))
			if err != nil {
				t.Fatalf("decode guest requests: %v", err)
			}
			if warning != test.wantWarning {
				t.Fatalf("warning = %v, want %v", warning, test.wantWarning)
			}
			if len(requests) != test.wantCount {
				t.Fatalf("request count = %d, want %d", len(requests), test.wantCount)
			}
		})
	}
}

func TestProjectionDecodersRejectMalformedJSON(t *testing.T) {
	if _, _, err := decodeTranscriptMetadata([]byte(`{"event_key":`)); err == nil {
		t.Fatal("malformed transcript metadata returned no error")
	}
	if _, _, err := decodePartyGuestServiceRequests([]byte(`[{"quantity":`)); err == nil {
		t.Fatal("malformed party guest requests returned no error")
	}
}
