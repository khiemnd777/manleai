import { apiRequest, RequestError } from "@/lib/api/client";
import type {
  AvailabilitySegment,
  AvailabilitySlot,
  AppointmentRecord,
  SchedulingActionInput,
  SchedulingActionResponse,
  SchedulingAvailabilityResponse,
  SchedulingConfirmedAppointment
} from "@/types/api";

type StaffOnlyCreatePayloadInput = {
  availabilityQuoteID: string;
  slot: AvailabilitySlot;
  serviceID: string;
  staffSelectionMode: "specific" | "anyone";
  customerName: string;
  customerPhone: string;
  customerEmail?: string;
  timezone: string;
  notes?: string;
};

export type AggregateCreatePayloadInput = {
  availabilityQuoteID: string;
  slot: AvailabilitySlot;
  partySize: number;
  customerName: string;
  customerPhone: string;
  customerEmail?: string;
  timezone: string;
  notes?: string;
};

export type AvailabilityGuestGroup = {
  guestReference: string;
  segments: AvailabilitySegment[];
};

export type SchedulingActionConflict =
  | "quote_stale"
  | "capability_changed"
  | "operation_conflict"
  | "target_missing"
  | "unknown";

export type SchedulingOperationIdentity = {
  key: string;
  fingerprint: string;
};

export type InternalLifecycleCutoffState =
  | { kind: "disabled"; open: false }
  | { kind: "invalid"; open: false }
  | { kind: "closed"; open: false; closesAt: number }
  | { kind: "open"; open: true; closesAt: number };

type SchedulingAvailabilityOrigin =
  | { target_appointment_id: string; retry_of_attempt_id?: never }
  | { retry_of_attempt_id: string; target_appointment_id?: never }
  | { target_appointment_id?: never; retry_of_attempt_id?: never };

export type SchedulingAvailabilityInput = SchedulingAvailabilityOrigin & {
  service_id?: string;
  staff_id?: string;
  staff_selection_mode?: "specific" | "anyone";
  segments: Array<{
    service_id: string;
    staff_id?: string;
    staff_selection_mode: "specific" | "anyone";
    guest_reference?: string;
    quantity: number;
  }>;
  party_size: number;
  preferred_date: string;
  limit: number;
};

type InternalLifecyclePayloadInput = {
  appointment: AppointmentRecord;
  expectedVersion: number;
};

type InternalReschedulePayloadInput = InternalLifecyclePayloadInput & {
  availabilityQuoteID: string;
  slot: AvailabilitySlot;
  timezone: string;
};

export function checkSchedulingAvailability(
  salonID: string,
  input: SchedulingAvailabilityInput
) {
  return apiRequest<SchedulingAvailabilityResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-availability`,
    { method: "POST", body: JSON.stringify(input) }
  );
}

export function executeSchedulingAction(salonID: string, input: SchedulingActionInput) {
  return apiRequest<SchedulingActionResponse>(
    `/api/salons/${encodeURIComponent(salonID)}/scheduling-actions`,
    { method: "POST", body: JSON.stringify(input) }
  );
}

export function buildStaffOnlyCreatePayload(input: StaffOnlyCreatePayloadInput): Omit<SchedulingActionInput, "operation_key"> | null {
  const staffID = input.slot.staff_id || input.slot.segments?.[0]?.staff_id || "";
  if (!staffID || !input.slot.fingerprint || !input.availabilityQuoteID) return null;
  return {
    operation_type: "book",
    availability_quote_id: input.availabilityQuoteID,
    slot_fingerprint: input.slot.fingerprint,
    customer_name: input.customerName,
    customer_phone: input.customerPhone,
    customer_email: input.customerEmail || undefined,
    segments: [{
      service_id: input.serviceID,
      staff_id: staffID,
      staff_selection_mode: input.staffSelectionMode,
      quantity: 1,
      requested_start_time: input.slot.start_time,
      requested_end_time: input.slot.end_time
    }],
    requested_start_time: input.slot.start_time,
    requested_end_time: input.slot.end_time,
    requested_timezone: input.timezone,
    party_size: 1,
    notes: input.notes || undefined
  };
}

export function buildAggregateCreatePayload(input: AggregateCreatePayloadInput): Omit<SchedulingActionInput, "operation_key"> | null {
  if (!input.availabilityQuoteID || !input.slot.fingerprint || input.partySize < 1 || !input.slot.start_time || !input.slot.end_time) return null;
  const segments = schedulingSegmentsFromQuote(input.slot, input.partySize);
  if (!segments) return null;
  return {
    operation_type: "book",
    availability_quote_id: input.availabilityQuoteID,
    slot_fingerprint: input.slot.fingerprint,
    customer_name: input.customerName,
    customer_phone: input.customerPhone,
    customer_email: input.customerEmail || undefined,
    segments,
    requested_start_time: input.slot.start_time,
    requested_end_time: input.slot.end_time,
    requested_timezone: input.timezone,
    party_size: input.partySize,
    notes: input.notes || undefined
  };
}

export function buildInternalRescheduleAvailabilityInput(
  appointment: AppointmentRecord,
  preferredDate: string,
  limit: number
): SchedulingAvailabilityInput | null {
  const plan = internalLifecyclePlan(appointment);
  if (!plan || !preferredDate || limit < 1) return null;
  return {
    target_appointment_id: appointment.id,
    segments: plan.segments.map((segment) => ({
      service_id: segment.service_id,
      staff_id: segment.staff_selection_mode === "specific" ? segment.staff_id : undefined,
      staff_selection_mode: segment.staff_selection_mode,
      guest_reference: segment.guest_reference || undefined,
      quantity: 1
    })),
    party_size: plan.partySize,
    preferred_date: preferredDate,
    limit
  };
}

export function buildInternalReschedulePayload(
  input: InternalReschedulePayloadInput
): Omit<SchedulingActionInput, "operation_key"> | null {
  const plan = internalLifecyclePlan(input.appointment);
  if (!plan || input.expectedVersion !== plan.version || !input.availabilityQuoteID || !input.timezone || !input.slot.fingerprint) return null;
  const segments = schedulingSegmentsFromQuote(input.slot, plan.partySize);
  if (!segments) return null;
  return {
    operation_type: "reschedule",
    availability_quote_id: input.availabilityQuoteID,
    slot_fingerprint: input.slot.fingerprint,
    customer_name: input.appointment.customer_name,
    customer_phone: input.appointment.customer_phone,
    customer_email: input.appointment.customer_email || undefined,
    segments,
    requested_start_time: input.slot.start_time,
    requested_end_time: input.slot.end_time,
    requested_timezone: input.timezone,
    party_size: plan.partySize,
    notes: input.appointment.notes || undefined,
    target_appointment_id: input.appointment.id,
    target_scheduling_authority: "manleai_calendar",
    expected_target_authority_appointment_version: input.expectedVersion
  };
}

export function buildInternalCancelPayload(
  input: InternalLifecyclePayloadInput & { reason?: string }
): Omit<SchedulingActionInput, "operation_key"> | null {
  const plan = internalLifecyclePlan(input.appointment);
  if (!plan || input.expectedVersion !== plan.version) return null;
  return {
    operation_type: "cancel",
    notes: input.reason?.trim() || undefined,
    target_appointment_id: input.appointment.id,
    target_scheduling_authority: "manleai_calendar",
    expected_target_authority_appointment_version: input.expectedVersion
  };
}

export function hasCompleteInternalLifecyclePlan(appointment: AppointmentRecord) {
  return internalLifecyclePlan(appointment) !== null;
}

export function hasDurableInternalRescheduleConfirmation(
  response: SchedulingActionResponse,
  target: AppointmentRecord,
  quotedSlot: AvailabilitySlot
) {
  const plan = internalLifecyclePlan(target);
  const confirmed = response.kind === "confirmed_appointment" ? response.confirmed_appointment : null;
  if (!plan || !confirmed || response.operation_type !== "reschedule" || response.scheduling_authority !== "manleai_calendar") return false;
  const quotedChildCount = quotedSlot.segments?.length ?? 0;
  return response.target_authority_appointment_version === plan.version
    && response.authority_appointment_version === plan.version + 1
    && confirmed.appointment_id === target.id
    && Boolean(confirmed.booking_attempt_id?.trim())
    && confirmed.appointment_status === "rescheduled"
    && confirmed.active_child_count === quotedChildCount
    && quotedChildCount > 0
    && hasDurableAggregateConfirmation(confirmed, quotedSlot);
}

export function hasDurableInternalCancelConfirmation(
  response: SchedulingActionResponse,
  target: AppointmentRecord
) {
  const plan = internalLifecyclePlan(target);
  const confirmed = response.kind === "confirmed_appointment" ? response.confirmed_appointment : null;
  if (!plan || !confirmed || response.operation_type !== "cancel" || response.scheduling_authority !== "manleai_calendar") return false;
  return response.target_authority_appointment_version === plan.version
    && response.authority_appointment_version === plan.version + 1
    && confirmed.appointment_id === target.id
    && Boolean(confirmed.booking_attempt_id?.trim())
    && confirmed.appointment_status === "cancelled"
    && confirmed.active_child_count === 0;
}

export function hasCompleteAggregateQuote(slot: AvailabilitySlot, partySize: number) {
  return Boolean(slot.fingerprint && slot.start_time && slot.end_time && schedulingSegmentsFromQuote(slot, partySize));
}

export function hasDurableAggregateConfirmation(confirmed: SchedulingConfirmedAppointment, quotedSlot: AvailabilitySlot) {
  const quoted = quotedSlot.segments ?? [];
  const children = confirmed.children ?? [];
  if (typeof confirmed.appointment_id !== "string" || !confirmed.appointment_id.trim() || quoted.length === 0 || children.length !== quoted.length) return false;
  return children.every((child, index) => {
    const segment = quoted[index];
    return typeof child.appointment_service_id === "string"
      && Boolean(child.appointment_service_id.trim())
      && child.guest_reference === segment.guest_reference
      && child.service_id === segment.service_id
      && child.staff_id === segment.staff_id
      && child.staff_selection_mode === segment.staff_selection_mode
      && child.quantity === segment.quantity
      && child.scheduled_start_time === segment.scheduled_start_time
      && child.scheduled_end_time === segment.scheduled_end_time
      && child.occupied_start_time === segment.occupied_start_time
      && child.occupied_end_time === segment.occupied_end_time
      && child.buffer_before_minutes === segment.buffer_before_minutes
      && child.buffer_after_minutes === segment.buffer_after_minutes
      && resourceEvidenceMatches(child.resource_allocations, segment.resource_allocations);
  });
}

function resourceEvidenceMatches(
  left: Array<{ resource_pool_id: string; resource_name: string; units_allocated: number }> | undefined,
  right: Array<{ resource_pool_id: string; resource_name: string; units_allocated: number }> | undefined
) {
  if (!Array.isArray(left) || !Array.isArray(right)) return false;
  if (left.length !== right.length) return false;
  const byPool = (items: typeof left) => [...items].sort((a, b) => a.resource_pool_id.localeCompare(b.resource_pool_id));
  const sortedLeft = byPool(left);
  const sortedRight = byPool(right);
  return sortedLeft.every((item, index) => {
    const other = sortedRight[index];
    return item.resource_pool_id === other.resource_pool_id
      && item.resource_name === other.resource_name
      && item.units_allocated === other.units_allocated;
  });
}

function schedulingSegmentsFromQuote(slot: AvailabilitySlot, partySize: number) {
  if (partySize < 1) return null;
  const quotedSegments = slot.segments ?? [];
  if (quotedSegments.length === 0) return null;
  const guestReferences = new Set<string>();
  const segments = quotedSegments.map((segment) => {
    const guestReference = segment.guest_reference?.trim() || "";
    if (guestReference) guestReferences.add(guestReference);
    if (
      !segment.service_id ||
      !segment.staff_id ||
      (segment.staff_selection_mode !== "specific" && segment.staff_selection_mode !== "anyone") ||
      segment.quantity !== 1 ||
      (partySize > 1 && !guestReference) ||
      !segment.scheduled_start_time ||
      !segment.scheduled_end_time
    ) return null;
    return {
      service_id: segment.service_id,
      staff_id: segment.staff_id,
      staff_selection_mode: segment.staff_selection_mode,
      guest_reference: guestReference || undefined,
      quantity: segment.quantity,
      requested_start_time: segment.scheduled_start_time,
      requested_end_time: segment.scheduled_end_time
    };
  });
  if (segments.some((segment) => segment === null) || (partySize > 1 && guestReferences.size !== partySize)) return null;
  return segments as NonNullable<(typeof segments)[number]>[];
}

function internalLifecyclePlan(appointment: AppointmentRecord) {
  const version = appointment.authority_appointment_version ?? 0;
  const partySize = appointment.party_size ?? 0;
  const rootStart = dateEvidence(appointment.start_time);
  const rootEnd = dateEvidence(appointment.end_time);
  if (
    appointment.scheduling_authority !== "manleai_calendar"
    || (appointment.status !== "confirmed" && appointment.status !== "rescheduled")
    || !appointment.id
    || appointment.authority_appointment_id !== appointment.id
    || !Number.isInteger(version)
    || version < 1
    || !Number.isInteger(partySize)
    || partySize < 1
    || rootStart === null
    || rootEnd === null
    || rootEnd <= rootStart
  ) return null;

  const segments = [...(appointment.segments ?? [])].sort((left, right) => left.sort_order - right.sort_order);
  if (segments.length === 0) return null;
  const guestReferences: string[] = [];
  let firstScheduledStart = Number.POSITIVE_INFINITY;
  let lastScheduledEnd = Number.NEGATIVE_INFINITY;
  for (const [index, segment] of segments.entries()) {
    const staffMode = segment.staff_selection_mode;
    const resources = segment.resource_allocations ?? [];
    const scheduledStart = dateEvidence(segment.scheduled_start_time);
    const scheduledEnd = dateEvidence(segment.scheduled_end_time);
    const occupiedStart = dateEvidence(segment.occupied_start_time);
    const occupiedEnd = dateEvidence(segment.occupied_end_time);
    if (
      !segment.appointment_service_id?.trim()
      || segment.scheduling_authority !== "manleai_calendar"
      || !segment.service_id?.trim()
      || !segment.staff_id?.trim()
      || (staffMode !== "specific" && staffMode !== "anyone")
      || segment.quantity !== 1
      || segment.plan_version !== version
      || segment.sort_order !== index + 1
      || scheduledStart === null
      || scheduledEnd === null
      || occupiedStart === null
      || occupiedEnd === null
      || scheduledEnd <= scheduledStart
      || occupiedEnd <= occupiedStart
      || occupiedStart > scheduledStart
      || occupiedEnd < scheduledEnd
      || !Number.isInteger(segment.buffer_before_minutes)
      || (segment.buffer_before_minutes ?? -1) < 0
      || !Number.isInteger(segment.buffer_after_minutes)
      || (segment.buffer_after_minutes ?? -1) < 0
      || resources.some((resource) => !resource.resource_pool_id?.trim() || !resource.resource_name?.trim() || !Number.isInteger(resource.units_allocated) || resource.units_allocated < 1)
    ) return null;
    firstScheduledStart = Math.min(firstScheduledStart, scheduledStart);
    lastScheduledEnd = Math.max(lastScheduledEnd, scheduledEnd);
    guestReferences.push(segment.guest_reference?.trim() || "");
  }
  if (firstScheduledStart !== rootStart || lastScheduledEnd !== rootEnd) return null;
  const presentGuestReferences = guestReferences.filter(Boolean);
  if (presentGuestReferences.length > 0) {
    if (presentGuestReferences.length !== guestReferences.length || new Set(presentGuestReferences).size !== partySize) return null;
  } else if (partySize !== 1) {
    return null;
  }
  return {
    version,
    partySize,
    segments: segments.map((segment) => ({
      service_id: segment.service_id as string,
      staff_id: segment.staff_id as string,
      staff_selection_mode: segment.staff_selection_mode as "specific" | "anyone",
      guest_reference: segment.guest_reference?.trim() || ""
    }))
  };
}

function dateEvidence(value: string | undefined) {
  if (!value) return null;
  const parsed = new Date(value).getTime();
  return Number.isFinite(parsed) ? parsed : null;
}

export function groupAvailabilitySegmentsByGuest(segments: AvailabilitySegment[]): AvailabilityGuestGroup[] {
  const groups = new Map<string, AvailabilitySegment[]>();
  segments.forEach((segment) => {
    const guestReference = segment.guest_reference?.trim() || "";
    const group = groups.get(guestReference);
    if (group) group.push(segment);
    else groups.set(guestReference, [segment]);
  });
  return Array.from(groups, ([guestReference, groupedSegments]) => ({ guestReference, segments: groupedSegments }));
}

export function newSchedulingOperationKey() {
  return crypto.randomUUID();
}

export function schedulingOperationForPayload(
  current: SchedulingOperationIdentity | null,
  payload: object,
  createKey: () => string = newSchedulingOperationKey
) {
  const fingerprint = JSON.stringify(payload);
  if (current?.fingerprint === fingerprint) return current;
  return { key: createKey(), fingerprint };
}

export function shouldRetainSchedulingReplayProof(submissionUncertain: boolean) {
  return submissionUncertain;
}

export function internalLifecycleCutoffState(
  startTime: string,
  cutoffMinutes: number | null | undefined,
  now: number = Date.now()
): InternalLifecycleCutoffState {
  if (cutoffMinutes === null || cutoffMinutes === undefined) return { kind: "disabled", open: false };
  if (!Number.isInteger(cutoffMinutes) || cutoffMinutes < 0) return { kind: "invalid", open: false };
  const start = new Date(startTime).getTime();
  const closesAt = start - cutoffMinutes * 60_000;
  if (!Number.isFinite(start) || !Number.isFinite(closesAt) || !Number.isFinite(now)) return { kind: "invalid", open: false };
  return now < closesAt ? { kind: "open", open: true, closesAt } : { kind: "closed", open: false, closesAt };
}

export function schedulingActionConflict(error: unknown): SchedulingActionConflict {
  if (!(error instanceof RequestError)) return "unknown";
  if (error.status === 404 && error.code === "SCHEDULING_RESOURCE_NOT_FOUND") return "target_missing";
  if (error.status !== 409) return "unknown";
  switch (error.code) {
    case "AVAILABILITY_QUOTE_STALE":
    case "AVAILABILITY_QUOTE_REQUIRED":
      return "quote_stale";
    case "SCHEDULING_AUTHORITY_NOT_READY":
      return "capability_changed";
    case "SCHEDULING_OPERATION_CONFLICT":
      return "operation_conflict";
    default:
      return "unknown";
  }
}
