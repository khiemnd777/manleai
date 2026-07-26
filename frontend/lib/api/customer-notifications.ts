import { apiRequest } from "@/lib/api/client";

export type CustomerSMSPolicy = {
  enabled: boolean;
  quiet_start?: string;
  quiet_end?: string;
  timezone: string;
  version: number;
  ready: boolean;
};

export type CustomerSMSConsent = {
  id: string;
  status: "pending" | "consented" | "declined" | "opted_out";
  destination_masked: string;
  version: number;
  source: string;
  requested_at?: string;
  consented_at?: string;
  declined_at?: string;
  opted_out_at?: string;
  updated_at: string;
};

export type CustomerNotificationEvent = {
  event_type: string;
  delivery_status: string;
  provider_status?: string;
  error_code?: string;
  created_at: string;
};

export type CustomerNotificationDelivery = {
  id: string;
  notification_type: string;
  delivery_status: string;
  destination_masked: string;
  delivery_attempts: number;
  provider_status?: string;
  last_delivery_error_code?: string;
  can_requeue: boolean;
  requeue_blocked_reason?: string;
  next_delivery_at: string;
  delivered_at?: string;
  dead_lettered_at?: string;
  redacted: boolean;
  redacted_at?: string;
  redaction_version?: number;
  created_at: string;
  events?: CustomerNotificationEvent[];
};

export type CustomerNotificationDetail = {
  consent?: CustomerSMSConsent;
  deliveries: CustomerNotificationDelivery[];
};

export function getCustomerSMSPolicy(salonID: string) {
  return apiRequest<CustomerSMSPolicy>(
    `/api/salons/${encodeURIComponent(salonID)}/customer-sms-policy`
  );
}

export function updateCustomerSMSPolicy(
  salonID: string,
  input: { enabled: boolean; quietStart: string; quietEnd: string; expectedVersion: number }
) {
  return apiRequest<CustomerSMSPolicy>(
    `/api/salons/${encodeURIComponent(salonID)}/customer-sms-policy`,
    {
      method: "PUT",
      body: JSON.stringify({
        enabled: input.enabled,
        quiet_start: input.quietStart,
        quiet_end: input.quietEnd,
        expected_version: input.expectedVersion
      })
    }
  );
}

export function getAppointmentCustomerNotifications(salonID: string, appointmentID: string) {
  return apiRequest<CustomerNotificationDetail>(
    `/api/salons/${encodeURIComponent(salonID)}/appointments/${encodeURIComponent(appointmentID)}/customer-notifications`
  );
}

export function getRequestCustomerNotifications(salonID: string, requestID: string) {
  return apiRequest<CustomerNotificationDetail>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-requests/${encodeURIComponent(requestID)}/customer-notifications`
  );
}

export function attestCustomerSMSConsent(
  salonID: string,
  input: { destination: string; attested: boolean; actionKey: string }
) {
  return apiRequest<CustomerSMSConsent>(
    `/api/salons/${encodeURIComponent(salonID)}/customer-sms-consents/attest`,
    {
      method: "POST",
      body: JSON.stringify({
        destination: input.destination,
        attested: input.attested,
        action_key: input.actionKey
      })
    }
  );
}

export function requeueCustomerNotification(
  salonID: string,
  appointmentID: string,
  deliveryID: string,
  actionKey: string
) {
  return apiRequest<CustomerNotificationDetail>(
    `/api/salons/${encodeURIComponent(salonID)}/appointments/${encodeURIComponent(appointmentID)}/customer-notifications/${encodeURIComponent(deliveryID)}/requeue`,
    { method: "POST", body: JSON.stringify({ action_key: actionKey }) }
  );
}

export function requeueRequestCustomerNotification(
  salonID: string,
  requestID: string,
  deliveryID: string,
  actionKey: string
) {
  return apiRequest<CustomerNotificationDetail>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-requests/${encodeURIComponent(requestID)}/customer-notifications/${encodeURIComponent(deliveryID)}/requeue`,
    { method: "POST", body: JSON.stringify({ action_key: actionKey }) }
  );
}

export function newCustomerNotificationActionKey(prefix: string) {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return `${prefix}:${crypto.randomUUID()}`;
  return `${prefix}:${Date.now()}:${Math.random().toString(16).slice(2)}`;
}
