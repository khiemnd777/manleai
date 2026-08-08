import { apiRequest, RequestError } from "./client";
import type {
  ManleAICalendarAggregateResponse,
  ManleAICalendarConfigInput,
  ManleAICalendarExceptionInput,
  ManleAICalendarHoursInput,
  ManleAICalendarMutationMeta,
  ManleAICalendarMutationResponse,
  ManleAICalendarResourceInput,
  ManleAICalendarResourceListResponse,
  ManleAICalendarServicePolicyInput,
  ManleAICalendarServicePolicyResponse,
  ManleAICalendarStaffProfileInput,
  ManleAICalendarStaffProfileResponse
} from "../../types/api";

const versionConflictCode = "MANLEAI_CALENDAR_CONFIG_VERSION_CONFLICT";
export type InternalCalendarSurface = "tenant" | "platform";

export function getManleAICalendar(salonID: string, surface: InternalCalendarSurface = "tenant") {
  return calendarRequest<ManleAICalendarAggregateResponse>(internalCalendarPath(salonID, surface), surface);
}

export function updateManleAICalendarConfig(salonID: string, input: ManleAICalendarConfigInput, surface: InternalCalendarSurface = "tenant") {
  return mutate(salonID, "/config", "PUT", input, surface);
}

export function replaceManleAICalendarHours(salonID: string, input: ManleAICalendarHoursInput, surface: InternalCalendarSurface = "tenant") {
  return mutate(salonID, "/hours", "PUT", input, surface);
}

export function getManleAICalendarStaffProfile(salonID: string, staffID: string, surface: InternalCalendarSurface = "tenant") {
  return calendarRequest<ManleAICalendarStaffProfileResponse>(`${internalCalendarPath(salonID, surface)}/staff/${encodeURIComponent(staffID)}`, surface);
}

export function updateManleAICalendarStaffProfile(
  salonID: string,
  staffID: string,
  input: ManleAICalendarStaffProfileInput,
  surface: InternalCalendarSurface = "tenant"
) {
  return mutate(salonID, `/staff/${encodeURIComponent(staffID)}`, "PUT", input, surface);
}

export function getManleAICalendarServicePolicy(salonID: string, serviceID: string, surface: InternalCalendarSurface = "tenant") {
  return calendarRequest<ManleAICalendarServicePolicyResponse>(`${internalCalendarPath(salonID, surface)}/services/${encodeURIComponent(serviceID)}`, surface);
}

export function updateManleAICalendarServicePolicy(
  salonID: string,
  serviceID: string,
  input: ManleAICalendarServicePolicyInput,
  surface: InternalCalendarSurface = "tenant"
) {
  return mutate(salonID, `/services/${encodeURIComponent(serviceID)}`, "PUT", input, surface);
}

export function listManleAICalendarResources(salonID: string, surface: InternalCalendarSurface = "tenant") {
  return calendarRequest<ManleAICalendarResourceListResponse>(`${internalCalendarPath(salonID, surface)}/resources`, surface);
}

export function createManleAICalendarResource(salonID: string, input: ManleAICalendarResourceInput, surface: InternalCalendarSurface = "tenant") {
  return mutate(salonID, "/resources", "POST", input, surface);
}

export function updateManleAICalendarResource(
  salonID: string,
  resourceID: string,
  input: ManleAICalendarResourceInput,
  surface: InternalCalendarSurface = "tenant"
) {
  return mutate(salonID, `/resources/${encodeURIComponent(resourceID)}`, "PUT", input, surface);
}

export function archiveManleAICalendarResource(
  salonID: string,
  resourceID: string,
  input: ManleAICalendarMutationMeta,
  surface: InternalCalendarSurface = "tenant"
) {
  return mutate(salonID, `/resources/${encodeURIComponent(resourceID)}/archive`, "POST", input, surface);
}

export function createManleAICalendarException(salonID: string, input: ManleAICalendarExceptionInput, surface: InternalCalendarSurface = "tenant") {
  return mutate(salonID, "/exceptions", "POST", input, surface);
}

export function cancelManleAICalendarException(
  salonID: string,
  exceptionID: string,
  input: ManleAICalendarMutationMeta,
  surface: InternalCalendarSurface = "tenant"
) {
  return mutate(salonID, `/exceptions/${encodeURIComponent(exceptionID)}/cancel`, "POST", input, surface);
}

export function activateManleAICalendar(salonID: string, input: ManleAICalendarMutationMeta, surface: InternalCalendarSurface = "tenant") {
  return mutate(salonID, "/activate", "POST", input, surface);
}

export function newManleAICalendarActionKey() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `manleai-calendar-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export function isManleAICalendarVersionConflict(error: unknown) {
  return error instanceof RequestError && error.status === 409 && error.code === versionConflictCode;
}

export function salonLocalDateTimeToISO(value: string, timezone: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value);
  if (!match) return null;
  const wanted = match.slice(1).map(Number);
  const utcWall = Date.UTC(wanted[0], wanted[1] - 1, wanted[2], wanted[3], wanted[4]);
  const offsets = new Set<number>();

  for (let hoursFromWall = -48; hoursFromWall <= 48; hoursFromWall += 6) {
    const sampledInstant = utcWall + hoursFromWall * 60 * 60 * 1000;
    const sampledParts = zonedParts(sampledInstant, timezone);
    if (!sampledParts) return null;
    const sampledWall = Date.UTC(
      sampledParts[0],
      sampledParts[1] - 1,
      sampledParts[2],
      sampledParts[3],
      sampledParts[4]
    );
    offsets.add(sampledWall - sampledInstant);
  }

  const matches = new Set<number>();
  for (const offset of offsets) {
    const candidate = utcWall - offset;
    const candidateParts = zonedParts(candidate, timezone);
    if (candidateParts?.every((part, index) => part === wanted[index])) {
      matches.add(candidate);
    }
  }

  if (matches.size !== 1) return null;
  return new Date([...matches][0]).toISOString();
}

export function internalCalendarPath(salonID: string, surface: InternalCalendarSurface) {
  return surface === "platform"
    ? `/api/v2/platform/tenants/${encodeURIComponent(salonID)}/scheduling/internal-calendar`
    : `/api/salons/${encodeURIComponent(salonID)}/manleai-calendar`;
}

function mutate(
  salonID: string,
  suffix: string,
  method: "POST" | "PUT",
  input: object,
  surface: InternalCalendarSurface
) {
  return calendarRequest<ManleAICalendarMutationResponse>(internalCalendarMutationPath(salonID, suffix, surface), surface, {
    method,
    body: JSON.stringify(input)
  });
}

export function internalCalendarMutationPath(salonID: string, suffix: string, surface: InternalCalendarSurface) {
  const normalizedSuffix = surface === "platform" && suffix === "/config" ? "/policy" : surface === "platform" && suffix === "/activate" ? "/activation" : suffix;
  return `${internalCalendarPath(salonID, surface)}${normalizedSuffix}`;
}

async function calendarRequest<T>(path: string, surface: InternalCalendarSurface, init: RequestInit = {}) {
  if (surface === "tenant") return apiRequest<T>(path, init);
  return (await apiRequest<{ data: T }>(path, init)).data;
}

function zonedParts(value: number, timezone: string) {
  try {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hourCycle: "h23"
    }).formatToParts(new Date(value));
    const byType = new Map<string, string>(parts.map((part) => [part.type, part.value]));
    return ["year", "month", "day", "hour", "minute"].map((key) => Number(byType.get(key)));
  } catch {
    return null;
  }
}
