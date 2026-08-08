export function platformAuthorityChangePath(salonID: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(salonID)}/scheduling/authority`;
}
