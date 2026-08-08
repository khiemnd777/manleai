export function platformAIRuntimePath(salonID: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(salonID)}/ai-receptionist/runtime`;
}

