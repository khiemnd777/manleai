export const activeTenantSalonStorageKey = "manleai.active_tenant_salon_id";

export function storedActiveTenantSalonID() {
  if (typeof window === "undefined") return "";
  return window.localStorage.getItem(activeTenantSalonStorageKey)?.trim() ?? "";
}

export function persistActiveTenantSalonID(salonID: string) {
  if (typeof window === "undefined") return;
  if (salonID) window.localStorage.setItem(activeTenantSalonStorageKey, salonID);
  else window.localStorage.removeItem(activeTenantSalonStorageKey);
}
