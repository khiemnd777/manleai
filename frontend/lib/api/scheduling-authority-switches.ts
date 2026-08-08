import { apiRequest } from "@/lib/api/client";
import { platformAuthorityChangePath } from "@/lib/api/scheduling-authority-routes";
import type { SchedulingAuthority, SchedulingAuthoritySwitchResponse, SchedulingAuthoritySwitchRun } from "@/types/api";

export { platformAuthorityChangePath } from "@/lib/api/scheduling-authority-routes";

export type PreviewSchedulingAuthoritySwitchInput = {
  operation_key: string;
  source_scheduling_authority: SchedulingAuthority;
  target_scheduling_authority: SchedulingAuthority;
  expected_source_authority_version: number;
  rollback_of_switch_run_id?: string;
};
export type SchedulingAuthoritySurface = "tenant" | "platform";

export type ChangeSchedulingAuthorityInput = {
  target_scheduling_authority: SchedulingAuthority;
  expected_authority_version: number;
  action_key: string;
  rollback_of_switch_run_id?: string;
};

export type PlatformAuthorityChangeResponse = {
  data: SchedulingAuthoritySwitchRun;
  meta: {
    replayed: boolean;
    resource_version: number;
    permissions: { can_read: boolean; allowed_actions: string[] };
  };
};

export function previewSchedulingAuthoritySwitch(salonID: string, input: PreviewSchedulingAuthoritySwitchInput, surface: SchedulingAuthoritySurface = "tenant") {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`${switchPath(salonID, surface)}/preview`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export async function latestSchedulingAuthoritySwitch(salonID: string, surface: SchedulingAuthoritySurface = "tenant") {
  if (surface === "platform") {
    const response = await apiRequest<PlatformAuthorityChangeResponse>(`${platformAuthorityChangePath(salonID)}/history/latest`);
    return { scheduling_authority_switch: response.data, replayed: response.meta.replayed } as SchedulingAuthoritySwitchResponse;
  }
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

export function preparePlatformSchedulingAuthorityChange(salonID: string, input: ChangeSchedulingAuthorityInput) {
  return apiRequest<PlatformAuthorityChangeResponse>(`${platformAuthorityChangePath(salonID)}/readiness`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function changePlatformSchedulingAuthority(salonID: string, input: ChangeSchedulingAuthorityInput) {
  return apiRequest<PlatformAuthorityChangeResponse>(platformAuthorityChangePath(salonID), {
    method: "PUT",
    body: JSON.stringify(input)
  });
}

function switchPath(salonID: string, surface: SchedulingAuthoritySurface) {
  const encoded = encodeURIComponent(salonID);
  return surface === "platform"
    ? `${platformAuthorityChangePath(salonID)}/history`
    : `/api/salons/${encoded}/scheduling-authority-switches`;
}

export function newSchedulingAuthorityActionKey(prefix: "preview" | "commit" | "change") {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `settings-authority-${prefix}-${suffix}`;
}
