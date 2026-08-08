import assert from "node:assert/strict";
import test from "node:test";
import {
  openAIConfigPayload,
  openAIConfigToForm,
  openAIRuntimeState,
  platformIntegrationConfigBasePath,
  squareConfigToForm,
  squareInitialProviderActivationPayload,
  squareSchedulingCapabilityReevaluationPayload,
  twilioConfigPayload,
  twilioConfigToForm,
  twilioVoiceRoutingState
} from "./integration-config-contract";
import type {
  OpenAIIntegrationConfig,
  OpenAIRuntimeVerification,
  SquareIntegrationConfig,
  TwilioIntegrationConfig
} from "../../types/api";

test("Twilio preserves the canonical realtime transport and every salon-scoped callback field", () => {
  const config: TwilioIntegrationConfig = {
    provider: "twilio",
    configured: true,
    voice_route_id: "11111111-1111-4111-8111-111111111111",
    voice_routing_enabled: true,
    voice_inbound_number: "+13125550101",
    voice_routing_configured: true,
    voice_routing_blockers: [],
    account_sid_hint: "AC••••1111",
    public_base_url: "https://salon-a.example.com",
    incoming_path: "/voice/a/incoming",
    turn_path: "/voice/a/turn",
    recording_path: "/voice/a/recording",
    stream_path: "/voice/a/stream",
    voice_transport: "realtime_stream",
    inbound_webhook_url: "https://salon-a.example.com/voice/a/incoming",
    turn_webhook_url: "https://salon-a.example.com/voice/a/turn",
    recording_webhook_url: "https://salon-a.example.com/voice/a/recording",
    stream_webhook_url: "wss://salon-a.example.com/voice/a/stream",
    auth_token_configured: true,
    auth_token_source: "database",
    owner_sms_enabled: true,
    owner_sms_destination_masked: "+1******0101",
    owner_sms_consent_attested: true,
    account_sid_configured: true,
    messaging_service_configured: true,
    sender_configured: false,
    notification_status_path: "/notifications/a/status",
    notification_inbound_path: "/notifications/a/inbound",
    notification_status_url: "https://salon-a.example.com/notifications/a/status",
    notification_inbound_url: "https://salon-a.example.com/notifications/a/inbound"
  };

  const form = twilioConfigToForm(config);
  const payload = twilioConfigPayload(form);

  assert.equal(form.voice_transport, "realtime_stream");
  assert.equal(form.voice_routing_enabled, true);
  assert.equal(payload.voice_inbound_number, "+13125550101");
  assert.equal(payload.voice_routing_enabled, true);
  assert.equal(payload.voice_transport, "realtime_stream");
  assert.equal(payload.incoming_path, "/voice/a/incoming");
  assert.equal(payload.stream_path, "/voice/a/stream");
  assert.equal(payload.notification_status_path, "/notifications/a/status");
  assert.equal(payload.notification_inbound_path, "/notifications/a/inbound");
  assert.equal(payload.auth_token, "");
  assert.equal(payload.clear_auth_token, false);
  assert.equal("owner_sms_destination" in payload, false);
  assert.equal("messaging_service_sid" in payload, false);
  assert.equal("sender_phone" in payload, false);
});

test("OpenAI round-trips speech output, noise handling, and realtime instructions", () => {
  const config: OpenAIIntegrationConfig = {
    provider: "openai",
    enabled: true,
    configured: true,
    runtime_resolvable: true,
    runtime_blockers: [],
    base_url: "https://api.openai.com/v1",
    destination_profile: "openai_public",
    destination_managed: true,
    transcription_model: "gpt-4o-transcribe",
    reply_model: "gpt-4.1-mini",
    speech_model: "gpt-4o-mini-tts",
    speech_voice: "marin",
    speech_output_mode: "buffered_realtime",
    realtime_enabled: true,
    realtime_model: "gpt-realtime-2",
    realtime_voice: "marin",
    realtime_noise_profile: "strong_noise_rejection",
    realtime_instructions: "Use only backend-approved responses.",
    api_key_configured: true,
    api_key_source: "database",
    credential_revision: 3,
    credential_unique: true
  };

  const form = openAIConfigToForm(config);
  const payload = openAIConfigPayload(form);

  assert.equal(payload.speech_output_mode, "buffered_realtime");
  assert.equal(payload.realtime_noise_profile, "strong_noise_rejection");
  assert.equal(payload.realtime_instructions, "Use only backend-approved responses.");
  assert.equal(payload.api_key, "");
  assert.equal(payload.clear_api_key, false);
  assert.equal(payload.base_url, "https://api.openai.com/v1");
});

test("OpenAI runtime UI states keep saved configuration separate from live evidence", () => {
  const config = {
    enabled: true,
    runtime_resolvable: true
  } as OpenAIIntegrationConfig;
  const verification = {
    status: "failed",
    fresh: true,
    capabilities: [
      { capability: "transcription", required: true, status: "verified" },
      { capability: "speech", required: true, status: "failed" }
    ]
  } as OpenAIRuntimeVerification;

  assert.equal(openAIRuntimeState({ ...config, enabled: false }), "disabled");
  assert.equal(openAIRuntimeState({ ...config, runtime_resolvable: false }), "needs_configuration");
  assert.equal(openAIRuntimeState(config, null), "verification_required");
  assert.equal(openAIRuntimeState(config, { ...verification, status: "queued" }), "verifying");
  assert.equal(openAIRuntimeState(config, { ...verification, fresh: false }), "verification_stale");
  assert.equal(openAIRuntimeState(config, verification), "partially_verified");
  assert.equal(openAIRuntimeState(config, { ...verification, capabilities: [] }), "verification_failed");
  assert.equal(openAIRuntimeState(config, { ...verification, status: "succeeded" }), "live_verified");
});

test("Square keeps visible settings while leaving write-only secrets blank", () => {
  const config: SquareIntegrationConfig = {
    provider: "square",
    configured: true,
    environment: "production",
    client_id: "sq-client-a",
    redirect_url: "https://salon-a.example.com/square/callback",
    api_version: "2026-05-20",
    api_base_url: "https://connect.squareup.com",
    client_secret_configured: true,
    client_secret_source: "database",
    webhook_notification_url: "https://salon-a.example.com/square/webhook",
    webhook_configured: true,
    webhook_signature_key_configured: true,
    webhook_signature_key_source: "database"
  };

  const form = squareConfigToForm(config);

  assert.equal(form.environment, "production");
  assert.equal(form.client_id, "sq-client-a");
  assert.equal(form.webhook_notification_url, "https://salon-a.example.com/square/webhook");
  assert.equal(form.client_secret, "");
  assert.equal(form.webhook_signature_key, "");
});

test("Platform configuration path encodes one exact tenant identifier", () => {
  assert.equal(
    platformIntegrationConfigBasePath("salon/a"),
    "/api/v2/platform/tenants/salon%2Fa/integrations"
  );
});

test("Square safety reevaluation sends fences but no caller-selected capabilities", () => {
  assert.deepEqual(squareSchedulingCapabilityReevaluationPayload("square-safe-1", 7, 11), {
    action_key: "square-safe-1",
    expected_connection_capability_version: 7,
    expected_integration_config_version: 11
  });
});

test("Square initial activation sends every tenant evidence fence", () => {
  assert.deepEqual(squareInitialProviderActivationPayload("square-activate-1", 2, 7, 11), {
    action_key: "square-activate-1",
    expected_version: 2,
    expected_connection_capability_version: 7,
    expected_integration_config_version: 11
  });
});

test("Twilio routing state does not call saved configuration live verified", () => {
  const config = {
    voice_routing_configured: true
  } as TwilioIntegrationConfig;
  assert.equal(
    twilioVoiceRoutingState(config, { routing_configured: true, live_verified: false, verification_stale: false, blockers: [] }),
    "routing_configured"
  );
  assert.equal(
    twilioVoiceRoutingState(config, { routing_configured: true, live_verified: true, verification_stale: false, blockers: [] }),
    "live_verified"
  );
  assert.equal(twilioVoiceRoutingState(config, null), "routing_configured");
});
