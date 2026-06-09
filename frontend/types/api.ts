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
  requested_start_time: string;
  requested_end_time: string;
  notes?: string;
  error_code?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
  appointment?: AppointmentRecord;
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
  latest_test_booking?: TestBookingRecord;
  checks: ReadinessCheck[];
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
  requested_start_time?: string;
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
  salon_phone?: string;
  ready: boolean;
  blocked_reason?: string;
};
