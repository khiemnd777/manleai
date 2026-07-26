"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { CalendarSearch, CheckCircle2 } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  buildInternalCancelPayload,
  buildInternalRescheduleAvailabilityInput,
  buildInternalReschedulePayload,
  checkSchedulingAvailability,
  executeSchedulingAction,
  groupAvailabilitySegmentsByGuest,
  hasCompleteAggregateQuote,
  hasCompleteInternalLifecyclePlan,
  hasDurableInternalCancelConfirmation,
  hasDurableInternalRescheduleConfirmation,
  internalLifecycleCutoffState,
  schedulingActionConflict,
  schedulingOperationForPayload,
  shouldRetainSchedulingReplayProof
} from "@/lib/api/scheduling-actions";
import { RequestError } from "@/lib/api/client";
import type { AvailabilityResult, AvailabilitySegment, AvailabilitySlot, AppointmentRecord } from "@/types/api";

type InternalLifecycleMode = "reschedule" | "cancel";

type InternalAppointmentLifecycleProps = {
  salonID: string;
  timezone: string;
  appointment: AppointmentRecord;
  mode: InternalLifecycleMode;
  cutoffMinutes: number | null | undefined;
  onClose: () => void;
  onConfirmed: (appointmentID: string, mode: InternalLifecycleMode, version: number) => Promise<void>;
  onConflict: (message: string) => Promise<void>;
  onBusyChange?: (busy: boolean) => void;
};

const lifecycleAvailabilityLimit = 5;

export function InternalAppointmentLifecycle({
  salonID,
  timezone,
  appointment,
  mode,
  cutoffMinutes,
  onClose,
  onConfirmed,
  onConflict,
  onBusyChange
}: InternalAppointmentLifecycleProps) {
  const operationRef = useRef<{ key: string; fingerprint: string } | null>(null);
  const availabilityRequestRef = useRef(0);
  const [preferredDate, setPreferredDate] = useState(() => salonDateInput(new Date(appointment.start_time), timezone));
  const [availability, setAvailability] = useState<AvailabilityResult | null>(null);
  const [expectedVersion, setExpectedVersion] = useState(appointment.authority_appointment_version ?? 0);
  const [selectedFingerprint, setSelectedFingerprint] = useState("");
  const [reason, setReason] = useState("");
  const [checking, setChecking] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [submissionUncertain, setSubmissionUncertain] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState<{ appointmentID: string; version: number } | null>(null);

  const completeTarget = hasCompleteInternalLifecyclePlan(appointment);
  const selectedSlot = useMemo(
    () => availability?.slots.find((slot) => slot.fingerprint === selectedFingerprint) ?? null,
    [availability, selectedFingerprint]
  );
  const cutoff = lifecycleCutoffPresentation(appointment.start_time, cutoffMinutes, timezone);

  useEffect(() => {
    onBusyChange?.(checking || submitting || submissionUncertain);
    return () => onBusyChange?.(false);
  }, [checking, onBusyChange, submissionUncertain, submitting]);

  useEffect(() => {
    if (!availability?.expires_at || shouldRetainSchedulingReplayProof(submissionUncertain)) return;
    const expiresAt = new Date(availability.expires_at).getTime();
    if (!Number.isFinite(expiresAt)) return;
    const timer = window.setTimeout(() => {
      clearAvailabilityProof();
      operationRef.current = null;
      setError("This verified reschedule plan expired. Check availability again before submitting.");
    }, Math.max(0, expiresAt - Date.now()));
    return () => window.clearTimeout(timer);
  }, [availability?.expires_at, submissionUncertain]);

  function clearAvailabilityProof() {
    availabilityRequestRef.current += 1;
    setAvailability(null);
    setSelectedFingerprint("");
    setExpectedVersion(appointment.authority_appointment_version ?? 0);
  }

  function changeDate(value: string) {
    operationRef.current = null;
    setSubmissionUncertain(false);
    clearAvailabilityProof();
    setPreferredDate(value);
    setError("");
  }

  async function checkAvailability() {
    if (mode !== "reschedule" || !completeTarget || !cutoff.open || !preferredDate) return;
    const input = buildInternalRescheduleAvailabilityInput(appointment, preferredDate, lifecycleAvailabilityLimit);
    if (!input) {
      setError("The current appointment does not contain one complete active guest, service, staff, time, and resource plan.");
      return;
    }
    const requestID = ++availabilityRequestRef.current;
    operationRef.current = null;
    setSubmissionUncertain(false);
    setChecking(true);
    setError("");
    setAvailability(null);
    setSelectedFingerprint("");
    try {
      const response = await checkSchedulingAvailability(salonID, input);
      if (requestID !== availabilityRequestRef.current) return;
      const verified = response.kind === "verified_slots" ? response.verified_slots : null;
      const returnedVersion = response.kind === "verified_slots" ? response.target_authority_appointment_version ?? 0 : 0;
      const nestedTargetVersion = verified?.target_authority_appointment_version ?? 0;
      if (
        response.kind !== "verified_slots"
        || response.scheduling_authority !== "manleai_calendar"
        || !verified?.quote_id
        || returnedVersion !== appointment.authority_appointment_version
        || nestedTargetVersion !== returnedVersion
        || verified.slots.some((slot) => !hasCompleteAggregateQuote(slot, appointment.party_size ?? 0))
      ) {
        clearAvailabilityProof();
        setError("The backend did not return a complete target-versioned aggregate plan. Reload the appointment before retrying.");
        return;
      }
      setExpectedVersion(returnedVersion);
      setAvailability(verified);
    } catch (failure) {
      if (requestID !== availabilityRequestRef.current) return;
      const conflict = schedulingActionConflict(failure);
      if (conflict !== "unknown") {
        clearAvailabilityProof();
        await onConflict(lifecycleConflictMessage(conflict));
        onClose();
        return;
      }
      setError(failure instanceof Error ? failure.message : "Could not load verified reschedule plans.");
    } finally {
      if (requestID === availabilityRequestRef.current) setChecking(false);
    }
  }

  async function submitReschedule() {
    if (!selectedSlot || !availability?.quote_id || (!cutoff.open && !submissionUncertain)) return;
    const logicalPayload = buildInternalReschedulePayload({
      appointment,
      expectedVersion,
      availabilityQuoteID: availability.quote_id,
      slot: selectedSlot,
      timezone
    });
    if (!logicalPayload) {
      clearAvailabilityProof();
      setError("The verified plan is missing exact target, version, guest, staff, time, or resource evidence. Check availability again.");
      return;
    }
    operationRef.current = schedulingOperationForPayload(operationRef.current, logicalPayload);
    setSubmitting(true);
    setError("");
    try {
      const response = await executeSchedulingAction(salonID, { ...logicalPayload, operation_key: operationRef.current.key });
      if (!hasDurableInternalRescheduleConfirmation(response, appointment, selectedSlot)) {
        setSubmissionUncertain(true);
        setError("The response did not prove the durable rescheduled root, new version, and exact replacement children. Retry this exact operation to recover its result.");
        return;
      }
      const version = response.authority_appointment_version as number;
      setSubmissionUncertain(false);
      setSuccess({ appointmentID: appointment.id, version });
      await onConfirmed(appointment.id, "reschedule", version);
    } catch (failure) {
      await handleActionFailure(failure);
    } finally {
      setSubmitting(false);
    }
  }

  async function submitCancel() {
    if (!completeTarget || (!cutoff.open && !submissionUncertain)) return;
    const logicalPayload = buildInternalCancelPayload({
      appointment,
      expectedVersion: appointment.authority_appointment_version ?? 0,
      reason
    });
    if (!logicalPayload) {
      setError("The current root appointment does not contain complete target-origin lifecycle evidence.");
      return;
    }
    operationRef.current = schedulingOperationForPayload(operationRef.current, logicalPayload);
    setSubmitting(true);
    setError("");
    try {
      const response = await executeSchedulingAction(salonID, { ...logicalPayload, operation_key: operationRef.current.key });
      if (!hasDurableInternalCancelConfirmation(response, appointment)) {
        setSubmissionUncertain(true);
        setError("The response did not prove a durable cancelled root, advanced version, and zero active children. Retry this exact operation to recover its result.");
        return;
      }
      const version = response.authority_appointment_version as number;
      setSubmissionUncertain(false);
      setSuccess({ appointmentID: appointment.id, version });
      await onConfirmed(appointment.id, "cancel", version);
    } catch (failure) {
      await handleActionFailure(failure);
    } finally {
      setSubmitting(false);
    }
  }

  async function handleActionFailure(failure: unknown) {
    const conflict = schedulingActionConflict(failure);
    if (conflict !== "unknown") {
      operationRef.current = null;
      setSubmissionUncertain(false);
      clearAvailabilityProof();
      await onConflict(lifecycleConflictMessage(conflict));
      onClose();
      return;
    }
    if (failure instanceof RequestError) {
      setSubmissionUncertain(false);
      setError(failure.message || "The lifecycle action was rejected. No appointment state was inferred from this response.");
      return;
    }
    setSubmissionUncertain(true);
    setError("The response was lost, so the lifecycle result is unknown. Keep this dialog open and retry the exact operation with the same operation key.");
  }

  if (!completeTarget) {
    return (
      <div>
        <Alert title="Lifecycle evidence unavailable" message="This internal appointment does not expose one complete current active plan. Reload before rescheduling or cancelling it." />
        <Button type="button" variant="secondary" className="mt-5 w-full sm:w-auto" onClick={onClose}>Close</Button>
      </div>
    );
  }

  if (success) {
    return (
      <div>
        <div className="rounded-md border border-emerald-200 bg-emerald-50 p-5">
          <div className="flex items-start gap-3">
            <CheckCircle2 className="mt-0.5 h-5 w-5 flex-none text-emerald-700" />
            <div>
              <div className="font-semibold text-emerald-950">Appointment {mode === "reschedule" ? "rescheduled" : "cancelled"}</div>
              <div className="mt-1 text-sm leading-6 text-emerald-900">
                Durable root <span className="font-semibold">{success.appointmentID}</span> advanced to version {success.version}.
                {mode === "cancel" ? " The backend also proved zero active children." : " The returned children exactly match the selected aggregate plan."}
              </div>
            </div>
          </div>
        </div>
        <Button type="button" className="mt-5 w-full sm:w-auto" onClick={onClose}>Done</Button>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      {error ? <Alert title={`${mode === "reschedule" ? "Reschedule" : "Cancellation"} not completed`} message={error} /> : null}
      {submissionUncertain ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
          The outcome is unknown. Appointment fields, quote proof, reason, target version, and operation key are locked until the exact retry returns durable evidence.
        </div>
      ) : null}

      <CurrentAppointmentPlan appointment={appointment} timezone={timezone} />

      <div className={`rounded-md border p-4 text-sm leading-6 ${cutoff.open ? "border-line bg-slate-50 text-muted" : "border-amber-200 bg-amber-50 text-amber-900"}`}>
        {cutoff.message}
      </div>

      {mode === "reschedule" ? (
        <>
          <section>
            <div className="text-sm font-semibold text-ink">1. Choose a new date</div>
            <div className="mt-3 flex flex-col gap-3 sm:flex-row sm:items-end">
              <label className="block flex-1">
                <span className="text-sm font-medium text-ink">Preferred date</span>
                <input type="date" className={inputClassName} value={preferredDate} onChange={(event) => changeDate(event.target.value)} disabled={checking || submitting || submissionUncertain || !cutoff.open} />
              </label>
              <Button type="button" onClick={() => void checkAvailability()} disabled={!preferredDate || checking || submitting || submissionUncertain || !cutoff.open}>
                <CalendarSearch className="h-4 w-4" /> {checking ? "Checking..." : "Check exact availability"}
              </Button>
            </div>
          </section>

          <section>
            <div className="text-sm font-semibold text-ink">2. Select the complete replacement plan</div>
            {checking ? (
              <div className="mt-3 space-y-3"><Skeleton className="h-28" /><Skeleton className="h-28" /></div>
            ) : !availability ? (
              <div className="mt-3 rounded-md border border-line bg-slate-50 p-4 text-sm leading-6 text-muted">Choose a date and request target-aware availability. No staff, time, or resource assignment is inferred in the browser.</div>
            ) : availability.slots.length === 0 ? (
              <div className="mt-3 rounded-md border border-line p-5 text-sm leading-6 text-muted">No all-or-none replacement plan is available for the immutable guest and service shape on this date.</div>
            ) : (
              <div className="mt-3 space-y-3">
                {availability.slots.map((slot) => {
                  const selected = slot.fingerprint === selectedFingerprint;
                  return (
                    <button
                      key={slot.fingerprint || `${slot.start_time}-${slot.end_time}`}
                      type="button"
                      className={`w-full rounded-md border p-4 text-left transition ${selected ? "border-brand bg-teal-50" : "border-line bg-white hover:bg-slate-50"}`}
                      onClick={() => { operationRef.current = null; setSelectedFingerprint(slot.fingerprint || ""); setError(""); }}
                      disabled={submitting || submissionUncertain}
                    >
                      <span className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
                        <span>
                          <span className="block text-sm font-semibold text-ink">{formatDateTimeRange(slot.start_time, slot.end_time, timezone)}</span>
                          <span className="mt-1 block text-xs leading-5 text-muted">{slot.segments?.length ?? 0} concrete service unit(s), including exact staff and resource allocations</span>
                        </span>
                        <Badge value={selected ? "selected" : "available"} />
                      </span>
                      <QuotedPlan segments={slot.segments ?? []} timezone={timezone} />
                    </button>
                  );
                })}
              </div>
            )}
          </section>

          {selectedSlot ? (
            <section className="rounded-md border border-line p-4">
              <div className="text-sm font-semibold text-ink">3. Review current → new</div>
              <div className="mt-3 grid gap-4 lg:grid-cols-2">
                <PlanComparison label="Current" value={formatDateTimeRange(appointment.start_time, appointment.end_time, timezone)} />
                <PlanComparison label="New" value={formatDateTimeRange(selectedSlot.start_time, selectedSlot.end_time, timezone)} />
              </div>
            </section>
          ) : null}

          <div className="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="secondary" onClick={onClose} disabled={submitting || submissionUncertain}>Close</Button>
            <Button type="button" onClick={() => void submitReschedule()} disabled={!selectedSlot || submitting || (!cutoff.open && !submissionUncertain)}>
              {submitting ? "Rescheduling..." : submissionUncertain ? "Retry exact reschedule" : "Confirm whole-party reschedule"}
            </Button>
          </div>
        </>
      ) : (
        <>
          <label className="block">
            <span className="text-sm font-medium text-ink">Cancellation reason (optional)</span>
            <textarea className={textareaClassName} value={reason} onChange={(event) => { operationRef.current = null; setReason(event.target.value); }} disabled={submitting || submissionUncertain} placeholder="Customer requested cancellation" />
          </label>
          <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
            This cancels the whole durable root and releases every active child together. It does not delete appointment or lifecycle history.
          </div>
          <div className="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="secondary" onClick={onClose} disabled={submitting || submissionUncertain}>Close</Button>
            <Button type="button" variant="danger" onClick={() => void submitCancel()} disabled={submitting || (!cutoff.open && !submissionUncertain)}>
              {submitting ? "Cancelling..." : submissionUncertain ? "Retry exact cancellation" : "Cancel whole appointment"}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

function CurrentAppointmentPlan({ appointment, timezone }: { appointment: AppointmentRecord; timezone: string }) {
  const segments = [...(appointment.segments ?? [])].sort((left, right) => left.sort_order - right.sort_order);
  return (
    <section className="rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">{appointment.customer_name}</div>
          <div className="mt-1 text-xs leading-5 text-muted">Whole root · {appointment.party_size ?? 0} guest(s) · version {appointment.authority_appointment_version ?? "-"}</div>
        </div>
        <Badge value={appointment.status} />
      </div>
      <div className="mt-3 text-sm text-ink">{formatDateTimeRange(appointment.start_time, appointment.end_time, timezone)}</div>
      <div className="mt-3 space-y-2">
        {segments.map((segment) => (
          <div key={segment.appointment_service_id || `${segment.sort_order}-${segment.service_id}`} className="rounded-md border border-line bg-white p-3 text-xs leading-5 text-muted">
            <div className="font-semibold text-ink">{segment.guest_reference || "Guest"} · {segment.service_name}</div>
            <div>{segment.staff_name || "Assigned staff"} · {formatDateTimeRange(segment.scheduled_start_time || "", segment.scheduled_end_time || "", timezone)}</div>
            <div>{resourceLabel(segment.resource_allocations)}</div>
          </div>
        ))}
      </div>
    </section>
  );
}

function QuotedPlan({ segments, timezone }: { segments: AvailabilitySegment[]; timezone: string }) {
  return (
    <span className="mt-3 block space-y-2">
      {groupAvailabilitySegmentsByGuest(segments).map((group, guestIndex) => (
        <span key={group.guestReference || `guest-${guestIndex}`} className="block rounded-md border border-line bg-white p-3 text-xs leading-5 text-muted">
          <span className="block font-semibold text-ink">{group.guestReference || "Guest"}</span>
          {group.segments.map((segment, index) => (
            <span key={`${segment.service_id}-${segment.scheduled_start_time}-${index}`} className="mt-1 block">
              {segment.service_name} · {segment.staff_name || "Assigned staff"} · {formatDateTimeRange(segment.scheduled_start_time, segment.scheduled_end_time, timezone)} · {resourceLabel(segment.resource_allocations)}
            </span>
          ))}
        </span>
      ))}
    </span>
  );
}

function PlanComparison({ label, value }: { label: string; value: string }) {
  return <div className="rounded-md bg-slate-50 p-3"><div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div><div className="mt-1 text-sm text-ink">{value}</div></div>;
}

function resourceLabel(resources: Array<{ resource_name: string; units_allocated: number }> | undefined) {
  if (!resources?.length) return "No pooled resource allocation";
  return resources.map((resource) => `${resource.resource_name} × ${resource.units_allocated}`).join(", ");
}

function lifecycleCutoffPresentation(startTime: string, cutoffMinutes: number | null | undefined, timezone: string) {
  const state = internalLifecycleCutoffState(startTime, cutoffMinutes);
  if (state.kind === "disabled") {
    return {
      open: false,
      message: cutoffMinutes === null
        ? "Automated lifecycle action is disabled because no cutoff value is configured for this action."
        : "Automated lifecycle action is disabled because cutoff configuration evidence is unavailable in this view."
    };
  }
  if (state.kind === "invalid") {
    return { open: false, message: "The appointment cutoff could not be verified. Reload the target and calendar configuration before acting." };
  }
  return {
    open: state.open,
    message: state.open
      ? `This action remains open until ${formatDateTime(new Date(state.closesAt).toISOString(), timezone)} (${cutoffMinutes} minutes before start).`
      : `This action closed at ${formatDateTime(new Date(state.closesAt).toISOString(), timezone)} (${cutoffMinutes} minutes before start).`
  };
}

function lifecycleConflictMessage(conflict: ReturnType<typeof schedulingActionConflict>) {
  if (conflict === "quote_stale") return "Availability or the target plan changed. The appointment was reloaded and stale quote proof was cleared.";
  if (conflict === "capability_changed") return "The lifecycle window or internal configuration changed. The appointment was reloaded before another action.";
  if (conflict === "operation_conflict") return "The appointment version or operation data changed. The target was reloaded and the previous operation proof was cleared.";
  if (conflict === "target_missing") return "The lifecycle target is no longer available for this salon. The appointment list was reloaded and local proof was cleared.";
  return "The lifecycle target changed and was reloaded.";
}

function salonDateInput(value: Date, timezone: string) {
  try {
    return new Intl.DateTimeFormat("en-CA", { timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit" }).format(value);
  } catch {
    return value.toISOString().slice(0, 10);
  }
}

function formatDateTimeRange(start: string, end: string, timezone: string) {
  if (!start || !end) return "Time evidence unavailable";
  return `${formatDateTime(start, timezone)} – ${formatTime(end, timezone)}`;
}

function formatDateTime(value: string, timezone: string) {
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime())) return "Invalid time";
  return new Intl.DateTimeFormat("en-US", { timeZone: timezone, month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit" }).format(parsed);
}

function formatTime(value: string, timezone: string) {
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime())) return "Invalid time";
  return new Intl.DateTimeFormat("en-US", { timeZone: timezone, hour: "numeric", minute: "2-digit" }).format(parsed);
}

const inputClassName = "mt-2 h-11 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-muted";
const textareaClassName = "mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-muted";
