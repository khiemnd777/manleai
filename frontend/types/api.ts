export type User = {
  id: string;
  email: string;
  full_name: string;
  phone?: string;
  status: string;
  principal_scope: "tenant" | "platform";
};

export type SchedulingAuthority = "owner_manual" | "manleai_calendar" | "external_provider";
export type BookingMode = "confirmed_booking" | "pending_approval" | "disabled";

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
  /** External-provider adapter intent only. Never use this field as the scheduling-authority discriminator. */
  active_pos_provider: string;
  public_slug?: string;
  public_catalog_enabled: boolean;
  scheduling_authority?: SchedulingAuthority;
  scheduling_authority_version?: number;
  created_at?: string;
  updated_at?: string;
};

export type PublicCatalogSettings = {
  salon_id: string;
  public_slug?: string;
  public_catalog_enabled: boolean;
  public_path?: string;
  scheduling_authority: SchedulingAuthority;
  scheduling_authority_version: number;
  eligible_service_count: number;
  eligible_staff_count: number;
  published_hours_count: number;
  bookable_service_count: number;
  bookable_staff_count: number;
  can_publish: boolean;
  readiness_label: string;
  readiness_blockers: Array<{
    code: string;
    scope: string;
    message: string;
  }>;
  blocked_reason?: string;
  updated_at: string;
};

export type SalonSettings = {
  id: string;
  salon_id: string;
  ai_greeting: string;
  ai_voice: string;
  ai_tone: string;
  booking_mode: BookingMode;
  recording_enabled: boolean;
  recording_consent_message: string;
  sms_confirmation_enabled: boolean;
  sms_reminder_enabled: boolean;
  reminder_hours_before: number;
  handoff_enabled: boolean;
  consultation_enabled: boolean;
  scheduling_authority: SchedulingAuthority;
  scheduling_authority_version: number;
  created_at: string;
  updated_at: string;
};

export type SchedulingAuthorityReadinessCheck = {
  code: string;
  ready: boolean;
  scope?: string;
  entity_id?: string;
};

export type SchedulingAuthorityReadinessBlocker = {
  code: string;
  scope?: string;
  entity_id?: string;
  message: string;
};

export type SchedulingAuthorityReadiness = {
  target_scheduling_authority: SchedulingAuthority;
  ready: boolean;
  authority_version: number;
  config_version?: number;
  eligible_service_count?: number;
  service_count?: number;
  staff_count?: number;
  business_hour_period_count?: number;
  checks: SchedulingAuthorityReadinessCheck[];
};

export type SchedulingAuthoritySwitchStatus = "preview_ready" | "preview_blocked" | "committed" | "failed";

export type SchedulingAuthoritySwitchRun = {
  id: string;
  salon_id: string;
  source_scheduling_authority: SchedulingAuthority;
  target_scheduling_authority: SchedulingAuthority;
  expected_source_authority_version: number;
  operation_key: string;
  rollback_of_switch_run_id?: string;
  readiness_snapshot: SchedulingAuthorityReadiness;
  blockers: SchedulingAuthorityReadinessBlocker[];
  status: SchedulingAuthoritySwitchStatus;
  previewed_at: string;
  blocked_at?: string;
  committed_at?: string;
  created_at: string;
  updated_at: string;
};

export type SchedulingAuthoritySwitchResponse = {
  scheduling_authority_switch: SchedulingAuthoritySwitchRun;
  replayed: boolean;
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

export type ManleAICalendarIntegerConstraint = {
  minimum: number;
  maximum: number;
};

export type ManleAICalendarSlotStepConstraint = ManleAICalendarIntegerConstraint & {
  must_divide_minutes: number;
};

export type ManleAICalendarConstraints = {
  slot_step_minutes: ManleAICalendarSlotStepConstraint;
  minimum_booking_notice_minutes: ManleAICalendarIntegerConstraint;
  booking_horizon_days: ManleAICalendarIntegerConstraint;
  cutoff_minutes: ManleAICalendarIntegerConstraint;
  max_party_size: ManleAICalendarIntegerConstraint;
  buffer_minutes: ManleAICalendarIntegerConstraint;
  period_minutes: ManleAICalendarIntegerConstraint;
  resource_capacity: ManleAICalendarIntegerConstraint;
  resource_units_required: ManleAICalendarIntegerConstraint;
  exception_capacity_override: ManleAICalendarIntegerConstraint;
  action_key_max_bytes: number;
  exception_reason_max_bytes: number;
  resource_name_max_characters: number;
  capacity_modes: ManleAICalendarCapacityMode[];
  exception_scope_types: ManleAICalendarExceptionScope[];
  exception_effects: ManleAICalendarExceptionEffect[];
  execution_engine_available: boolean;
};

export type ManleAICalendarCapacityMode = "staff_only" | "pooled";

export type ManleAICalendarExceptionScope = "salon" | "staff" | "resource";

export type ManleAICalendarExceptionEffect = "available" | "unavailable" | "capacity_override";

export type ManleAICalendarConfig = {
  salon_id: string;
  version: number;
  slot_step_minutes: number;
  minimum_booking_notice_minutes: number;
  booking_horizon_days: number;
  reschedule_cutoff_minutes: number | null;
  cancellation_cutoff_minutes: number | null;
  max_party_size: number;
  default_buffer_before_minutes: number;
  default_buffer_after_minutes: number;
  activated_at: string | null;
  activated_by_user_id?: string;
  activated_version: number | null;
  created_at: string;
  updated_at: string;
};

export type ManleAICalendarHourPeriod = {
  id: string;
  day_of_week: number;
  start_minute: number;
  end_minute: number;
  created_at: string;
  updated_at: string;
};

export type ManleAICalendarHourPeriodInput = {
  day_of_week: number;
  start_minute: number;
  end_minute: number;
};

export type ManleAICalendarStaffRef = {
  id: string;
  name: string;
  active: boolean;
  ai_bookable: boolean;
  archived_at: string | null;
};

export type ManleAICalendarServiceRef = {
  id: string;
  name: string;
  duration_minutes: number;
  active: boolean;
  ai_bookable: boolean;
  archived_at: string | null;
};

export type ManleAICalendarWeeklyPeriod = {
  id: string;
  staff_id: string;
  day_of_week: number;
  start_minute: number;
  end_minute: number;
  created_at: string;
  updated_at: string;
};

export type ManleAICalendarWeeklyPeriodInput = {
  day_of_week: number;
  start_minute: number;
  end_minute: number;
};

export type ManleAICalendarStaffProfile = {
  staff: ManleAICalendarStaffRef;
  weekly_periods: ManleAICalendarWeeklyPeriod[];
  eligible_services: ManleAICalendarServiceRef[];
};

export type ManleAICalendarResourceRequirement = {
  resource_pool_id: string;
  resource_name: string;
  units_required: number;
  pool_capacity: number;
  pool_archived_at: string | null;
  created_at: string;
  updated_at: string;
};

export type ManleAICalendarResourceRequirementInput = {
  resource_pool_id: string;
  units_required: number;
};

export type ManleAICalendarServicePolicy = {
  service: ManleAICalendarServiceRef;
  configured: boolean;
  enabled: boolean;
  capacity_mode: ManleAICalendarCapacityMode | null;
  buffer_before_minutes_override: number | null;
  buffer_after_minutes_override: number | null;
  eligible_staff: ManleAICalendarStaffRef[];
  resource_requirements: ManleAICalendarResourceRequirement[];
  created_at: string | null;
  updated_at: string | null;
};

export type ManleAICalendarResourcePool = {
  id: string;
  name: string;
  capacity: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
};

export type ManleAICalendarException = {
  id: string;
  scope_type: ManleAICalendarExceptionScope;
  staff_id?: string;
  resource_pool_id?: string;
  effect: ManleAICalendarExceptionEffect;
  starts_at: string;
  ends_at: string;
  capacity_override: number | null;
  reason?: string;
  created_by_user_id: string;
  cancelled_at: string | null;
  cancelled_by_user_id?: string;
  created_at: string;
};

export type ManleAICalendarReadinessBlocker = {
  code: string;
  dimension: "configuration" | "execution";
  scope: "calendar" | "service" | "staff" | "resource";
  entity_id?: string;
  message: string;
};

export type ManleAICalendarReadiness = {
  configuration_ready: boolean;
  execution_ready: boolean;
  authority_version: number;
  config_version: number;
  blockers: ManleAICalendarReadinessBlocker[];
  capabilities?: ManleAICalendarCapabilities;
};

export type ManleAICalendarCapabilities = {
  staff_only_availability: boolean;
  staff_only_create: boolean;
  pooled_capacity: boolean;
  party_create: boolean;
  reschedule: boolean;
  cancel: boolean;
};

export type ManleAICalendarAggregate = {
  salon_id: string;
  timezone: string;
  scheduling_authority: SchedulingAuthority;
  authority_version: number;
  config_version: number;
  config: ManleAICalendarConfig | null;
  hours: ManleAICalendarHourPeriod[];
  staff_profiles: ManleAICalendarStaffProfile[];
  service_policies: ManleAICalendarServicePolicy[];
  resources: ManleAICalendarResourcePool[];
  exceptions: ManleAICalendarException[];
  readiness: ManleAICalendarReadiness;
  constraints: ManleAICalendarConstraints;
};

export type ManleAICalendarAggregateResponse = {
  manleai_calendar: ManleAICalendarAggregate;
};

export type ManleAICalendarMutationResponse = ManleAICalendarAggregateResponse & {
  replayed: boolean;
};

export type ManleAICalendarStaffProfileResponse = {
  staff_profile: ManleAICalendarStaffProfile;
  config_version: number;
  readiness: ManleAICalendarReadiness;
};

export type ManleAICalendarServicePolicyResponse = {
  service_policy: ManleAICalendarServicePolicy;
  config_version: number;
  readiness: ManleAICalendarReadiness;
};

export type ManleAICalendarResourceListResponse = {
  resources: ManleAICalendarResourcePool[];
  config_version: number;
  readiness: ManleAICalendarReadiness;
};

export type ManleAICalendarMutationMeta = {
  action_key: string;
  expected_config_version: number;
};

export type ManleAICalendarConfigInput = ManleAICalendarMutationMeta & {
  slot_step_minutes: number;
  minimum_booking_notice_minutes: number;
  booking_horizon_days: number;
  reschedule_cutoff_minutes: number | null;
  cancellation_cutoff_minutes: number | null;
  max_party_size: number;
  default_buffer_before_minutes: number;
  default_buffer_after_minutes: number;
};

export type ManleAICalendarHoursInput = ManleAICalendarMutationMeta & {
  periods: ManleAICalendarHourPeriodInput[];
};

export type ManleAICalendarStaffProfileInput = ManleAICalendarMutationMeta & {
  weekly_periods: ManleAICalendarWeeklyPeriodInput[];
  eligible_service_ids: string[];
};

export type ManleAICalendarServicePolicyInput = ManleAICalendarMutationMeta & {
  enabled: boolean;
  capacity_mode: ManleAICalendarCapacityMode | null;
  buffer_before_minutes_override: number | null;
  buffer_after_minutes_override: number | null;
  eligible_staff_ids: string[];
  resource_requirements: ManleAICalendarResourceRequirementInput[];
};

export type ManleAICalendarResourceInput = ManleAICalendarMutationMeta & {
  name: string;
  capacity: number;
};

export type ManleAICalendarExceptionInput = ManleAICalendarMutationMeta & {
  scope_type: ManleAICalendarExceptionScope;
  staff_id?: string;
  resource_pool_id?: string;
  effect: ManleAICalendarExceptionEffect;
  starts_at: string;
  ends_at: string;
  capacity_override: number | null;
  reason?: string;
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
  webhook_notification_url?: string;
  webhook_configured: boolean;
  webhook_signature_key_configured: boolean;
  webhook_signature_key_source: string;
  updated_at?: string;
  version?: number;
  replayed?: boolean;
};

export type TwilioIntegrationConfig = {
  provider: "twilio" | string;
  configured: boolean;
  voice_route_id: string;
  voice_routing_enabled: boolean;
  voice_inbound_number: string;
  voice_routing_configured: boolean;
  voice_routing_blockers: string[];
  account_sid_hint?: string;
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
  owner_sms_enabled: boolean;
  owner_sms_destination_masked?: string;
  owner_sms_consent_attested: boolean;
  owner_sms_consent_attested_at?: string;
  account_sid_configured: boolean;
  messaging_service_configured: boolean;
  sender_configured: boolean;
  notification_status_path: string;
  notification_inbound_path: string;
  notification_status_url: string;
  notification_inbound_url: string;
  updated_at?: string;
  version?: number;
  replayed?: boolean;
};

export type TwilioVoiceRoutingStatus = {
  routing_configured: boolean;
  live_verified: boolean;
  last_verified_inbound_at?: string;
  last_observed_inbound_at?: string;
  verification_stale: boolean;
  blockers: string[];
};

export type OpenAIIntegrationConfig = {
  provider: "openai" | string;
  enabled: boolean;
  configured: boolean;
  runtime_resolvable?: boolean;
  runtime_blockers?: string[];
  base_url: string;
  destination_profile: string;
  destination_managed: boolean;
  transcription_model: string;
  reply_model: string;
  speech_model: string;
  speech_voice: string;
  speech_output_mode: "streaming_tts" | "buffered_realtime" | string;
  realtime_enabled: boolean;
  realtime_model: string;
  realtime_voice: string;
  realtime_noise_profile: string;
  realtime_instructions: string;
  api_key_configured: boolean;
  api_key_source: string;
  credential_revision?: number;
  credential_unique?: boolean;
  updated_at?: string;
  version?: number;
  replayed?: boolean;
};

export type OpenAIRuntimeVerificationCapability = {
  capability: string;
  required: boolean;
  status: "pending" | "running" | "verified" | "failed" | "stale" | "not_required" | string;
  latency_ms?: number;
  provider_request_id?: string;
  error_code?: string;
  verified_at?: string;
};

export type OpenAIRuntimeVerification = {
  id: string;
  salon_id: string;
  status: "queued" | "claimed" | "succeeded" | "failed" | "stale" | string;
  fresh: boolean;
  config_version: number;
  credential_revision: number;
  destination_policy_version: string;
  verification_contract_version: string;
  attempt_count: number;
  error_code?: string;
  capabilities: OpenAIRuntimeVerificationCapability[];
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
};

export type OpenAIRuntimeVerificationResponse = {
  verification: OpenAIRuntimeVerification;
  replayed: boolean;
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
  included_sections?: string[];
  excluded_data: string[];
  requires_secret_reentry: string[];
  salon_profile?: ConfigurationSalonProfile;
  ai_receptionist?: ConfigurationAIReceptionist;
  public_booking_page?: ConfigurationPublicBookingPage;
  integrations?: IntegrationConfigs;
  integration_providers?: Array<"square" | "twilio" | "openai" | string>;
  pos_connection?: ConfigurationPOSConnection;
  service_categories?: ConfigurationServiceCategoryBundle;
  service_aliases?: ConfigurationServiceAliasBundle;
  service_consultation_profiles?: ConfigurationServiceConsultationProfileBundle;
  knowledge_base?: ConfigurationKnowledgeBase;
  local_business_hours?: ConfigurationLocalBusinessHours;
};

export type ConfigurationLocalBusinessHours = {
  management_mode: "local" | "provider_read_only" | string;
  periods: Array<{
    day_of_week: number;
    start_local_time: string;
    end_local_time: string;
    end_at_midnight: boolean;
  }>;
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
  ai_tone: string;
  booking_mode: string;
  recording_enabled: boolean;
  recording_consent_message: string;
  sms_confirmation_enabled: boolean;
  sms_reminder_enabled: boolean;
  reminder_hours_before: number;
  handoff_enabled: boolean;
  consultation_enabled: boolean;
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

export type ConfigurationServiceCategoryBundle = {
  items: ConfigurationServiceCategory[];
  count: number;
};

export type ConfigurationServiceCategory = {
  source_key: string;
  name: string;
  slug: string;
  description?: string;
  status: string;
  source: string;
  sort_order: number;
  aliases: ConfigurationServiceCategoryAlias[];
  created_at: string;
  updated_at: string;
};

export type ConfigurationServiceCategoryAlias = {
  source_key: string;
  alias: string;
  normalized_alias: string;
  source: string;
  status: string;
  confidence: number;
  created_at: string;
  updated_at: string;
};

export type ConfigurationServiceAliasBundle = {
  items: ConfigurationServiceAlias[];
  count: number;
};

export type ConfigurationServiceAlias = {
  source_key: string;
  alias: string;
  normalized_alias: string;
  target_service: ConfigurationServiceAliasTarget;
  source: string;
  status: string;
  confidence: number;
  created_at: string;
  updated_at: string;
};

export type ConfigurationServiceAliasTarget = {
  name: string;
  duration_minutes?: number;
  price_display?: string;
};

export type ConfigurationServiceConsultationProfileBundle = {
  items: ConfigurationServiceConsultationProfile[];
  count: number;
};

export type ConfigurationServiceConsultationProfile = {
  source_key: string;
  target_service: ConfigurationServiceAliasTarget;
  status: string;
  recommended_outcomes: string[];
  compatible_current_systems: string[];
  length_capabilities: string[];
  priority_tags: string[];
  finish_options: string[];
  maintenance_note?: string;
  owner_approved_summary?: string;
  created_at?: string;
  updated_at?: string;
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
  included_sections: string[];
  target_scheduling_authority: SchedulingAuthority;
  target_scheduling_authority_version: number;
  source_active_pos_provider: string;
  target_active_pos_provider: string;
  result_active_pos_provider: string;
  source_booking_mode: string;
  target_booking_mode: string;
  result_booking_mode: string;
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

export type SquareWebhookFilterStatus =
  | "pending"
  | "processing"
  | "failed"
  | "dead_letter"
  | "succeeded";

export type SquareWebhookProcessingStatus = SquareWebhookFilterStatus | "ignored";

export type SquareWebhookEvent = {
  id: string;
  event_type: string;
  processing_status: SquareWebhookProcessingStatus;
  processing_attempts: number;
  requeue_count: number;
  last_error_class?: string;
  last_error_code?: string;
  can_requeue: boolean;
  requeue_blocked_reason?: string;
  next_attempt_at: string;
  delivered_at?: string;
  processed_at?: string;
  dead_lettered_at?: string;
  created_at: string;
  updated_at: string;
};

export type SquareWebhookMetrics = {
  pending: number;
  processing: number;
  failed: number;
  dead_letter: number;
  succeeded_recent: number;
  recent_window_hours: number;
  last_delivered_at?: string;
  last_succeeded_at?: string;
};

export type SquareCalendarRepairHealth = {
  relevant: boolean;
  status: string;
  repair_attempts: number;
  last_error_class?: string;
  last_error_code?: string;
  next_repair_at?: string;
  lease_expires_at?: string;
  last_repaired_at?: string;
  updated_at?: string;
};

export type SquareWebhookEventsResponse = {
  events: SquareWebhookEvent[];
  metrics: SquareWebhookMetrics;
  calendar_repair: SquareCalendarRepairHealth;
  limit: number;
  offset: number;
  has_more: boolean;
};

export type SquareWebhookEventDetailResponse = {
  event: SquareWebhookEvent;
};

export type POSFieldAuthority = {
  operational_source: "manleai" | "provider" | string;
  provider?: string;
  provider_label?: string;
  operational_write_mode: "local" | "provider_read_only" | "provider_sync" | string;
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
  service_category_id?: string;
  category_name?: string;
  category_slug?: string;
  category_source: string;
  category_confidence?: number;
  category_reviewed_at?: string;
  consultation_profile?: ServiceConsultationProfile;
  field_authority?: POSFieldAuthority;
};

export type ServiceConsultationProfile = {
  status: "draft" | "ready" | "disabled" | string;
  recommended_outcomes: string[];
  compatible_current_systems: string[];
  length_capabilities: string[];
  priority_tags: string[];
  finish_options: string[];
  maintenance_note?: string;
  owner_approved_summary?: string;
  revision: number;
};

export type POSServiceCategory = {
  id: string;
  salon_id: string;
  name: string;
  slug: string;
  description?: string;
  status: string;
  sort_order: number;
  source: string;
  service_count: number;
  aliases?: POSServiceCategoryAlias[];
  reviewed_at?: string;
  archived_at?: string;
  created_at: string;
  updated_at: string;
};

export type POSServiceCategoryAlias = {
  id: string;
  salon_id: string;
  category_id: string;
  category_name?: string;
  alias: string;
  normalized_alias: string;
  source: string;
  status: string;
  confidence: number;
  created_at: string;
  updated_at: string;
};

export type ServiceCategorySuggestionRefresh = {
  created_categories: number;
  restored_system_categories: number;
  created_aliases: number;
  updated_system_aliases: number;
  skipped_alias_conflicts: number;
  suggested_services: number;
  skipped_reviewed_services: number;
  skipped_ambiguous_services: number;
  unmatched_unreviewed_services: number;
  created_service_aliases: number;
  updated_system_service_aliases: number;
  skipped_service_alias_conflicts: number;
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
  field_authority?: POSFieldAuthority;
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
  active_customers?: number;
  pos_linked_customers?: number;
  confirmed_appointments: number;
  pending_requests: number;
  customers_with_calls: number;
  last_customer_activity_at?: string;
};

export type StaffSelectionMode = "specific" | "anyone" | string;

export type AppointmentStatus =
  | "confirmed"
  | "rescheduled"
  | "provider_pending"
  | "cancelled"
  | "declined"
  | "no_show"
  | "unknown"
  | string;

export type BookingSegmentSnapshot = {
  appointment_service_id?: string;
  scheduling_authority?: SchedulingAuthority;
  service_id?: string;
  service_name: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  guest_reference?: string;
  quantity?: number;
  plan_version?: number;
  duration_minutes?: number;
  scheduled_start_time?: string;
  scheduled_end_time?: string;
  occupied_start_time?: string;
  occupied_end_time?: string;
  buffer_before_minutes?: number;
  buffer_after_minutes?: number;
  resource_allocations?: AvailabilityResourceAllocation[];
  sort_order: number;
};

export type BookingSegmentRequest = {
  service_id: string;
  staff_id?: string;
  staff_selection_mode: StaffSelectionMode;
  guest_reference?: string;
  quantity?: number;
};

export type SchedulingRequestStatus = "pending" | "contacted" | "resolved" | "dismissed";

export type SchedulingRequestOperation = "book" | "reschedule" | "cancel";

export type SchedulingRequestSegment = {
  id: string;
  scheduling_request_id: string;
  service_id: string;
  service_name: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode: StaffSelectionMode;
  guest_reference?: string;
  quantity: number;
  duration_minutes: number;
  requested_start_time?: string;
  requested_end_time?: string;
  sort_order: number;
  redacted: boolean;
  redacted_at?: string;
  redaction_version?: number;
  created_at: string;
};

export type SchedulingRequestEvent = {
  id: string;
  scheduling_request_id: string;
  action_key: string;
  event_type: string;
  request_version: number;
  actor_user_id?: string;
  payload: unknown;
  redacted: boolean;
  redacted_at?: string;
  redaction_version?: number;
  created_at: string;
};

export type SchedulingRequest = {
  id: string;
  salon_id: string;
  scheduling_authority: "owner_manual";
  operation_key: string;
  operation_type: SchedulingRequestOperation;
  source: string;
  status: SchedulingRequestStatus;
  version: number;
  call_session_id?: string;
  customer_name: string;
  customer_phone: string;
  customer_email?: string;
  requested_start_time?: string;
  requested_end_time?: string;
  requested_timezone?: string;
  party_size?: number;
  notes?: string;
  target_description?: string;
  target_appointment_id?: string;
  target_scheduling_authority?: SchedulingAuthority;
  segments?: SchedulingRequestSegment[];
  events?: SchedulingRequestEvent[];
  resolution_reason?: string;
  redacted: boolean;
  redacted_at?: string;
  redaction_version?: number;
  contacted_at?: string;
  resolved_at?: string;
  dismissed_at?: string;
  created_at: string;
  updated_at: string;
};

export type SchedulingRequestsResponse = {
  scheduling_requests: SchedulingRequest[];
  limit?: number;
  offset?: number;
  has_more?: boolean;
  total?: number;
};

export type SchedulingRequestResponse = {
  scheduling_request: SchedulingRequest;
};

export type UpdateSchedulingRequestInput = {
  action_key: string;
  expected_version: number;
  status: SchedulingRequestStatus;
  resolution_reason?: string;
  note?: string;
};

export type OwnerNotificationDeliveryStatus =
  | "queued"
  | "delivering"
  | "provider_accepted"
  | "sent"
  | "delivered"
  | "failed"
  | "undelivered"
  | "dead_letter"
  | "disabled";

export type OwnerNotificationDeliveryEvent = {
  id: string;
  event_type: string;
  delivery_status: OwnerNotificationDeliveryStatus;
  provider_status?: string;
  error_code?: string;
  created_at: string;
};

export type OwnerNotificationDelivery = {
  id: string;
  salon_id: string;
  notification_type: string;
  in_app_status: string;
  delivery_status: OwnerNotificationDeliveryStatus;
  delivery_provider?: string;
  destination_masked?: string;
  delivery_attempts: number;
  provider_status?: string;
  last_delivery_error_code?: string;
  can_requeue: boolean;
  requeue_blocked_reason?: string;
  next_delivery_at: string;
  delivered_at?: string;
  dead_lettered_at?: string;
  last_provider_event_at?: string;
  redacted: boolean;
  redacted_at?: string;
  redaction_version?: number;
  created_at: string;
  events?: OwnerNotificationDeliveryEvent[];
};

export type OwnerNotificationDeliveryMetrics = {
  queued: number;
  delivering: number;
  provider_accepted: number;
  sent: number;
  delivered: number;
  dead_letter: number;
  disabled: number;
};

export type OwnerNotificationDeliveriesResponse = {
  deliveries: OwnerNotificationDelivery[];
  metrics: OwnerNotificationDeliveryMetrics;
  limit: number;
  offset: number;
  has_more: boolean;
};

export type OwnerNotificationDeliveryDetailResponse = {
  delivery: OwnerNotificationDelivery;
};

export type AppointmentRecord = {
  id: string;
  salon_id: string;
  booking_attempt_id: string;
  scheduling_authority: SchedulingAuthority;
  authority_provider?: string;
  authority_appointment_id: string;
  authority_appointment_version?: number;
  /** @deprecated External-provider compatibility alias. Dispatch and status use scheduling_authority plus authority_* evidence. */
  pos_provider?: string;
  /** @deprecated External-provider compatibility alias. */
  pos_appointment_id?: string;
  /** @deprecated External-provider compatibility alias. */
  pos_appointment_version?: number;
  status: AppointmentStatus;
  party_size?: number;
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
  pos_sync_status?: string;
  last_pos_synced_at?: string;
  pos_sync_error?: string;
  sync_warning?: string;
  can_edit?: boolean;
  can_cancel?: boolean;
  can_delete?: boolean;
  confirmed_at?: string;
  confirmation_source?: string;
  created_at: string;
  updated_at: string;
};

export type BookingAttempt = {
  id: string;
  salon_id: string;
  scheduling_authority: SchedulingAuthority;
  authority_provider?: string;
  authority_appointment_id?: string;
  authority_appointment_version?: number;
  source: string;
  status: string;
  /** @deprecated External-provider compatibility alias. Never infer origin from this field. */
  pos_provider?: string;
  /** @deprecated External-provider compatibility alias. */
  pos_booking_id?: string;
  /** @deprecated External-provider compatibility alias. */
  pos_booking_version?: number;
  operation_type: "book" | "reschedule" | "cancel";
  provider_outcome: "not_started" | "in_flight" | "succeeded" | "failed" | "unknown";
  retry_policy: "none" | "safe" | "blocked";
  reconciliation_status: "not_required" | "required" | "resolved";
  processing_lease_expires_at?: string;
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
  sync_warning?: string;
  can_retry: boolean;
  retry_blocked_reason?: string;
  booking_action?: "book" | "reschedule" | "cancel";
  target_appointment_id?: string;
  retry_of_attempt_id?: string;
  superseded_by_attempt_id?: string;
  retry_sequence?: number;
  superseded_at?: string;
  reconciliation_resolution?: "provider_attached" | "not_created" | "escalated";
  reconciliation_resolved_at?: string;
  notification_type?: string;
  notification_status?: string;
  created_at: string;
  updated_at: string;
  appointment?: AppointmentRecord;
};

export type BookingReconciliationTask = {
  id: string;
  salon_id: string;
  booking_attempt_id: string;
  status: "open" | "resolved" | "escalated";
  resolution?: "provider_attached" | "not_created" | "escalated";
  resolution_note?: string;
  resolved_at?: string;
  created_at: string;
  updated_at: string;
  booking_attempt?: BookingAttempt;
};

export type BookingReconciliationCandidate = {
  appointment_id: string;
  provider: string;
  provider_appointment_id: string;
  provider_appointment_version: number;
  provider_status: string;
  customer_name: string;
  customer_phone: string;
  customer_email?: string;
  service_id: string;
  staff_id?: string;
  start_time: string;
  end_time: string;
};

export type AvailabilitySlot = {
  fingerprint?: string;
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
  guest_reference?: string;
  quantity: number;
  duration_minutes: number;
  scheduled_start_time: string;
  scheduled_end_time: string;
  occupied_start_time: string;
  occupied_end_time: string;
  buffer_before_minutes: number;
  buffer_after_minutes: number;
  resource_allocations: AvailabilityResourceAllocation[];
};

export type AvailabilityResourceAllocation = {
  resource_pool_id: string;
  resource_name: string;
  units_allocated: number;
};

export type AvailabilityResult = {
  quote_id?: string;
  request_fingerprint?: string;
  expires_at?: string;
  target_authority_appointment_version?: number;
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

export type SchedulingAvailabilityResponse =
  | {
      kind: "verified_slots";
      scheduling_authority: SchedulingAuthority;
      target_authority_appointment_version?: number;
      verified_slots: AvailabilityResult;
    }
  | {
      kind: "request_only";
      scheduling_authority: SchedulingAuthority;
      verified_slots?: never;
    };

export type SchedulingActionSegmentInput = {
  service_id: string;
  staff_id?: string;
  staff_selection_mode: StaffSelectionMode;
  guest_reference?: string;
  quantity: number;
  requested_start_time?: string;
  requested_end_time?: string;
};

export type SchedulingActionInput = {
  operation_type: "book" | "reschedule" | "cancel";
  operation_key: string;
  retry_of_attempt_id?: string;
  availability_quote_id?: string;
  slot_fingerprint?: string;
  customer_name?: string;
  customer_phone?: string;
  customer_email?: string;
  segments?: SchedulingActionSegmentInput[];
  requested_start_time?: string;
  requested_end_time?: string;
  requested_timezone?: string;
  party_size?: number;
  notes?: string;
  target_appointment_id?: string;
  target_scheduling_authority?: SchedulingAuthority;
  expected_target_authority_appointment_version?: number;
  target_description?: string;
};

export type SchedulingConfirmedAppointment = {
  appointment_id: string;
  booking_attempt_id?: string;
  appointment_status?: AppointmentStatus;
  active_child_count?: number;
  external_attempt_id?: string;
  appointment?: AppointmentRecord;
  external_attempt?: BookingAttempt;
  children?: SchedulingConfirmedAppointmentSegment[];
};

export type SchedulingConfirmedAppointmentSegment = {
  appointment_service_id: string;
  guest_reference?: string;
  service_id: string;
  staff_id: string;
  staff_selection_mode: StaffSelectionMode;
  quantity: number;
  scheduled_start_time: string;
  scheduled_end_time: string;
  occupied_start_time: string;
  occupied_end_time: string;
  buffer_before_minutes: number;
  buffer_after_minutes: number;
  resource_allocations: AvailabilityResourceAllocation[];
};

export type SchedulingActionResponse =
  | {
      kind: "confirmed_appointment";
      operation_type: "book" | "reschedule" | "cancel";
      scheduling_authority: SchedulingAuthority;
      target_authority_appointment_version?: number;
      authority_appointment_version?: number;
      replayed?: boolean;
      confirmed_appointment: SchedulingConfirmedAppointment;
    }
  | {
      kind: "pending_owner_review";
      operation_type: "book" | "reschedule" | "cancel";
      scheduling_authority: SchedulingAuthority;
      target_authority_appointment_version?: number;
      authority_appointment_version?: number;
      replayed?: boolean;
      pending_owner_review: {
        scheduling_request_id: string;
        status: string;
        version: number;
        request?: SchedulingRequest;
      };
    }
  | {
      kind: "external_fallback_pending";
      operation_type: "book" | "reschedule" | "cancel";
      scheduling_authority: SchedulingAuthority;
      target_authority_appointment_version?: number;
      authority_appointment_version?: number;
      replayed?: boolean;
      external_fallback_pending: {
        external_attempt_id: string;
        external_attempt?: BookingAttempt;
      };
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
  operation_type: "book" | "reschedule" | "cancel";
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
  provider_outcome: "not_started" | "in_flight" | "succeeded" | "failed" | "unknown";
  retry_policy: "none" | "safe" | "blocked";
  reconciliation_status: "not_required" | "required" | "resolved";
  can_retry: boolean;
  retry_blocked_reason?: string;
  created_at?: string;
  updated_at?: string;
};

export type SquareReadiness = {
  ai_enabled: boolean;
  ai_runtime_version: number;
  scheduling_authority: SchedulingAuthority;
  can_test_booking: boolean;
  can_cancel_test_booking: boolean;
  can_enable_ai_booking: boolean;
  booking_write_blocked?: boolean;
  booking_write_blocked_code?: string;
  booking_write_blocked_reason?: string;
  booking_write_blocked_at?: string;
  appointment_change_write_blocked?: boolean;
  appointment_change_write_blocked_code?: string;
  appointment_change_write_blocked_reason?: string;
  appointment_change_write_blocked_at?: string;
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
  metadata?: Record<string, unknown>;
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

export type PartyGuestService = {
  guest_reference?: string;
  service_id?: string;
  service_name?: string;
  quantity?: number;
  sort_order?: number;
  notes?: string;
};

export type PartyBookingRequest = {
  id: string;
  salon_id: string;
  call_session_id: string;
  event_key?: string;
  status: string;
  party_size?: number;
  representative_name?: string;
  representative_phone?: string;
  requested_date?: string;
  requested_time_window?: string;
  guest_service_requests?: PartyGuestService[];
  flexibility_notes?: string;
  summary: string;
  created_at: string;
  updated_at: string;
  resolved_at?: string;
  resolved_by?: string;
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

export type PendingConversationAct = {
  kind: string;
  source_service_ids?: string[];
  target_service_ids?: string[];
  source_category_id?: string;
  source_category_name?: string;
  target_category_id?: string;
  target_category_name?: string;
  scope?: string;
  guest_scope?: "caller" | "another_guest" | string;
  prompt_key: string;
};

export type ConversationDraftMutation = {
  kind: string;
  before_service_id?: string;
  before_service_name?: string;
  before_service_ids: string[];
  before_segments: BookingSegmentRequest[];
  after_service_ids: string[];
  after_segments: BookingSegmentRequest[];
};

export type ConversationDialogState = {
  version: number;
  phase: "open" | "drafting" | "clarifying" | "availability" | "review" | string;
  pending?: PendingConversationAct;
  last_mutation?: ConversationDraftMutation;
  mutation_history?: ConversationDraftMutation[];
  review_required: boolean;
  review_accepted: boolean;
  no_progress_count: number;
  last_prompt_key?: string;
  last_act_kind?: string;
  draft_revision: number;
  reviewed_revision: number;
  authorized_revision: number;
  last_mutation_revision?: number;
  reviewed_booking_mode?: BookingMode;
  selected_scheduling_authority?: SchedulingAuthority;
  consultation?: ConversationConsultationState;
};

export type ConversationConsultationState = {
  status: string;
  resume_phase: string;
  needs: {
    current_system?: string;
    desired_outcome?: string;
    length_change?: string;
    priorities?: string[];
    desired_finishes?: string[];
    compared_service_ids?: string[];
    booking_requested?: boolean;
    conversation_complete?: boolean;
  };
  candidate_service_ids?: string[];
  recommended_service_ids?: string[];
  selected_service_id?: string;
  last_asked_field?: string;
  profile_revisions?: Record<string, number>;
  recommendation_reasons?: Record<string, string[]>;
  no_progress_count: number;
  exit_reason?: string;
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
  booking_action?: "book" | "reschedule" | "cancel";
  target_appointment_id?: string;
  reschedule_candidates?: RescheduleCandidate[];
  customer_name?: string;
  customer_phone?: string;
  customer_email?: string;
  service_id?: string;
  service_name?: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode?: StaffSelectionMode;
  requested_date?: string;
  requested_start_time?: string;
  offered_slots?: OfferedSlot[];
  booking_segments?: BookingSegmentRequest[];
  dialog_state: ConversationDialogState;
  booking_attempt_id?: string;
  appointment_id?: string;
  scheduling_request_id?: string;
  scheduling_result_evidence?: ConversationSchedulingResultEvidence;
  summary?: string;
  lifecycle_status: "active" | "archived" | "redacted";
  archived_at?: string;
  redacted_at?: string;
  retention_expires_at: string;
  started_at: string;
  ended_at?: string;
  created_at: string;
  updated_at: string;
  transcript?: TranscriptMessage[];
  handoff?: HandoffRequest;
  party_request?: PartyBookingRequest;
};

export type ConversationSchedulingResultEvidence = {
  complete: boolean;
  kind: "incomplete" | "pending_owner_review" | "completed_operation";
  scheduling_authority?: SchedulingAuthority;
  target_scheduling_authority?: SchedulingAuthority;
  operation_type?: "book" | "reschedule" | "cancel";
  result_status: "incomplete" | "confirmed" | "rescheduled" | "cancelled" | "owner_review_pending";
  current_status: string;
  is_current: boolean;
  appointment_id?: string;
  booking_attempt_id?: string;
  scheduling_request_id?: string;
  authority_appointment_version?: number;
  current_authority_appointment_version?: number;
  root_count: number;
  result_child_count: number;
  current_active_child_count: number;
  incomplete_reason?: string;
};

export type RescheduleCandidate = {
  appointment_id: string;
  scheduling_authority?: SchedulingAuthority;
  authority_appointment_version?: number;
  party_size?: number;
  status?: AppointmentStatus;
  active_child_count?: number;
  service_label: string;
  staff_label: string;
  service_id?: string;
  staff_id?: string;
  staff_selection_mode?: StaffSelectionMode;
  segments?: BookingSegmentRequest[];
  start_time: string;
  end_time: string;
};

export type RealtimeEventLog = {
  id: string;
  provider: string;
  provider_call_id?: string;
  event_type: string;
  stage?: string;
  stream_sid?: string;
  stream_event?: string;
  stream_error?: string;
  error?: string;
  diagnostics?: Record<string, string>;
  redacted?: boolean;
  created_at: string;
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
  phone_answering_ready: boolean;
  request_capture_ready: boolean;
  automated_booking_ready: boolean;
  phone_booking_ready: boolean;
  scheduling_authority: SchedulingAuthority;
  scheduling_authority_version: number;
  booking_mode: "confirmed_booking" | "pending_approval" | "disabled" | string;
  phone_answering: VoiceReadinessDimension;
  request_capture: VoiceReadinessDimension;
  automated_booking: VoiceReadinessDimension;
  blocked_reason?: string;
  input_mode: "gather" | "recording" | "realtime_stream" | string;
  ai: VoiceAIStatus;
  booking: VoiceBookingReadiness;
};

export type VoiceReadinessBlocker = {
  code: string;
  scope?: string;
  entity_id?: string;
  message: string;
};

export type VoiceReadinessDimension = {
  ready: boolean;
  blockers: VoiceReadinessBlocker[];
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
  booking_write_blocked?: boolean;
  booking_write_blocked_code?: string;
  booking_write_blocked_reason?: string;
  booking_write_blocked_at?: string;
  guidance_service_count: number;
  service_count: number;
  consultation_enabled: boolean;
  consultation_ready_service_count: number;
  service_guidance: {
    status: "recommendation_ready" | "catalog_only" | "consultation_disabled" | "catalog_unavailable";
    catalog_available: boolean;
    consultation_enabled: boolean;
    recommendation_ready: boolean;
    ready_service_count: number;
    message?: string;
  };
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

export type ServiceAlias = {
  id: string;
  salon_id: string;
  service_id: string;
  service_name: string;
  alias: string;
  normalized_alias: string;
  source: string;
  status: string;
  confidence: number;
  correction_id?: string;
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
  applied_service_alias_id?: string;
  created_at: string;
  updated_at: string;
};

export type OperationsHealthStatus = "healthy" | "running" | "degraded" | "stale" | "unknown";

export type OperationsHealthLink = {
  label: string;
  href: string;
};

export type OperationsHealthJob = {
  key: string;
  label: string;
  status: OperationsHealthStatus;
  last_started_at?: string;
  last_completed_at?: string;
  last_success_at?: string;
  last_heartbeat_at?: string;
  last_duration_ms?: number;
  last_processed_count?: number;
  error_class?: string;
  error_code?: string;
  stale_after_seconds: number;
  links: OperationsHealthLink[];
};

export type OperationsHealthQueue = {
  key: string;
  label: string;
  status: OperationsHealthStatus;
  backlog_count: number;
  oldest_at?: string;
  dead_letter_count: number;
  error_code?: string;
  links: OperationsHealthLink[];
};

export type OperationsHealthResponse = {
  status: OperationsHealthStatus;
  evaluated_at: string;
  jobs: OperationsHealthJob[];
  queues: OperationsHealthQueue[];
};
