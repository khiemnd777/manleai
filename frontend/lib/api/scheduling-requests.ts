import { apiRequest } from "@/lib/api/client";
import type {
  SchedulingRequestResponse,
  SchedulingRequestsResponse,
  SchedulingRequestStatus,
  UpdateSchedulingRequestInput
} from "@/types/api";

export type SchedulingRequestListParams = {
  status?: SchedulingRequestStatus;
  limit: number;
  offset: number;
};

export function listSchedulingRequests(salonID: string, params: SchedulingRequestListParams) {
  const query = new URLSearchParams({
    limit: String(params.limit),
    offset: String(params.offset)
  });
  if (params.status) query.set("status", params.status);

  return apiRequest<SchedulingRequestsResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-requests?${query.toString()}`
  );
}

export function getSchedulingRequest(salonID: string, requestID: string) {
  return apiRequest<SchedulingRequestResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-requests/${encodeURIComponent(requestID)}`
  );
}

export function updateSchedulingRequest(
  salonID: string,
  requestID: string,
  input: UpdateSchedulingRequestInput
) {
  return apiRequest<SchedulingRequestResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-requests/${encodeURIComponent(requestID)}`,
    {
      method: "PATCH",
      body: JSON.stringify(input)
    }
  );
}

export function newSchedulingRequestActionKey() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `owner-review-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}
