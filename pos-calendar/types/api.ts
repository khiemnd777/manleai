export type User = {
  id: string;
  email: string;
  full_name: string;
  phone?: string;
  status: string;
};

export type SchedulingAuthority = "owner_manual" | "manleai_calendar" | "external_provider";

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
  updated_at?: string;
};

export type POSService = {
  id?: string;
  salon_id?: string;
  pos_provider: string;
  pos_service_id: string;
  pos_service_version?: number;
  name: string;
  description?: string;
  duration_minutes: number;
  price_cents?: number;
  currency?: string;
  active: boolean;
  ai_bookable: boolean;
  sync_status: string;
  pos_linked: boolean;
  last_synced_at?: string;
  sync_error?: string;
};

export type POSStaffMember = {
  id?: string;
  salon_id?: string;
  pos_provider: string;
  pos_staff_id: string;
  name: string;
  email?: string;
  phone?: string;
  active: boolean;
  ai_bookable: boolean;
  sync_status: string;
  pos_linked: boolean;
  last_synced_at?: string;
  sync_error?: string;
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
  scheduling_authority?: SchedulingAuthority;
  authority_provider?: string;
  authority_appointment_id?: string;
  authority_appointment_version?: number;
  /** @deprecated External-provider compatibility alias. Never infer appointment origin from this field. */
  pos_provider?: string;
  /** @deprecated External-provider compatibility alias. */
  pos_appointment_id?: string;
  pos_appointment_version?: number;
  status: AppointmentStatus;
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
  created_at: string;
  updated_at: string;
};

export type BookingAttempt = {
  id: string;
  salon_id: string;
  source: string;
  status: string;
  scheduling_authority?: SchedulingAuthority;
  authority_provider?: string;
  authority_appointment_id?: string;
  authority_appointment_version?: number;
  /** @deprecated External-provider compatibility alias. Never infer attempt origin from this field. */
  pos_provider?: string;
  /** @deprecated External-provider compatibility alias. */
  pos_booking_id?: string;
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
  duration_minutes: number;
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

export type SyncLog = {
  id: string;
  salon_id: string;
  provider: string;
  sync_type: string;
  status: string;
  message?: string;
  started_at?: string;
  completed_at?: string;
  created_at?: string;
  updated_at?: string;
};

export type ReadinessCheck = {
  key: string;
  label: string;
  complete: boolean;
  detail?: string;
};

export type SquareReadiness = {
  salon_id: string;
  provider: string;
  connected: boolean;
  location_selected: boolean;
  service_count: number;
  staff_count: number;
  bookable_service_count: number;
  bookable_staff_count: number;
  ai_enabled: boolean;
  scheduling_authority: SchedulingAuthority;
  booking_write_blocked?: boolean;
  can_run_test_booking?: boolean;
  can_cancel_test_booking?: boolean;
  checks: ReadinessCheck[];
};

export type CalendarWarningSummary = {
  total_warnings: number;
  not_synced: number;
  sync_failed: number;
  pending_pos_sync: number;
  provider_pending?: number;
  fallback_pending: number;
};

export type CalendarRangeResponse = {
  salon_id: string;
  start_time: string;
  end_time: string;
  view: CalendarView;
  appointments: AppointmentRecord[];
  pending_requests: BookingAttempt[];
  warnings: CalendarWarningSummary;
};

export type CalendarEvent = {
  id: string;
  cursor: string;
  salon_id: string;
  type: "booking_confirmed" | "booking_fallback_pending" | string;
  notification_status: string;
  title: string;
  message: string;
  booking_attempt_id?: string;
  appointment_id?: string;
  source?: string;
  booking_status?: string;
  customer_name?: string;
  service_id?: string;
  staff_id?: string;
  start_time: string;
  end_time: string;
  created_at: string;
};

export type CalendarSyncSummary = {
  imported: number;
  updated: number;
  skipped: number;
};

export type CalendarSyncResponse = {
  provider: string;
  summary: CalendarSyncSummary;
  range: {
    start_time: string;
    end_time: string;
  };
};

export type CalendarView = "day" | "week" | "month" | "agenda";
