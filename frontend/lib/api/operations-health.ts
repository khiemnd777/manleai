import { apiRequest } from "@/lib/api/client";
import type { OperationsHealthResponse } from "@/types/api";

export function getOperationsHealth(salonID: string) {
  return apiRequest<OperationsHealthResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/operations/status`
  );
}
