import { apiRequest } from "@/lib/api/client";
import { platformAIRuntimePath } from "@/lib/api/ai-runtime-routes";

export type AIRuntimeState = {
  enabled: boolean;
  version: number;
};

export type AIRuntimeResponse = {
  data: AIRuntimeState;
  meta: {
    replayed: boolean;
    resource_version: number;
    permissions: {
      can_read: boolean;
      allowed_actions: string[];
    };
  };
};

export function getPlatformAIRuntime(salonID: string) {
  return apiRequest<AIRuntimeResponse>(platformAIRuntimePath(salonID));
}

export function updatePlatformAIRuntime(
  salonID: string,
  enabled: boolean,
  expectedVersion: number,
  actionKey: string
) {
  return apiRequest<AIRuntimeResponse>(platformAIRuntimePath(salonID), {
    method: "PUT",
    body: JSON.stringify({ enabled, expected_version: expectedVersion, action_key: actionKey })
  });
}
