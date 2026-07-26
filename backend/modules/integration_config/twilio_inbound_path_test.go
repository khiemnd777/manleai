package integrationconfig

import "testing"

func TestTwilioInboundPathScopesSalonExactlyOnce(t *testing.T) {
	tests := map[string]string{
		"":                                   "/api/notifications/twilio/inbound/salon-123",
		"/api/notifications/twilio/inbound":  "/api/notifications/twilio/inbound/salon-123",
		"/api/notifications/twilio/inbound/": "/api/notifications/twilio/inbound/salon-123",
		"/api/notifications/twilio/inbound/salon-123":          "/api/notifications/twilio/inbound/salon-123",
		"api/notifications/twilio/inbound/salon-123/salon-123": "/api/notifications/twilio/inbound/salon-123",
	}
	for input, want := range tests {
		if got := twilioInboundPath(input, "salon-123"); got != want {
			t.Fatalf("twilioInboundPath(%q)=%q, want %q", input, got, want)
		}
	}
}
