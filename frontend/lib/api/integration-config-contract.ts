import type {
  OpenAIIntegrationConfig,
  OpenAIRuntimeVerification,
  SquareIntegrationConfig,
  TwilioIntegrationConfig,
  TwilioVoiceRoutingStatus
} from "../../types/api";

export type IntegrationConfigProvider = "square" | "twilio" | "openai";
export type TwilioVoiceTransport = "recording" | "realtime_stream";
export type OpenAISpeechOutputMode = "streaming_tts" | "buffered_realtime";
export type OpenAIRuntimeState =
  | "disabled"
  | "needs_configuration"
  | "verification_required"
  | "verifying"
  | "verification_stale"
  | "partially_verified"
  | "verification_failed"
  | "live_verified";
export type OpenAIRealtimeNoiseProfile =
  | "automatic"
  | "standard"
  | "strong_noise_rejection"
  | "minimal_processing";

export type SquareConfigForm = {
  environment: string;
  client_id: string;
  client_secret: string;
  clear_client_secret: boolean;
  redirect_url: string;
  api_version: string;
  api_base_url: string;
  webhook_notification_url: string;
  webhook_signature_key: string;
  clear_webhook_signature_key: boolean;
};

export type TwilioConfigForm = {
  public_base_url: string;
  voice_inbound_number: string;
  voice_routing_enabled: boolean;
  auth_token: string;
  clear_auth_token: boolean;
  incoming_path: string;
  turn_path: string;
  recording_path: string;
  stream_path: string;
  voice_transport: TwilioVoiceTransport;
  owner_sms_enabled: boolean;
  owner_sms_destination: string;
  clear_owner_sms_destination: boolean;
  owner_sms_consent_attested: boolean;
  account_sid: string;
  clear_account_sid: boolean;
  messaging_service_sid: string;
  clear_messaging_service_sid: boolean;
  sender_phone: string;
  clear_sender_phone: boolean;
  notification_status_path: string;
  notification_inbound_path: string;
};

export type OpenAIConfigForm = {
  enabled: boolean;
  api_key: string;
  clear_api_key: boolean;
  base_url: string;
  transcription_model: string;
  reply_model: string;
  speech_model: string;
  speech_voice: string;
  speech_output_mode: OpenAISpeechOutputMode;
  realtime_enabled: boolean;
  realtime_model: string;
  realtime_voice: string;
  realtime_noise_profile: OpenAIRealtimeNoiseProfile;
  realtime_instructions: string;
};

export const defaultSquareConfigForm: SquareConfigForm = {
  environment: "sandbox",
  client_id: "",
  client_secret: "",
  clear_client_secret: false,
  redirect_url: "http://localhost:18089/api/integrations/square/callback",
  api_version: "2026-05-20",
  api_base_url: "",
  webhook_notification_url: "",
  webhook_signature_key: "",
  clear_webhook_signature_key: false
};

export const defaultTwilioConfigForm: TwilioConfigForm = {
  public_base_url: "",
  voice_inbound_number: "",
  voice_routing_enabled: false,
  auth_token: "",
  clear_auth_token: false,
  incoming_path: "/api/voice/twilio/incoming",
  turn_path: "/api/voice/twilio/turn",
  recording_path: "/api/voice/twilio/recording",
  stream_path: "/api/voice/twilio/stream",
  voice_transport: "recording",
  owner_sms_enabled: false,
  owner_sms_destination: "",
  clear_owner_sms_destination: false,
  owner_sms_consent_attested: false,
  account_sid: "",
  clear_account_sid: false,
  messaging_service_sid: "",
  clear_messaging_service_sid: false,
  sender_phone: "",
  clear_sender_phone: false,
  notification_status_path: "/api/notifications/twilio/status",
  notification_inbound_path: "/api/notifications/twilio/inbound"
};

export const defaultOpenAIConfigForm: OpenAIConfigForm = {
  enabled: false,
  api_key: "",
  clear_api_key: false,
  base_url: "https://api.openai.com/v1",
  transcription_model: "gpt-4o-mini-transcribe",
  reply_model: "gpt-4.1-mini",
  speech_model: "tts-1",
  speech_voice: "alloy",
  speech_output_mode: "streaming_tts",
  realtime_enabled: false,
  realtime_model: "gpt-realtime-2",
  realtime_voice: "alloy",
  realtime_noise_profile: "automatic",
  realtime_instructions: ""
};

export function squareConfigToForm(config?: SquareIntegrationConfig): SquareConfigForm {
  if (!config) return { ...defaultSquareConfigForm };
  return {
    environment: config.environment || "sandbox",
    client_id: config.client_id || "",
    client_secret: "",
    clear_client_secret: false,
    redirect_url: config.redirect_url || defaultSquareConfigForm.redirect_url,
    api_version: config.api_version || defaultSquareConfigForm.api_version,
    api_base_url: config.api_base_url || "",
    webhook_notification_url: config.webhook_notification_url || "",
    webhook_signature_key: "",
    clear_webhook_signature_key: false
  };
}

export function twilioConfigToForm(config?: TwilioIntegrationConfig): TwilioConfigForm {
  if (!config) return { ...defaultTwilioConfigForm };
  return {
    public_base_url: config.public_base_url || "",
    voice_inbound_number: config.voice_inbound_number || "",
    voice_routing_enabled: config.voice_routing_enabled,
    auth_token: "",
    clear_auth_token: false,
    incoming_path: config.incoming_path || defaultTwilioConfigForm.incoming_path,
    turn_path: config.turn_path || defaultTwilioConfigForm.turn_path,
    recording_path: config.recording_path || defaultTwilioConfigForm.recording_path,
    stream_path: config.stream_path || defaultTwilioConfigForm.stream_path,
    voice_transport: normalizeTwilioVoiceTransport(config.voice_transport),
    owner_sms_enabled: config.owner_sms_enabled,
    owner_sms_destination: "",
    clear_owner_sms_destination: false,
    owner_sms_consent_attested: config.owner_sms_consent_attested,
    account_sid: "",
    clear_account_sid: false,
    messaging_service_sid: "",
    clear_messaging_service_sid: false,
    sender_phone: "",
    clear_sender_phone: false,
    notification_status_path: config.notification_status_path || defaultTwilioConfigForm.notification_status_path,
    notification_inbound_path: config.notification_inbound_path || defaultTwilioConfigForm.notification_inbound_path
  };
}

export function openAIConfigToForm(config?: OpenAIIntegrationConfig): OpenAIConfigForm {
  if (!config) return { ...defaultOpenAIConfigForm };
  return {
    enabled: config.enabled,
    api_key: "",
    clear_api_key: false,
    base_url: config.base_url || defaultOpenAIConfigForm.base_url,
    transcription_model: config.transcription_model || defaultOpenAIConfigForm.transcription_model,
    reply_model: config.reply_model || defaultOpenAIConfigForm.reply_model,
    speech_model: config.speech_model || defaultOpenAIConfigForm.speech_model,
    speech_voice: config.speech_voice || defaultOpenAIConfigForm.speech_voice,
    speech_output_mode: config.speech_output_mode === "buffered_realtime" ? "buffered_realtime" : "streaming_tts",
    realtime_enabled: config.realtime_enabled,
    realtime_model: config.realtime_model || defaultOpenAIConfigForm.realtime_model,
    realtime_voice: config.realtime_voice || defaultOpenAIConfigForm.realtime_voice,
    realtime_noise_profile: normalizeOpenAIRealtimeNoiseProfile(config.realtime_noise_profile),
    realtime_instructions: config.realtime_instructions || ""
  };
}

export function squareConfigPayload(form: SquareConfigForm): Record<string, unknown> {
  return { ...form };
}

export function twilioConfigPayload(form: TwilioConfigForm): Record<string, unknown> {
  const payload: Record<string, unknown> = { ...form };
  if (!form.owner_sms_destination) delete payload.owner_sms_destination;
  if (!form.messaging_service_sid) delete payload.messaging_service_sid;
  if (!form.sender_phone) delete payload.sender_phone;
  return payload;
}

export function openAIConfigPayload(form: OpenAIConfigForm): Record<string, unknown> {
  return { ...form };
}

export function platformIntegrationConfigBasePath(tenantID: string) {
  return `/api/platform/tenants/${encodeURIComponent(tenantID)}/technical/integration-configs`;
}

export function twilioVoiceRoutingState(
  config?: TwilioIntegrationConfig,
  status?: TwilioVoiceRoutingStatus | null
): "needs_setup" | "routing_configured" | "live_verified" {
  if (!config?.voice_routing_configured || (status && !status.routing_configured)) return "needs_setup";
  return status?.live_verified ? "live_verified" : "routing_configured";
}

export function openAIRuntimeState(
  config?: OpenAIIntegrationConfig,
  verification?: OpenAIRuntimeVerification | null
): OpenAIRuntimeState {
  if (!config?.enabled) return "disabled";
  if (!config.runtime_resolvable) return "needs_configuration";
  if (!verification) return "verification_required";
  if (!verification.fresh || verification.status === "stale") return "verification_stale";
  if (verification.status === "queued" || verification.status === "claimed") return "verifying";
  if (verification.status === "succeeded") return "live_verified";
  if (verification.status === "failed") {
    return verification.capabilities.some((capability) => capability.required && capability.status === "verified")
      ? "partially_verified"
      : "verification_failed";
  }
  return "verification_required";
}

export function normalizeTwilioVoiceTransport(value: string): TwilioVoiceTransport {
  return value === "realtime_stream" ? "realtime_stream" : "recording";
}

export function normalizeOpenAIRealtimeNoiseProfile(value: string): OpenAIRealtimeNoiseProfile {
  switch (value) {
    case "standard":
    case "strong_noise_rejection":
    case "minimal_processing":
      return value;
    default:
      return "automatic";
  }
}
