import { apiRequest, apiRequestWithResponse } from "@/lib/api/client";
import type {
  SquareWebhookEventDetailResponse,
  SquareWebhookEventsResponse,
  SquareWebhookFilterStatus
} from "@/types/api";

export const SQUARE_WEBHOOK_STATUS_FILTERS = [
  "pending",
  "processing",
  "failed",
  "dead_letter",
  "succeeded"
] as const satisfies readonly SquareWebhookFilterStatus[];

export type SquareWebhookListStatus = "" | SquareWebhookFilterStatus;

export function listSquareWebhookEvents(
  salonID: string,
  status: SquareWebhookListStatus,
  limit = 25,
  offset = 0,
  surface: "tenant" | "platform" = "tenant"
) {
  const query = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (status) query.set("status", status);
  return apiRequest<SquareWebhookEventsResponse>(
    `${squareWebhookBase(salonID, surface)}?${query.toString()}`
  );
}

export function getSquareWebhookEvent(salonID: string, webhookEventID: string, surface: "tenant" | "platform" = "tenant") {
  return apiRequest<SquareWebhookEventDetailResponse>(
    `${squareWebhookBase(salonID, surface)}/${encodeURIComponent(webhookEventID)}`
  );
}

export async function requeueSquareWebhookEvent(
  salonID: string,
  webhookEventID: string,
  actionKey: string,
  surface: "tenant" | "platform" = "tenant"
) {
  const result = await apiRequestWithResponse<SquareWebhookEventDetailResponse>(
    `${squareWebhookBase(salonID, surface)}/${encodeURIComponent(webhookEventID)}/requeue`,
    { method: "POST", body: JSON.stringify({ action_key: actionKey }) }
  );
  return {
    event: result.data.event,
    replayed: result.response.headers.get("X-Idempotent-Replay") === "true"
  };
}

function squareWebhookBase(salonID: string, surface: "tenant" | "platform") {
  return surface === "platform"
    ? `/api/platform/tenants/${encodeURIComponent(salonID)}/operations/square-webhooks`
    : `/api/salons/${encodeURIComponent(salonID)}/square-webhook-events`;
}

export function newSquareWebhookActionKey() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `square-webhook-requeue-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
