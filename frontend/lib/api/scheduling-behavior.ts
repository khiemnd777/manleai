import { apiRequest } from "@/lib/api/client";
import { platformBookingModePath, platformSchedulingBehaviorPath } from "@/lib/api/scheduling-behavior-routes";
import type { BookingMode, SchedulingBehavior } from "@/types/api";

type PlatformResourceMeta = {
  replayed: boolean;
  resource_version: number;
  permissions: { can_read: boolean; allowed_actions: string[] };
};

export type PlatformSchedulingBehaviorResponse = {
  data: SchedulingBehavior;
  meta: PlatformResourceMeta;
};

export type BookingModeMutationResponse = {
  data: { booking_mode: BookingMode; version: number };
  meta: PlatformResourceMeta;
};

export function getPlatformSchedulingBehavior(salonID: string) {
  return apiRequest<PlatformSchedulingBehaviorResponse>(platformSchedulingBehaviorPath(salonID));
}

export function updatePlatformBookingMode(
  salonID: string,
  bookingMode: BookingMode,
  expectedVersion: number,
  actionKey: string
) {
  return apiRequest<BookingModeMutationResponse>(platformBookingModePath(salonID), {
    method: "PUT",
    body: JSON.stringify({ booking_mode: bookingMode, expected_version: expectedVersion, action_key: actionKey })
  });
}

export function newBookingModeActionKey() {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `scheduling-booking-mode-${suffix}`;
}
