"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { CalendarSearch, Check, ChevronLeft, ChevronRight, Plus, Trash2, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  buildAggregateCreatePayload,
  checkSchedulingAvailability,
  executeSchedulingAction,
  groupAvailabilitySegmentsByGuest,
  hasCompleteAggregateQuote,
  hasDurableAggregateConfirmation,
  schedulingActionConflict,
  schedulingOperationForPayload,
  shouldRetainSchedulingReplayProof
} from "@/lib/api/scheduling-actions";
import { RequestError } from "@/lib/api/client";
import type {
  AvailabilityResult,
  AvailabilitySegment,
  AvailabilitySlot,
  ManleAICalendarAggregate,
  ManleAICalendarCapabilities,
  ManleAICalendarServicePolicy,
  ManleAICalendarStaffRef
} from "@/types/api";

type InternalAppointmentCreateProps = {
  salonID: string;
  timezone: string;
  calendar: ManleAICalendarAggregate;
  capabilityReady: boolean;
  capabilityBlockers: string[];
  onClose: () => void;
  onConfirmed: (appointmentID: string) => Promise<void>;
  onReadinessInvalidated: () => Promise<void>;
  onBusyChange?: (busy: boolean) => void;
};

type CreateStep = 1 | 2 | 3 | 4 | 5 | 6;

type CustomerForm = {
  name: string;
  phone: string;
  email: string;
  notes: string;
};

type GuestDraft = {
  id: string;
  reference: string;
  segments: SegmentDraft[];
};

type SegmentDraft = {
  id: string;
  serviceID: string;
  staffMode: "anyone" | "specific";
  staffID: string;
};

export function InternalAppointmentCreate({
  salonID,
  timezone,
  calendar,
  capabilityReady,
  capabilityBlockers,
  onClose,
  onConfirmed,
  onReadinessInvalidated,
  onBusyChange
}: InternalAppointmentCreateProps) {
  const operationRef = useRef<{ key: string; fingerprint: string } | null>(null);
  const availabilityRequestRef = useRef(0);
  const draftIDRef = useRef(2);
  const [step, setStep] = useState<CreateStep>(1);
  const [guests, setGuests] = useState<GuestDraft[]>([initialGuestDraft()]);
  const [preferredDate, setPreferredDate] = useState(() => salonDateInput(new Date(), timezone));
  const [availability, setAvailability] = useState<AvailabilityResult | null>(null);
  const [selectedSlotFingerprint, setSelectedSlotFingerprint] = useState("");
  const [customer, setCustomer] = useState<CustomerForm>({ name: "", phone: "", email: "", notes: "" });
  const [checking, setChecking] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [confirmedAppointmentID, setConfirmedAppointmentID] = useState("");
  const [submissionUncertain, setSubmissionUncertain] = useState(false);

  useEffect(() => {
    onBusyChange?.(checking || submitting || submissionUncertain);
    return () => onBusyChange?.(false);
  }, [checking, onBusyChange, submissionUncertain, submitting]);

  const capabilities = calendar.readiness.capabilities;
  const policies = useMemo(
    () => calendar.service_policies.filter((policy) => createPolicyAvailable(policy, capabilities)),
    [calendar.service_policies, capabilities]
  );
  const policiesByID = useMemo(() => new Map(policies.map((policy) => [policy.service.id, policy])), [policies]);
  const draftSegments = useMemo(
    () => guests.flatMap((guest) => guest.segments.map((segment) => ({
      service_id: segment.serviceID,
      staff_id: segment.staffMode === "specific" ? segment.staffID : undefined,
      staff_selection_mode: segment.staffMode,
      guest_reference: guest.reference.trim(),
      quantity: 1
    }))),
    [guests]
  );
  const draftCapability = useMemo(
    () => evaluateDraftCapability(guests, policiesByID, capabilities),
    [capabilities, guests, policiesByID]
  );
  const partySize = guests.length;
  const selectedSlot = availability?.slots.find((slot) => slot.fingerprint === selectedSlotFingerprint) ?? null;

  useEffect(() => {
    if (!availability?.expires_at || shouldRetainSchedulingReplayProof(submissionUncertain)) return;
    const expiry = new Date(availability.expires_at).getTime();
    if (!Number.isFinite(expiry)) return;
    const delay = Math.max(0, expiry - Date.now());
    const timer = window.setTimeout(() => {
      clearAvailabilityProof();
      setStep(2);
      setError("This verified aggregate opening expired. Check availability again before reviewing or confirming.");
    }, delay);
    return () => window.clearTimeout(timer);
  }, [availability?.expires_at, submissionUncertain]);

  function nextDraftID(prefix: "guest" | "segment") {
    const value = draftIDRef.current;
    draftIDRef.current += 1;
    return `${prefix}-${value}`;
  }

  function clearAvailabilityProof() {
    availabilityRequestRef.current += 1;
    setAvailability(null);
    setSelectedSlotFingerprint("");
  }

  function invalidateDraft() {
    operationRef.current = null;
    setSubmissionUncertain(false);
    clearAvailabilityProof();
    setError("");
  }

  function changeGuests(update: (current: GuestDraft[]) => GuestDraft[]) {
    invalidateDraft();
    setGuests(update);
  }

  function addGuest() {
    if (capabilities?.party_create !== true || guests.length >= maxPartySize(calendar)) return;
    const guestID = nextDraftID("guest");
    const segmentID = nextDraftID("segment");
    changeGuests((current) => [...current, {
      id: guestID,
      reference: `Guest ${current.length + 1}`,
      segments: [emptySegmentDraft(segmentID)]
    }]);
  }

  function removeGuest(guestID: string) {
    if (guests.length <= 1) return;
    changeGuests((current) => current.filter((guest) => guest.id !== guestID));
  }

  function updateGuestReference(guestID: string, reference: string) {
    changeGuests((current) => current.map((guest) => guest.id === guestID ? { ...guest, reference } : guest));
  }

  function addSegment(guestID: string) {
    if (capabilities?.party_create !== true) return;
    const segmentID = nextDraftID("segment");
    changeGuests((current) => current.map((guest) => guest.id === guestID
      ? { ...guest, segments: [...guest.segments, emptySegmentDraft(segmentID)] }
      : guest));
  }

  function removeSegment(guestID: string, segmentID: string) {
    changeGuests((current) => current.map((guest) => guest.id === guestID && guest.segments.length > 1
      ? { ...guest, segments: guest.segments.filter((segment) => segment.id !== segmentID) }
      : guest));
  }

  function updateSegment(guestID: string, segmentID: string, patch: Partial<SegmentDraft>) {
    changeGuests((current) => current.map((guest) => guest.id === guestID ? {
      ...guest,
      segments: guest.segments.map((segment) => segment.id === segmentID ? { ...segment, ...patch } : segment)
    } : guest));
  }

  function changePreferredDate(value: string) {
    invalidateDraft();
    setPreferredDate(value);
  }

  function changeCustomer(patch: Partial<CustomerForm>) {
    operationRef.current = null;
    setSubmissionUncertain(false);
    setCustomer((current) => ({ ...current, ...patch }));
    setError("");
  }

  async function checkAvailability() {
    const rootSegment = draftSegments[0];
    if (!capabilityReady || !draftCapability.ready || !rootSegment || !preferredDate) {
      setError(draftCapability.message || "This appointment structure is not ready for internal execution.");
      return;
    }
    const requestID = ++availabilityRequestRef.current;
    setChecking(true);
    setError("");
    setSelectedSlotFingerprint("");
    try {
      const response = await checkSchedulingAvailability(salonID, {
        service_id: rootSegment.service_id,
        staff_id: rootSegment.staff_id,
        staff_selection_mode: rootSegment.staff_selection_mode,
        segments: draftSegments,
        party_size: partySize,
        preferred_date: preferredDate,
        limit: 8
      });
      if (requestID !== availabilityRequestRef.current) return;
      if (
        response.kind !== "verified_slots" ||
        response.scheduling_authority !== "manleai_calendar" ||
        !response.verified_slots.quote_id
      ) {
        clearAvailabilityProof();
        await onReadinessInvalidated();
        setError("Internal verified availability is no longer available. Readiness was reloaded; review the current capabilities before retrying.");
        return;
      }
      const verified = response.verified_slots;
      if (verified.slots.length > 0 && !verified.slots.some((slot) => slotHasCompletePolicyProof(slot, partySize, policiesByID))) {
        clearAvailabilityProof();
        setError("The availability response did not contain a complete aggregate quote with concrete staff and segment times. No opening can be selected.");
        return;
      }
      setAvailability(verified);
      setStep(3);
    } catch (availabilityFailure) {
      if (requestID !== availabilityRequestRef.current) return;
      setAvailability(null);
      const conflict = schedulingActionConflict(availabilityFailure);
      if (conflict === "capability_changed" || conflict === "quote_stale") await onReadinessInvalidated();
      setError(availabilityFailure instanceof Error ? availabilityFailure.message : "Could not load verified internal openings.");
    } finally {
      if (requestID === availabilityRequestRef.current) setChecking(false);
    }
  }

  async function confirmAppointment() {
    if ((!draftCapability.ready && !submissionUncertain) || !availability?.quote_id || !selectedSlot?.fingerprint) return;
    if (!customer.name.trim() || !customer.phone.trim()) {
      setError("Customer name and phone are required before confirmation.");
      setStep(4);
      return;
    }
    const logicalPayload = buildAggregateCreatePayload({
      availabilityQuoteID: availability.quote_id,
      slot: selectedSlot,
      partySize,
      customerName: customer.name.trim(),
      customerPhone: customer.phone.trim(),
      customerEmail: customer.email.trim(),
      timezone,
      notes: customer.notes.trim()
    });
    if (!logicalPayload) {
      setError("The verified aggregate opening is missing concrete staff, guest, or timing evidence. Check availability again.");
      clearAvailabilityProof();
      setStep(2);
      return;
    }
    operationRef.current = schedulingOperationForPayload(operationRef.current, logicalPayload);
    const operationKey = operationRef.current.key;
    setSubmitting(true);
    setError("");
    try {
      const response = await executeSchedulingAction(salonID, { ...logicalPayload, operation_key: operationKey });
      const appointmentID = response.kind === "confirmed_appointment"
        && response.operation_type === "book"
        && response.scheduling_authority === "manleai_calendar"
        && hasDurableAggregateConfirmation(response.confirmed_appointment, selectedSlot)
        ? response.confirmed_appointment.appointment_id.trim()
        : "";
      if (!appointmentID) {
        setSubmissionUncertain(true);
        setError("The response did not include durable root appointment evidence. Keep this dialog open and retry the exact aggregate operation so the backend can prove whether the atomic commit completed.");
        return;
      }
      setSubmissionUncertain(false);
      setConfirmedAppointmentID(appointmentID);
      setStep(6);
      await onConfirmed(appointmentID);
    } catch (actionFailure) {
      const conflict = schedulingActionConflict(actionFailure);
      if (conflict !== "unknown") {
        setSubmissionUncertain(false);
        clearAvailabilityProof();
        setStep(2);
        await onReadinessInvalidated();
        setError(conflictMessage(conflict));
      } else if (actionFailure instanceof RequestError) {
        setSubmissionUncertain(false);
        setError(actionFailure.message || "Could not create the appointment. Retry keeps the same operation key.");
      } else {
        setSubmissionUncertain(true);
        setError("The response was lost, so the aggregate result is unknown. Keep this dialog open and retry the exact operation; the same operation key and complete quote will be replayed.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (!capabilityReady && !submissionUncertain) {
    return (
      <div>
        <Alert title="Internal create is not ready" message={capabilityBlockers[0] || "The backend has not enabled an internal create capability."} />
        {capabilityBlockers.length > 1 ? (
          <ul className="mt-3 space-y-1 text-sm leading-6 text-muted">
            {capabilityBlockers.slice(1).map((blocker) => <li key={blocker}>• {blocker}</li>)}
          </ul>
        ) : null}
        <Button type="button" variant="secondary" className="mt-5 w-full sm:w-auto" onClick={onClose}>Close</Button>
      </div>
    );
  }

  if (step === 6) {
    return (
      <div>
        <div className="rounded-md border border-emerald-200 bg-emerald-50 p-5">
          <div className="flex items-start gap-3">
            <Check className="mt-0.5 h-5 w-5 flex-none text-emerald-700" />
            <div>
              <div className="font-semibold text-emerald-950">Appointment confirmed</div>
              <div className="mt-1 text-sm leading-6 text-emerald-900">
                One atomic internal commit returned durable root appointment ID <span className="font-semibold">{confirmedAppointmentID}</span>.
              </div>
            </div>
          </div>
        </div>
        <Button type="button" className="mt-5 w-full sm:w-auto" onClick={onClose}>Done</Button>
      </div>
    );
  }

  return (
    <div>
      <StepHeader step={step} />
      {error ? <div className="mt-4"><Alert title="Appointment not created" message={error} /></div> : null}

      {step === 1 ? (
        <section className="mt-5">
          <SectionTitle title="Build the appointment" description="Each guest and service unit is structured from active salon policies. Their order is preserved for verified scheduling." />
          {policies.length === 0 ? (
            <div className="mt-4 rounded-md border border-line p-5 text-sm leading-6 text-muted">
              No active service policy is currently enabled by the matching create capability. Review Services and scheduling readiness.
            </div>
          ) : (
            <div className="mt-4 space-y-4">
              {guests.map((guest, guestIndex) => (
                <div key={guest.id} className="rounded-md border border-line bg-slate-50 p-4">
                  <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-end">
                    <Field label={`Guest ${guestIndex + 1} label`}>
                      <input value={guest.reference} maxLength={200} onChange={(event) => updateGuestReference(guest.id, event.target.value)} className={inputClassName} />
                    </Field>
                    {guests.length > 1 ? (
                      <Button type="button" variant="danger" onClick={() => removeGuest(guest.id)}>
                        <Trash2 className="h-4 w-4" /> Remove guest
                      </Button>
                    ) : null}
                  </div>
                  <div className="mt-4 space-y-3">
                    {guest.segments.map((segment, segmentIndex) => {
                      const policy = policiesByID.get(segment.serviceID);
                      const eligibleStaff = policy?.eligible_staff.filter(activeStaffRef) ?? [];
                      return (
                        <div key={segment.id} className="rounded-md border border-line bg-white p-4">
                          <div className="flex items-center justify-between gap-3">
                            <div className="text-sm font-semibold text-ink">Service {segmentIndex + 1}</div>
                            {guest.segments.length > 1 ? (
                              <Button type="button" variant="secondary" onClick={() => removeSegment(guest.id, segment.id)}>
                                <Trash2 className="h-4 w-4" /> Remove
                              </Button>
                            ) : null}
                          </div>
                          <div className="mt-3 grid gap-4 md:grid-cols-3">
                            <Field label="Service">
                              <select
                                value={segment.serviceID}
                                onChange={(event) => updateSegment(guest.id, segment.id, { serviceID: event.target.value, staffMode: "anyone", staffID: "" })}
                                className={inputClassName}
                              >
                                <option value="">Choose service</option>
                                {policies.map((item) => <option key={item.service.id} value={item.service.id}>{item.service.name}</option>)}
                              </select>
                            </Field>
                            <Field label="Staff preference">
                              <select
                                value={segment.staffMode}
                                onChange={(event) => updateSegment(guest.id, segment.id, { staffMode: event.target.value as SegmentDraft["staffMode"], staffID: "" })}
                                className={inputClassName}
                              >
                                <option value="anyone">Anyone available</option>
                                <option value="specific">Specific staff member</option>
                              </select>
                            </Field>
                            {segment.staffMode === "specific" ? (
                              <Field label="Staff member">
                                <select value={segment.staffID} onChange={(event) => updateSegment(guest.id, segment.id, { staffID: event.target.value })} className={inputClassName}>
                                  <option value="">Choose eligible staff</option>
                                  {eligibleStaff.map((member) => <option key={member.id} value={member.id}>{member.name}</option>)}
                                </select>
                              </Field>
                            ) : (
                              <div className="rounded-md border border-line bg-slate-50 p-3 text-xs leading-5 text-muted">
                                The verified quote will return one concrete assigned staff member.
                              </div>
                            )}
                          </div>
                          {policy ? (
                            <div className="mt-3 flex flex-wrap gap-2 text-xs text-muted">
                              <Badge value={policy.capacity_mode === "pooled" ? "pooled" : "staff-only"} />
                              <span>{policy.service.duration_minutes} minutes</span>
                              {policy.capacity_mode === "pooled" ? <span>{policy.resource_requirements.length} resource requirement(s)</span> : null}
                            </div>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                  <Button type="button" variant="secondary" className="mt-3 w-full sm:w-auto" onClick={() => addSegment(guest.id)} disabled={capabilities?.party_create !== true}>
                    <Plus className="h-4 w-4" /> Add service for this guest
                  </Button>
                </div>
              ))}
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <Button type="button" variant="secondary" onClick={addGuest} disabled={capabilities?.party_create !== true || guests.length >= maxPartySize(calendar)}>
                  <Users className="h-4 w-4" /> Add guest
                </Button>
                <span className="text-xs leading-5 text-muted">{guests.length} of {maxPartySize(calendar)} guests · multi-guest and multi-service require party create capability</span>
              </div>
            </div>
          )}
          {draftCapability.message ? <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">{draftCapability.message}</div> : null}
        </section>
      ) : null}

      {step === 2 ? (
        <section className="mt-5">
          <SectionTitle title="Choose a date" description={`The complete guest/service structure will be verified atomically by ManleAI Calendar in ${timezone}.`} />
          <div className="mt-4 max-w-sm">
            <Field label="Preferred date">
              <input type="date" value={preferredDate} onChange={(event) => changePreferredDate(event.target.value)} disabled={checking} className={inputClassName} />
            </Field>
          </div>
          {checking ? (
            <div className="mt-4 space-y-3" aria-label="Loading verified aggregate openings">
              <Skeleton className="h-28" />
              <Skeleton className="h-28" />
            </div>
          ) : null}
        </section>
      ) : null}

      {step === 3 ? (
        <section className="mt-5">
          <SectionTitle title="Choose a verified aggregate opening" description={availability?.expires_at ? `Quote expires ${formatDateTime(availability.expires_at, timezone)}.` : "The backend did not return a quote expiry."} />
          {!availability ? (
            <div className="mt-4 space-y-3"><Skeleton className="h-28" /><Skeleton className="h-28" /></div>
          ) : availability.slots.length === 0 ? (
            <div className="mt-4 rounded-md border border-line p-5 text-sm leading-6 text-muted">No all-or-none opening was returned for the complete group. Choose another date or adjust the guest/service structure.</div>
          ) : (
            <div className="mt-4 space-y-3">
              {availability.slots.map((slot) => {
                const selected = slot.fingerprint === selectedSlotFingerprint;
                const complete = slotHasCompletePolicyProof(slot, partySize, policiesByID);
                return (
                  <div key={slot.fingerprint || `${slot.start_time}-${slot.end_time}`} className={`rounded-md border ${selected ? "border-brand bg-teal-50" : "border-line bg-white"}`}>
                    <button
                      type="button"
                      onClick={() => {
                        operationRef.current = null;
                        setSelectedSlotFingerprint(slot.fingerprint || "");
                        setError("");
                      }}
                      disabled={!complete}
                      className="w-full p-4 text-left transition hover:bg-slate-50 disabled:cursor-not-allowed disabled:bg-slate-100"
                    >
                      <span className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
                        <span>
                          <span className="block text-sm font-semibold text-ink">{formatDate(slot.start_time, timezone)} · {formatTimeRange(slot.start_time, slot.end_time, timezone)}</span>
                          <span className="mt-1 block text-xs leading-5 text-muted">{partySize} guest(s) · {slot.segments?.length ?? 0} concrete service unit(s)</span>
                        </span>
                        <Badge value={!complete ? "incomplete" : selected ? "selected" : "available"} />
                      </span>
                    </button>
                    {complete ? <div className="px-4 pb-4"><QuotedGuestPlan slot={slot} timezone={timezone} compact /></div> : <div className="px-4 pb-4 text-xs text-red-700">Complete segment proof was not returned.</div>}
                  </div>
                );
              })}
            </div>
          )}
        </section>
      ) : null}

      {step === 4 ? (
        <section className="mt-5">
          <SectionTitle title="Customer details" description="Name and phone belong to the one root appointment. Guest/service references remain attached to its concrete child segments." />
          <div className="mt-4 grid gap-4 sm:grid-cols-2">
            <Field label="Customer name"><input value={customer.name} onChange={(event) => changeCustomer({ name: event.target.value })} className={inputClassName} /></Field>
            <Field label="Customer phone"><input value={customer.phone} onChange={(event) => changeCustomer({ phone: event.target.value })} className={inputClassName} /></Field>
            <Field label="Customer email (optional)"><input type="email" value={customer.email} onChange={(event) => changeCustomer({ email: event.target.value })} className={inputClassName} /></Field>
            <Field label="Notes (optional)"><input value={customer.notes} onChange={(event) => changeCustomer({ notes: event.target.value })} className={inputClassName} /></Field>
          </div>
        </section>
      ) : null}

      {step === 5 && selectedSlot ? (
        <section className="mt-5">
          <SectionTitle title="Final aggregate review" description="The exact verified segment order and allocations will be submitted under one operation key. Confirmation appears only after one durable root commit." />
          <div className="mt-4 grid gap-3 rounded-md border border-line bg-slate-50 p-4 sm:grid-cols-2">
            <ReviewItem label="Root time" value={`${formatDate(selectedSlot.start_time, timezone)} · ${formatTimeRange(selectedSlot.start_time, selectedSlot.end_time, timezone)}`} />
            <ReviewItem label="Customer" value={`${customer.name} · ${customer.phone}`} />
            <ReviewItem label="Origin" value="ManleAI Calendar" />
            <ReviewItem label="Atomic scope" value={`${partySize} guest(s) · ${selectedSlot.segments?.length ?? 0} service unit(s)`} />
          </div>
          <QuotedGuestPlan slot={selectedSlot} timezone={timezone} />
          <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
            Every child segment commits or none do. Any staff, time, resource, authority, or configuration conflict clears the entire quote and requires a new aggregate check. Internal reschedule and cancellation remain gated for Phase 4C.
          </div>
        </section>
      ) : null}

      <div className="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-between">
        <Button type="button" variant="secondary" onClick={() => setStep((current) => Math.max(1, current - 1) as CreateStep)} disabled={step === 1 || checking || submitting || submissionUncertain}>
          <ChevronLeft className="h-4 w-4" /> Back
        </Button>
        {step === 1 ? (
          <Button type="button" onClick={() => setStep(2)} disabled={!draftCapability.ready}>Continue <ChevronRight className="h-4 w-4" /></Button>
        ) : step === 2 ? (
          <Button type="button" onClick={() => void checkAvailability()} disabled={checking || !preferredDate || !draftCapability.ready}>
            <CalendarSearch className="h-4 w-4" /> {checking ? "Checking..." : "Check complete group"}
          </Button>
        ) : step === 3 ? (
          <Button type="button" onClick={() => setStep(4)} disabled={!selectedSlot}>Continue <ChevronRight className="h-4 w-4" /></Button>
        ) : step === 4 ? (
          <Button type="button" onClick={() => setStep(5)} disabled={!customer.name.trim() || !customer.phone.trim()}>Review appointment <ChevronRight className="h-4 w-4" /></Button>
        ) : (
          <Button type="button" onClick={() => void confirmAppointment()} disabled={submitting || !selectedSlot || (!draftCapability.ready && !submissionUncertain)}>
            {submitting ? "Confirming..." : submissionUncertain ? "Retry exact aggregate operation" : "Confirm appointment"}
          </Button>
        )}
      </div>
    </div>
  );
}

function QuotedGuestPlan({ slot, timezone, compact = false }: { slot: AvailabilitySlot; timezone: string; compact?: boolean }) {
  const groups = groupAvailabilitySegmentsByGuest(slot.segments ?? []);
  return (
    <div className={`${compact ? "mt-3" : "mt-4"} space-y-3`}>
      {groups.map((group, groupIndex) => (
        <div key={group.guestReference || `guest-${groupIndex}`} className={`rounded-md border border-line ${compact ? "bg-slate-50 p-3" : "bg-white p-4"}`}>
          <div className="text-xs font-semibold uppercase tracking-wide text-muted">{group.guestReference || `Guest ${groupIndex + 1}`}</div>
          <div className="mt-2 space-y-2">
            {group.segments.map((segment, segmentIndex) => (
              <QuotedSegment key={`${segment.service_id}-${segment.scheduled_start_time}-${segmentIndex}`} segment={segment} timezone={timezone} compact={compact} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function QuotedSegment({ segment, timezone, compact }: { segment: AvailabilitySegment; timezone: string; compact: boolean }) {
  const staff = segment.staff_name?.trim() || segment.staff_id || "Missing staff evidence";
  const allocations = segment.resource_allocations ?? [];
  return (
    <div className={`${compact ? "text-xs" : "text-sm"} leading-5 text-ink`}>
      <div className="font-semibold">{segment.service_name}</div>
      <div className="text-muted">{formatTimeRange(segment.scheduled_start_time, segment.scheduled_end_time, timezone)} · {staff}</div>
      {allocations.length > 0 ? (
        <div className="mt-1 flex flex-wrap gap-1.5">
          {allocations.map((allocation) => (
            <span key={allocation.resource_pool_id} className="rounded-sm bg-violet-50 px-2 py-0.5 text-violet-800">
              {allocation.resource_name || allocation.resource_pool_id} · {allocation.units_allocated} unit(s)
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function StepHeader({ step }: { step: CreateStep }) {
  const labels = ["Guests & services", "Date", "Verified plan", "Customer", "Review"];
  return (
    <div className="grid grid-cols-5 gap-1" aria-label={`Step ${Math.min(step, 5)} of 5`}>
      {labels.map((label, index) => {
        const number = index + 1;
        const active = number === step;
        const complete = number < step;
        return (
          <div key={label} className="min-w-0 text-center">
            <div className={`h-1.5 rounded-sm ${active || complete ? "bg-brand" : "bg-slate-200"}`} />
            <div className={`mt-2 truncate text-[11px] ${active ? "font-semibold text-ink" : "text-muted"}`}>{label}</div>
          </div>
        );
      })}
    </div>
  );
}

function SectionTitle({ title, description }: { title: string; description: string }) {
  return <div><div className="text-sm font-semibold text-ink">{title}</div><div className="mt-1 text-sm leading-6 text-muted">{description}</div></div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="block min-w-0"><span className="text-sm font-medium text-ink">{label}</span>{children}</label>;
}

function ReviewItem({ label, value }: { label: string; value: string }) {
  return <div><div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div><div className="mt-1 break-words text-sm font-medium text-ink">{value}</div></div>;
}

function initialGuestDraft(): GuestDraft {
  return { id: "guest-1", reference: "Guest 1", segments: [emptySegmentDraft("segment-1")] };
}

function emptySegmentDraft(id: string): SegmentDraft {
  return { id, serviceID: "", staffMode: "anyone", staffID: "" };
}

function createPolicyAvailable(policy: ManleAICalendarServicePolicy, capabilities?: ManleAICalendarCapabilities) {
  if (!policy.configured || !policy.enabled || !policy.service.active || !policy.service.ai_bookable || policy.service.archived_at || policy.service.duration_minutes <= 0) return false;
  if (policy.capacity_mode === "pooled") return capabilities?.pooled_capacity === true;
  if (policy.capacity_mode === "staff_only") return capabilities?.staff_only_create === true || capabilities?.party_create === true;
  return false;
}

function evaluateDraftCapability(
  guests: GuestDraft[],
  policiesByID: Map<string, ManleAICalendarServicePolicy>,
  capabilities?: ManleAICalendarCapabilities
): { ready: boolean; message: string } {
  if (guests.length === 0) return { ready: false, message: "Add at least one guest." };
  const references = guests.map((guest) => guest.reference.trim());
  if (references.some((reference) => !reference)) return { ready: false, message: "Each guest needs a label so returned segments can be grouped exactly." };
  if (new Set(references).size !== references.length) return { ready: false, message: "Guest labels must be unique within this appointment." };
  const segments = guests.flatMap((guest) => guest.segments);
  if (segments.length === 0 || segments.some((segment) => !segment.serviceID)) return { ready: false, message: "Choose a service for every service row." };
  if (segments.some((segment) => segment.staffMode === "specific" && !segment.staffID)) return { ready: false, message: "Choose an eligible staff member for every specific-staff preference." };
  const policies = segments.map((segment) => policiesByID.get(segment.serviceID));
  if (policies.some((policy) => !policy)) return { ready: false, message: "One or more selected services are no longer eligible for internal creation." };
  const aggregate = guests.length > 1 || segments.length > 1;
  const pooled = policies.some((policy) => policy?.capacity_mode === "pooled");
  if (aggregate && capabilities?.party_create !== true) return { ready: false, message: "Multi-guest and multi-service creation is disabled until party create capability is ready." };
  if (pooled && capabilities?.pooled_capacity !== true) return { ready: false, message: "A selected service requires pooled-capacity capability." };
  if (!aggregate && !pooled && capabilities?.staff_only_create !== true) return { ready: false, message: "Staff-only internal create capability is not ready." };
  return { ready: true, message: "" };
}

function slotHasCompletePolicyProof(
  slot: AvailabilitySlot,
  partySize: number,
  policiesByID: Map<string, ManleAICalendarServicePolicy>
) {
  if (!hasCompleteAggregateQuote(slot, partySize)) return false;
  return (slot.segments ?? []).every((segment) => {
    const policy = policiesByID.get(segment.service_id);
    const allocations = segment.resource_allocations;
    if (!policy) return false;
    if (!Array.isArray(allocations)) return false;
    if (policy.capacity_mode !== "pooled") return allocations.length === 0;
    if (policy.resource_requirements.length !== allocations.length) return false;
    return policy.resource_requirements.every((requirement) => allocations.some((allocation) =>
      allocation.resource_pool_id === requirement.resource_pool_id
      && allocation.units_allocated === requirement.units_required
      && Boolean(allocation.resource_name.trim())
    ));
  });
}

function activeStaffRef(member: ManleAICalendarStaffRef) {
  return member.active && member.ai_bookable && !member.archived_at;
}

function maxPartySize(calendar: ManleAICalendarAggregate) {
  return Math.max(1, calendar.config?.max_party_size ?? calendar.constraints.max_party_size.minimum);
}

function conflictMessage(conflict: ReturnType<typeof schedulingActionConflict>) {
  if (conflict === "quote_stale") return "A child staff, time, resource, authority, or configuration condition changed. No partial success was accepted; the complete aggregate quote was cleared and must be checked again.";
  if (conflict === "capability_changed") return "Scheduling capability or authority changed. No partial success was accepted; readiness was reloaded before a new aggregate check.";
  if (conflict === "operation_conflict") return "This operation key conflicts with different aggregate scheduling data. No success was recorded; the complete group must be checked and reviewed again.";
  return "The appointment was not created.";
}

function salonDateInput(date: Date, timezone: string) {
  const parts = new Intl.DateTimeFormat("en-CA", { timeZone: timezone, year: "numeric", month: "2-digit", day: "2-digit" }).formatToParts(date);
  const values = new Map(parts.map((part) => [part.type, part.value]));
  return `${values.get("year")}-${values.get("month")}-${values.get("day")}`;
}

function formatDate(value: string, timezone: string) {
  return new Date(value).toLocaleDateString(undefined, { timeZone: timezone, month: "short", day: "numeric", year: "numeric" });
}

function formatTimeRange(start: string, end: string, timezone: string) {
  const options = { timeZone: timezone, hour: "numeric", minute: "2-digit" } as const;
  return `${new Date(start).toLocaleTimeString(undefined, options)} – ${new Date(end).toLocaleTimeString(undefined, options)}`;
}

function formatDateTime(value: string, timezone: string) {
  return new Date(value).toLocaleString(undefined, { timeZone: timezone, dateStyle: "medium", timeStyle: "short" });
}

const inputClassName = "mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100";
