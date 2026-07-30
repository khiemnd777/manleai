package voice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/manleai/ai-receptionist/internal/config"
)

func TestResolveTwilioVoiceRouteRejectsCrossTenantCallForEveryFollowupEndpoint(t *testing.T) {
	const routeID = "11111111-1111-4111-8111-111111111111"
	store := newFakeVoiceStore()
	store.route = &CallRoute{
		SalonID: "salon_2", OwnerUserID: "owner_2", SessionID: "session_2",
		FromPhone: "+13125550101", ToPhone: "+13125550102",
	}
	service := NewService(store, newFakeConversationEngine(), testVoiceConfig(), AIProviders{})
	service.SetConfigResolver(&recordingVoiceConfigResolver{})

	for _, endpoint := range []string{
		TwilioEndpointTurn,
		TwilioEndpointRecording,
		TwilioEndpointStream,
		TwilioEndpointStreamStatus,
		TwilioEndpointStreamFallback,
	} {
		t.Run(endpoint, func(t *testing.T) {
			_, err := service.ResolveTwilioVoiceRoute(context.Background(), routeID, endpoint, TwilioWebhookEnvelope{
				ProviderCallID: "CA11111111111111111111111111111111",
				AccountSID:     "AC11111111111111111111111111111111",
				FromPhone:      "+13125550101",
				ToPhone:        "+13125550102",
			})
			if !errors.Is(err, ErrTwilioWebhookRejected) {
				t.Fatalf("error=%v, want fail-closed cross-tenant rejection", err)
			}
		})
	}
}

func TestVerifiedTwilioRuntimePayloadKeepsStableOpaqueIdempotencyWithoutProviderBody(t *testing.T) {
	proof := &VerifiedTwilioVoiceRoute{resolved: &ResolvedTwilioVoiceRoute{config: config.TwilioVoiceRouteConfig{
		SalonID: "salon_1", TwilioVoiceConfig: config.TwilioVoiceConfig{RouteID: "route_1", AuthToken: "tenant-secret"},
	}}}
	incoming := map[string]string{
		"TwilioIdempotencyToken": "raw-token", "RecordingSid": "RE123",
		"SpeechResult": "private caller words", "AccountSid": "AC123",
	}
	first := verifiedWebhookRuntimePayload(proof, incoming)
	second := verifiedWebhookRuntimePayload(proof, incoming)
	if first["TwilioIdempotencyToken"] == "" || first["TwilioIdempotencyToken"] != second["TwilioIdempotencyToken"] || first["RecordingSid"] != second["RecordingSid"] {
		t.Fatalf("stable idempotency payload=%#v/%#v", first, second)
	}
	serialized := first["TwilioIdempotencyToken"] + first["RecordingSid"]
	if strings.Contains(serialized, "raw-token") || strings.Contains(serialized, "RE123") || first["SpeechResult"] != "" || first["AccountSid"] != "" {
		t.Fatalf("verified payload retained provider or caller body: %#v", first)
	}
}
