import { apiRequest } from "@/lib/api/client";
import type { SchedulingAuthority, SchedulingAuthoritySwitchResponse } from "@/types/api";

export type PreviewSchedulingAuthoritySwitchInput = {
  operation_key: string;
  source_scheduling_authority: SchedulingAuthority;
  target_scheduling_authority: SchedulingAuthority;
  expected_source_authority_version: number;
  rollback_of_switch_run_id?: string;
};
export type SchedulingAuthoritySurface = "tenant" | "platform";

export function previewSchedulingAuthoritySwitch(salonID: string, input: PreviewSchedulingAuthoritySwitchInput, surface: SchedulingAuthoritySurface = "tenant") {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`${switchPath(salonID, surface)}/preview`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function latestSchedulingAuthoritySwitch(salonID: string, surface: SchedulingAuthoritySurface = "tenant") {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`${switchPath(salonID, surface)}/latest`);
}

export function getSchedulingAuthoritySwitch(salonID: string, runID: string, surface: SchedulingAuthoritySurface = "tenant") {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`${switchPath(salonID, surface)}/${runID}`);
}

export function commitSchedulingAuthoritySwitch(salonID: string, runID: string, actionKey: string, surface: SchedulingAuthoritySurface = "tenant") {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`${switchPath(salonID, surface)}/${runID}/commit`, {
    method: "POST",
    body: JSON.stringify({ action_key: actionKey })
  });
}

function switchPath(salonID: string, surface: SchedulingAuthoritySurface) {
  const encoded = encodeURIComponent(salonID);
  return surface === "platform"
    ? `/api/platform/tenants/${encoded}/technical/scheduling-authority-switches`
    : `/api/salons/${encoded}/scheduling-authority-switches`;
}

export function newSchedulingAuthorityActionKey(prefix: "preview" | "commit") {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `settings-authority-${prefix}-${suffix}`;
}
