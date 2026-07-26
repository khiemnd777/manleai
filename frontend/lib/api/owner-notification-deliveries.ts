import { apiRequest } from "@/lib/api/client";
import type {
  OwnerNotificationDeliveryDetailResponse,
  OwnerNotificationDeliveriesResponse
} from "@/types/api";

export type OwnerNotificationSurface = "tenant" | "platform";

export function listOwnerNotificationDeliveries(salonID: string, limit = 25, offset = 0, surface: OwnerNotificationSurface = "tenant") {
  const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  return apiRequest<OwnerNotificationDeliveriesResponse>(
    `${deliveryPath(salonID, surface)}?${query.toString()}`
  );
}

export function getOwnerNotificationDelivery(salonID: string, notificationID: string, surface: OwnerNotificationSurface = "tenant") {
  return apiRequest<OwnerNotificationDeliveryDetailResponse>(
    `${deliveryPath(salonID, surface)}/${encodeURIComponent(notificationID)}`
  );
}

export function requeueOwnerNotificationDelivery(
  salonID: string,
  notificationID: string,
  actionKey: string,
  surface: OwnerNotificationSurface = "tenant"
) {
  return apiRequest<OwnerNotificationDeliveryDetailResponse>(
    `${deliveryPath(salonID, surface)}/${encodeURIComponent(notificationID)}/requeue`,
    { method: "POST", body: JSON.stringify({ action_key: actionKey }) }
  );
}

function deliveryPath(salonID: string, surface: OwnerNotificationSurface) {
  const encoded = encodeURIComponent(salonID);
  return surface === "platform"
    ? `/api/platform/tenants/${encoded}/operations/owner-notification-deliveries`
    : `/api/salons/${encoded}/owner-notification-deliveries`;
}

export function newOwnerNotificationDeliveryActionKey() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `owner-notification-requeue-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
