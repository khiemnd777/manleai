import { apiRequest } from "@/lib/api/client";
import type {
  OwnerNotificationDeliveryDetailResponse,
  OwnerNotificationDeliveriesResponse
} from "@/types/api";

export type OwnerNotificationSurface = "tenant" | "platform";

export async function listOwnerNotificationDeliveries(salonID: string, limit = 25, offset = 0, surface: OwnerNotificationSurface = "tenant") {
  const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  return deliveryRequest<OwnerNotificationDeliveriesResponse>(`${deliveryPath(salonID, surface)}?${query.toString()}`, surface);
}

export async function getOwnerNotificationDelivery(salonID: string, notificationID: string, surface: OwnerNotificationSurface = "tenant") {
  return deliveryRequest<OwnerNotificationDeliveryDetailResponse>(`${deliveryPath(salonID, surface)}/${encodeURIComponent(notificationID)}`, surface);
}

export async function requeueOwnerNotificationDelivery(
  salonID: string,
  notificationID: string,
  actionKey: string,
  surface: OwnerNotificationSurface = "tenant"
) {
  return deliveryRequest<OwnerNotificationDeliveryDetailResponse>(
    `${deliveryPath(salonID, surface)}/${encodeURIComponent(notificationID)}/requeue`,
    surface,
    { method: "POST", body: JSON.stringify({ action_key: actionKey }) }
  );
}

function deliveryPath(salonID: string, surface: OwnerNotificationSurface) {
  const encoded = encodeURIComponent(salonID);
  return surface === "platform"
    ? `/api/v2/platform/tenants/${encoded}/operations/owner-notifications`
    : `/api/salons/${encoded}/owner-notification-deliveries`;
}

async function deliveryRequest<T>(path: string, surface: OwnerNotificationSurface, init: RequestInit = {}) {
  if (surface === "tenant") return apiRequest<T>(path, init);
  const response = await apiRequest<{ data: T }>(path, init);
  return response.data;
}

export function newOwnerNotificationDeliveryActionKey() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `owner-notification-requeue-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
