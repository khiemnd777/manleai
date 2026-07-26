import { apiRequest } from "@/lib/api/client";
import type { SchedulingAuthority, SchedulingAuthoritySwitchResponse } from "@/types/api";

export type PreviewSchedulingAuthoritySwitchInput = {
  operation_key: string;
  source_scheduling_authority: SchedulingAuthority;
  target_scheduling_authority: SchedulingAuthority;
  expected_source_authority_version: number;
  rollback_of_switch_run_id?: string;
};

export function previewSchedulingAuthoritySwitch(salonID: string, input: PreviewSchedulingAuthoritySwitchInput) {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`/api/salons/${salonID}/scheduling-authority-switches/preview`, {
    method: "POST",
    body: JSON.stringify(input)
  });
}

export function latestSchedulingAuthoritySwitch(salonID: string) {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`/api/salons/${salonID}/scheduling-authority-switches/latest`);
}

export function getSchedulingAuthoritySwitch(salonID: string, runID: string) {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`/api/salons/${salonID}/scheduling-authority-switches/${runID}`);
}

export function commitSchedulingAuthoritySwitch(salonID: string, runID: string, actionKey: string) {
  return apiRequest<SchedulingAuthoritySwitchResponse>(`/api/salons/${salonID}/scheduling-authority-switches/${runID}/commit`, {
    method: "POST",
    body: JSON.stringify({ action_key: actionKey })
  });
}

export function newSchedulingAuthorityActionKey(prefix: "preview" | "commit") {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `settings-authority-${prefix}-${suffix}`;
}
