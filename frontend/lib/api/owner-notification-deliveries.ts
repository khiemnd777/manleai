import { apiRequest } from "@/lib/api/client";
import type {
  OwnerNotificationDeliveryDetailResponse,
  OwnerNotificationDeliveriesResponse
} from "@/types/api";

export function listOwnerNotificationDeliveries(salonID: string, limit = 25, offset = 0) {
  const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  return apiRequest<OwnerNotificationDeliveriesResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/owner-notification-deliveries?${query.toString()}`
  );
}

export function getOwnerNotificationDelivery(salonID: string, notificationID: string) {
  return apiRequest<OwnerNotificationDeliveryDetailResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/owner-notification-deliveries/${encodeURIComponent(notificationID)}`
  );
}

export function requeueOwnerNotificationDelivery(
  salonID: string,
  notificationID: string,
  actionKey: string
) {
  return apiRequest<OwnerNotificationDeliveryDetailResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/owner-notification-deliveries/${encodeURIComponent(notificationID)}/requeue`,
    { method: "POST", body: JSON.stringify({ action_key: actionKey }) }
  );
}

export function newOwnerNotificationDeliveryActionKey() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `owner-notification-requeue-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
