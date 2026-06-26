export type User = {
  id: string;
  email: string;
  full_name: string;
  phone?: string;
  status: string;
};

export type Salon = {
  id: string;
  name: string;
  phone: string;
  address?: string;
  city?: string;
  state?: string;
  zip_code?: string;
  timezone: string;
  primary_language: string;
  secondary_language: string;
  handoff_phone?: string;
  ai_enabled: boolean;
  active_pos_provider: string;
  public_slug?: string;
  public_catalog_enabled: boolean;
  created_at?: string;
  updated_at?: string;
};

export type PublicCatalogSettings = {
  salon_id: string;
  public_slug?: string;
  public_catalog_enabled: boolean;
  public_path?: string;
  bookable_service_count: number;
  bookable_staff_count: number;
  can_publish: boolean;
  blocked_reason?: string;
  updated_at: string;
};

export type SalonSettings = {
  id: string;
  salon_id: string;
  ai_greeting: string;
  ai_voice: string;
  booking_mode: string;
  recording_enabled: boolean;
  recording_consent_message: string;
  sms_confirmation_enabled: boolean;
  sms_reminder_enabled: boolean;
  reminder_hours_before: number;
  handoff_enabled: boolean;
  created_at: string;
  updated_at: string;
};

export type BusinessHour = {
  id?: string;
  salon_id?: string;
  day_of_week: number;
  open_time?: string;
  close_time?: string;
  is_closed: boolean;
  created_at?: string;
  updated_at?: string;
};

export type BusinessHourPeriod = {
  id?: string;
  salon_id?: string;
  day_of_week: number;
  start_local_time: string;
  end_local_time: string;
  source: string;
  provider?: string;
  provider_location_id?: string;
  provider_period_index: number;
  last_synced_at?: string;
  created_at?: string;
  updated_at?: string;
};

export type SquareIntegrationConfig = {
  provider: "square" | string;
  configured: boolean;
  environment: string;
  client_id: string;
  redirect_url: string;
  api_version: string;
  api_base_url?: string;
  client_secret_configured: boolean;
  client_secret_source: string;
  updated_at?: string;
};

export type TwilioIntegrationConfig = {
  provider: "twilio" | string;
  configured: boolean;
  public_base_url: string;
  incoming_path: string;
  turn_path: string;
  recording_path: string;
  stream_path: string;
  voice_transport: string;
  inbound_webhook_url: string;
  turn_webhook_url: string;
  recording_webhook_url: string;
  stream_webhook_url: string;
  auth_token_configured: boolean;
  auth_token_source: string;
  updated_at?: string;
};

export type OpenAIIntegrationConfig = {
  provider: "openai" | string;
  enabled: boolean;
  configured: boolean;
  base_url: string;
  transcription_model: string;
  reply_model: string;
  speech_model: string;
  speech_voice: string;
  realtime_enabled: boolean;
  realtime_model: string;
  realtime_voice: string;
  realtime_instructions: string;
  api_key_configured: boolean;
  api_key_source: string;
  updated_at?: string;
};

export type IntegrationConfigs = {
  square: SquareIntegrationConfig;
  twilio: TwilioIntegrationConfig;
  openai: OpenAIIntegrationConfig;
};

export type ConfigurationBundle = {
  schema_version: string;
  exported_at: string;
  secrets_exported: boolean;
  operational_data_exported: boolean;
  excluded_data: string[];
  requires_secret_reentry: string[];
  salon_profile: ConfigurationSalonProfile;
  ai_receptionist: ConfigurationAIReceptionist;
  public_booking_page: ConfigurationPublicBookingPage;
  integrations: IntegrationConfigs;
  pos_connection: ConfigurationPOSConnection;
  knowledge_base: ConfigurationKnowledgeBase;
};

export type ConfigurationExport = ConfigurationBundle;

export type ConfigurationSalonProfile = {
  name: string;
  phone: string;
  address?: string;
  city?: string;
  state?: string;
  zip_code?: string;
  timezone: string;
  primary_language: string;
  secondary_language: string;
  handoff_phone?: string;
  ai_enabled: boolean;
  active_pos_provider: string;
  updated_at: string;
};

export type ConfigurationAIReceptionist = {
  ai_greeting: string;
  ai_voice: string;
  booking_mode: string;
  recording_enabled: boolean;
  recording_consent_message: string;
  sms_confirmation_enabled: boolean;
  sms_reminder_enabled: boolean;
  reminder_hours_before: number;
  handoff_enabled: boolean;
  updated_at: string;
};

export type ConfigurationPublicBookingPage = {
  public_slug?: string;
  public_catalog_enabled: boolean;
  public_path?: string;
  updated_at: string;
};

export type ConfigurationPOSConnection = {
  provider: string;
  status: string;
  merchant_id?: string;
  location_id?: string;
  scopes: string[];
  last_sync_at?: string;
  updated_at?: string;
};

export type ConfigurationKnowledgeBase = {
  items: ConfigurationKnowledgeItem[];
  count: number;
};

export type ConfigurationKnowledgeItem = {
  source_key: string;
  title: string;
  category: string;
  body: string;
  status: string;
  source: string;
  created_at: string;
  updated_at: string;
};

export type ConfigurationImportRequest = {
  request_id?: string;
  configuration: ConfigurationBundle;
};

export type ConfigurationImportResponse = {
  import_run_id?: string;
  salon_id?: string;
  request_id: string;
  dry_run: boolean;
  status: string;
  schema_version: string;
  can_apply: boolean;
  summary: ConfigurationImportSectionSummary[];
  warnings: ConfigurationImportIssue[];
  conflicts: ConfigurationImportIssue[];
  excluded_data: string[];
  requires_secret_reentry: string[];
};

export type ConfigurationImportSectionSummary = {
  section: string;
  created: number;
  updated: number;
  unchanged: number;
  skipped: number;
  conflicts: number;
};

export type ConfigurationImportIssue = {
  section: string;
  code: string;
  message: string;
  field?: string;
  source_key?: string;
};

export type POSConnection = {
  id?: string;
  salon_id: string;
  provider: string;
  status: string;
  merchant_id?: string;
  location_id?: string;
  scopes: string[];
  last_sync_at?: string;
  error_message?: string;
};

export type POSService = {
  id?: string;
  salon_id?: string;
  pos_provider: string;
  pos_service_id: string;
  pos_service_version?: number;
  name: string;
  description?: string;
  ai_description?: string;
  duration_minutes: number;
  price_from?: number;
  price_display?: string;
  ai_bookable: boolean;
  active: boolean;
  sync_status: string;
  archived_at?: string;
  last_synced_at?: string;
  sync_error?: string;
  source: string;
  pos_linked: boolean;
};

export type POSStaffMember = {
  id?: string;
  salon_id?: string;
  pos_provider: string;
  pos_staff_id: string;
  name: string;
  phone?: string;
  email?: string;
  ai_bookable: boolean;
  active: boolean;
  sync_status: string;
  archived_at?: string;
  last_synced_at?: string;
  sync_error?: string;
  source: string;
  pos_linked: boolean;
};

export type POSCustomer = {
  id?: string;
  pos_customer_id: string;
  name: string;
  phone: string;
  email?: string;
};

export type CustomerRecord = {
  id?: string;
  salon_id?: string;
  key: string;
  name?: string;
  phone?: string;
  email?: string;
  notes?: string;
  active: boolean;
  sync_status: string;
  archived_at?: string;
  last_synced_at?: string;
  sync_error?: string;
  source: string;
  pos_linked: boolean;
  last_activity_at: string;
  last_activity_source: string;
  last_outcome: string;
  confirmed_appointments: number;
  pending_requests: number;
  call_count: number;
  handoff_count: number;
  appointment_ids?: string[];
  booking_attempt_ids?: string[];
  call_session_ids?: string[];
  latest_appointment_at?: string;
  latest_request_at?: string;
};

export type CustomerSummary = {
  total_known_customers: number;
  confirmed_appointments: number;
  pending_requests: number;
  customers_with_calls: number;
  last_customer_activity_at?: string;
};

export type StaffSelectionMode = "specific" | "anyone" | string;

export type BookingSegmentSnapshot = {
  service_id?: string;
  service_name: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  duration_minutes?: number;
  sort_order: number;
};

export type BookingSegmentRequest = {
  service_id: string;
  staff_id?: string;
  staff_selection_mode: StaffSelectionMode;
};

export type AppointmentRecord = {
  id: string;
  salon_id: string;
  booking_attempt_id: string;
  pos_provider: string;
  pos_appointment_id: string;
  pos_appointment_version?: number;
  status: string;
  customer_name: string;
  customer_phone: string;
  customer_email?: string;
  service_id?: string;
  staff_id?: string;
  staff_selection_mode: StaffSelectionMode;
  segments?: BookingSegmentSnapshot[];
  start_time: string;
  end_time: string;
  notes?: string;
  created_at: string;
  updated_at: string;
};

export type BookingAttempt = {
  id: string;
  salon_id: string;
  source: string;
  status: string;
  pos_provider: string;
  pos_booking_id?: string;
  customer_name: string;
  customer_phone: string;
  customer_email?: string;
  service_id?: string;
  staff_id?: string;
  staff_selection_mode: StaffSelectionMode;
  segments?: BookingSegmentSnapshot[];
  requested_start_time: string;
  requested_end_time: string;
  notes?: string;
  error_code?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
  appointment?: AppointmentRecord;
};

export type AvailabilitySlot = {
  start_time: string;
  end_time: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  segments?: AvailabilitySegment[];
};

export type AvailabilitySegment = {
  service_id: string;
  service_name: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  duration_minutes: number;
};

export type AvailabilityResult = {
  service_id: string;
  service_name: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  segments?: AvailabilitySegment[];
  preferred_date: string;
  duration_minutes: number;
  timezone: string;
  slots: AvailabilitySlot[];
};

export type POSLocation = {
  id: string;
  name: string;
  timezone?: string;
  address?: string;
  status?: string;
};

export type SyncLog = {
  id: string;
  salon_id: string;
  provider: string;
  sync_type: string;
  status: string;
  message?: string;
  started_at: string;
  completed_at?: string;
};

export type ReadinessCheck = {
  key: string;
  label: string;
  complete: boolean;
  message?: string;
};

export type TestBookingRecord = {
  booking_attempt_id?: string;
  appointment_id?: string;
  status: string;
  appointment_status?: string;
  pos_booking_id?: string;
  pos_appointment_version?: number;
  customer_name?: string;
  customer_phone?: string;
  service_id?: string;
  staff_id?: string;
  start_time?: string;
  end_time?: string;
  error_code?: string;
  error_message?: string;
  created_at?: string;
  updated_at?: string;
};

export type SquareReadiness = {
  ai_enabled: boolean;
  can_test_booking: boolean;
  can_cancel_test_booking: boolean;
  can_enable_ai_booking: boolean;
  service_count: number;
  staff_count: number;
  business_hour_period_count: number;
  latest_test_booking?: TestBookingRecord;
  checks: ReadinessCheck[];
};

export type ProviderCapabilities = {
  service_upsert: boolean;
  service_archive: boolean;
  staff_upsert: boolean;
  staff_archive: boolean;
  customer_upsert: boolean;
};

export type ProviderOption = {
  provider: string;
  label: string;
  installed: boolean;
  active: boolean;
  status: string;
  blocked_reason?: string;
  capabilities: ProviderCapabilities;
};

export type ProviderMappingSummary = {
  service_count: number;
  staff_count: number;
  customer_count: number;
  bookable_service_count: number;
  bookable_staff_count: number;
  linked_service_count: number;
  linked_staff_count: number;
  linked_customer_count: number;
  unmapped_service_count: number;
  unmapped_staff_count: number;
  sync_failed_count: number;
};

export type ProviderSwitchReadiness = {
  salon_id: string;
  active_provider: string;
  active_provider_label: string;
  installed_providers: ProviderOption[];
  unavailable_providers: ProviderOption[];
  mapping: ProviderMappingSummary;
  checks: ReadinessCheck[];
  dry_run_booking_ready: boolean;
  can_start_switch: boolean;
  can_activate_provider: boolean;
  blocked_reason?: string;
};

export type ProviderSwitchMatchSummary = {
  total: number;
  suggested: number;
  unmatched: number;
  conflicts: number;
  confirmed: number;
  skipped: number;
};

export type ProviderSwitchMatch = {
  id: string;
  run_id: string;
  salon_id: string;
  entity_type: string;
  canonical_entity_id?: string;
  canonical_name?: string;
  provider_entity_id: string;
  provider_name: string;
  provider_phone?: string;
  provider_email?: string;
  provider_duration_minutes?: number;
  match_status: string;
  match_confidence: number;
  match_reason?: string;
  created_at: string;
  updated_at: string;
};

export type ProviderSwitchRun = {
  id: string;
  salon_id: string;
  from_provider: string;
  to_provider: string;
  status: string;
  blocked_reason?: string;
  dry_run_ready: boolean;
  can_activate: boolean;
  activated_at?: string;
  cancelled_at?: string;
  created_by_user_id?: string;
  created_at: string;
  updated_at: string;
  match_summary: ProviderSwitchMatchSummary;
  matches?: ProviderSwitchMatch[];
};

export type ProviderSwitchDryRunReadiness = {
  run_id: string;
  salon_id: string;
  from_provider: string;
  to_provider: string;
  status: string;
  checks: ReadinessCheck[];
  can_run_dry_run: boolean;
  dry_run_ready: boolean;
  can_activate: boolean;
  blocked_reason?: string;
};

export type TranscriptMessage = {
  id: string;
  session_id: string;
  salon_id: string;
  speaker: "ai" | "customer" | "tool";
  body: string;
  sequence: number;
  created_at: string;
};

export type HandoffRequest = {
  id: string;
  salon_id: string;
  call_session_id: string;
  status: string;
  reason: string;
  customer_name?: string;
  customer_phone?: string;
  summary: string;
  created_at: string;
  resolved_at?: string;
};

export type OfferedSlot = {
  start_time: string;
  end_time: string;
  staff_id: string;
  staff_name: string;
  staff_selection_mode?: StaffSelectionMode;
  segments?: OfferedSlotSegment[];
};

export type OfferedSlotSegment = {
  service_id: string;
  service_name?: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  duration_minutes?: number;
};

export type ConversationSession = {
  id: string;
  salon_id: string;
  channel: string;
  provider?: string;
  provider_call_id?: string;
  inbound_phone?: string;
  outbound_phone?: string;
  status: string;
  intent: string;
  outcome: string;
  customer_name?: string;
  customer_phone?: string;
  customer_email?: string;
  service_id?: string;
  service_name?: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode?: StaffSelectionMode;
  requested_start_time?: string;
  offered_slots?: OfferedSlot[];
  booking_segments?: BookingSegmentRequest[];
  booking_attempt_id?: string;
  appointment_id?: string;
  summary?: string;
  started_at: string;
  ended_at?: string;
  created_at: string;
  updated_at: string;
  transcript?: TranscriptMessage[];
  handoff?: HandoffRequest;
};

export type VoiceStatus = {
  provider: string;
  configured: boolean;
  signature_verification: boolean;
  inbound_webhook_url: string;
  turn_webhook_url: string;
  recording_webhook_url: string;
  stream_webhook_url: string;
  salon_phone?: string;
  ready: boolean;
  phone_booking_ready: boolean;
  blocked_reason?: string;
  input_mode: "gather" | "recording" | "realtime_stream" | string;
  ai: VoiceAIStatus;
  booking: VoiceBookingReadiness;
};

export type VoiceBookingReadiness = {
  ready: boolean;
  ai_enabled: boolean;
  active_provider: string;
  provider_connected: boolean;
  provider_synced: boolean;
  square_connected: boolean;
  square_synced: boolean;
  test_booking_cancelled: boolean;
  service_count: number;
  staff_count: number;
  business_hours_count: number;
  checks: ReadinessCheck[];
  blocked_reason?: string;
};

export type VoiceCapabilityStatus = {
  provider: string;
  configured: boolean;
  ready: boolean;
  model?: string;
  voice?: string;
  blocked_reason?: string;
};

export type VoiceAIStatus = {
  provider: string;
  configured: boolean;
  ready: boolean;
  stt: VoiceCapabilityStatus;
  llm: VoiceCapabilityStatus;
  tts: VoiceCapabilityStatus;
  realtime: VoiceCapabilityStatus;
};

export type KnowledgeItem = {
  id: string;
  salon_id: string;
  title: string;
  category: string;
  body: string;
  status: string;
  source: string;
  created_at: string;
  updated_at: string;
};

export type OwnerCorrection = {
  id: string;
  salon_id: string;
  call_session_id?: string;
  transcript_message_id?: string;
  correction: string;
  status: string;
  applied_knowledge_item_id?: string;
  created_at: string;
  updated_at: string;
};
