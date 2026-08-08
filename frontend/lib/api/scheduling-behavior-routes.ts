export function platformSchedulingBehaviorPath(salonID: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(salonID)}/scheduling/behavior`;
}

export function platformBookingModePath(salonID: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(salonID)}/scheduling/booking-mode`;
}
