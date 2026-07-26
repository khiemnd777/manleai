"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CalendarClock, CheckCircle2, Plus, RefreshCcw, XCircle } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useTenantSalon } from "@/components/layout/tenant-salon-context";
import { businessGet, newBusinessActionKey, type BusinessService, type BusinessStaff } from "@/lib/api/business";
import { apiRequest } from "@/lib/api/client";
import {
  buildStaffOnlyCreatePayload,
  checkSchedulingAvailability,
  executeSchedulingAction
} from "@/lib/api/scheduling-actions";
import type {
  AppointmentRecord,
  AvailabilitySlot,
  SchedulingAvailabilityResponse,
  SchedulingRequest,
  SchedulingRequestsResponse,
  SchedulingRequestStatus
} from "@/types/api";

type AppointmentsResponse = { appointments: AppointmentRecord[]; has_more?: boolean };
type Section = "appointments" | "requests" | "create";
type CreateForm = {
  customerName: string;
  customerPhone: string;
  customerEmail: string;
  serviceID: string;
  staffID: string;
  preferredDate: string;
  notes: string;
};

const emptyCreateForm: CreateForm = {
  customerName: "",
  customerPhone: "",
  customerEmail: "",
  serviceID: "",
  staffID: "",
  preferredDate: "",
  notes: ""
};

export function TenantAppointmentsConsole() {
  const { activeSalon, loading: salonLoading, error: salonError } = useTenantSalon();
  const [section, setSection] = useState<Section>("appointments");
  const [appointments, setAppointments] = useState<AppointmentRecord[]>([]);
  const [requests, setRequests] = useState<SchedulingRequest[]>([]);
  const [services, setServices] = useState<BusinessService[]>([]);
  const [staff, setStaff] = useState<BusinessStaff[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [form, setForm] = useState<CreateForm>(emptyCreateForm);
  const [availability, setAvailability] = useState<SchedulingAvailabilityResponse | null>(null);
  const [selectedSlot, setSelectedSlot] = useState<AvailabilitySlot | null>(null);
  const actionRef = useRef<{ signature: string; key: string } | null>(null);

  const load = useCallback(async () => {
    if (!activeSalon) return;
    setLoading(true);
    setError("");
    try {
      const surface = { kind: "tenant" as const, salonID: activeSalon.id };
      const [appointmentResponse, requestResponse, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<AppointmentsResponse>(`/api/salons/${activeSalon.id}/appointments?limit=100`),
        apiRequest<SchedulingRequestsResponse>(`/api/salons/${activeSalon.id}/scheduling-requests?limit=100`),
        businessGet<{ services: BusinessService[] }>(surface, "services"),
        businessGet<{ staff: BusinessStaff[] }>(surface, "staff")
      ]);
      setAppointments(appointmentResponse.appointments ?? []);
      setRequests(requestResponse.scheduling_requests ?? []);
      setServices(serviceResponse.services.filter((item) => item.active && !item.archived_at));
      setStaff(staffResponse.staff.filter((item) => item.active && !item.archived_at));
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load appointment business data.");
    } finally {
      setLoading(false);
    }
  }, [activeSalon]);

  useEffect(() => { void load(); }, [load]);

  const eligibleStaff = useMemo(() => {
    if (!form.serviceID) return staff;
    return staff.filter((member) => member.service_ids.includes(form.serviceID));
  }, [form.serviceID, staff]);

  function updateForm(patch: Partial<CreateForm>) {
    setForm((current) => ({ ...current, ...patch }));
    setAvailability(null);
    setSelectedSlot(null);
    actionRef.current = null;
  }

  async function checkAvailability() {
    if (!activeSalon || !form.serviceID || !form.preferredDate) {
      setError("Choose a service and preferred date first.");
      return;
    }
    const staffMode = form.staffID ? "specific" : "anyone";
    setBusy("availability");
    setError("");
    setSuccess("");
    try {
      const response = await checkSchedulingAvailability(activeSalon.id, {
        service_id: form.serviceID,
        staff_id: form.staffID || undefined,
        staff_selection_mode: staffMode,
        segments: [{ service_id: form.serviceID, staff_id: form.staffID || undefined, staff_selection_mode: staffMode, quantity: 1 }],
        party_size: 1,
        preferred_date: form.preferredDate,
        limit: 8
      });
      setAvailability(response);
      setSelectedSlot(response.kind === "verified_slots" ? response.verified_slots.slots[0] ?? null : null);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not check scheduling availability.");
    } finally {
      setBusy("");
    }
  }

  function actionKey(payload: unknown) {
    const signature = JSON.stringify(payload);
    if (!actionRef.current || actionRef.current.signature !== signature) {
      actionRef.current = { signature, key: newBusinessActionKey("tenant-scheduling") };
    }
    return actionRef.current.key;
  }

  async function createSchedulingWork() {
    if (!activeSalon || !availability || !form.customerName.trim() || !form.customerPhone.trim()) {
      setError("Customer name, phone, and a completed availability check are required.");
      return;
    }
    const staffMode = form.staffID ? "specific" : "anyone";
    let payload: Record<string, unknown>;
    if (availability.kind === "verified_slots") {
      if (!selectedSlot || !availability.verified_slots.quote_id) {
        setError("Choose a verified slot before creating the appointment.");
        return;
      }
      const confirmed = buildStaffOnlyCreatePayload({
        availabilityQuoteID: availability.verified_slots.quote_id,
        slot: selectedSlot,
        serviceID: form.serviceID,
        staffSelectionMode: staffMode,
        customerName: form.customerName.trim(),
        customerPhone: form.customerPhone.trim(),
        customerEmail: form.customerEmail.trim() || undefined,
        timezone: activeSalon.timezone,
        notes: form.notes.trim() || undefined
      });
      if (!confirmed) {
        setError("The verified slot does not contain a complete staff assignment. Check availability again.");
        return;
      }
      payload = confirmed;
    } else {
      payload = {
        operation_type: "book",
        customer_name: form.customerName.trim(),
        customer_phone: form.customerPhone.trim(),
        customer_email: form.customerEmail.trim() || undefined,
        segments: [{ service_id: form.serviceID, staff_id: form.staffID || undefined, staff_selection_mode: staffMode, quantity: 1 }],
        requested_timezone: activeSalon.timezone,
        party_size: 1,
        notes: form.notes.trim() || undefined
      };
    }
    setBusy("create");
    setError("");
    try {
      const result = await executeSchedulingAction(activeSalon.id, { ...payload, operation_key: actionKey(payload) } as Parameters<typeof executeSchedulingAction>[1]);
      actionRef.current = null;
      setForm(emptyCreateForm);
      setAvailability(null);
      setSelectedSlot(null);
      setSuccess(result.kind === "pending_owner_review"
        ? "A pending owner-review request was created. It is not a confirmed appointment."
        : result.kind === "confirmed_appointment"
          ? "The appointment was confirmed with durable scheduling evidence."
          : "The scheduling request is pending safe provider follow-up.");
      setSection(result.kind === "pending_owner_review" ? "requests" : "appointments");
      await load();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not create scheduling work.");
    } finally {
      setBusy("");
    }
  }

  async function transitionRequest(item: SchedulingRequest, status: SchedulingRequestStatus) {
    if (!activeSalon) return;
    setBusy(`request-${item.id}`);
    setError("");
    try {
      await apiRequest(`/api/salons/${activeSalon.id}/scheduling-requests/${item.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          action_key: newBusinessActionKey(`request-${status}`),
          expected_version: item.version,
          status,
          ...(status === "resolved" || status === "dismissed" ? { resolution_reason: `tenant_business_${status}` } : {})
        })
      });
      await load();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not update the owner-review request.");
    } finally {
      setBusy("");
    }
  }

  async function cancelAppointment(item: AppointmentRecord) {
    if (!activeSalon || !window.confirm(`Cancel the appointment for ${item.customer_name}?`)) return;
    const payload = {
      operation_type: "cancel" as const,
      operation_key: newBusinessActionKey("tenant-cancel"),
      customer_name: item.customer_name,
      customer_phone: item.customer_phone,
      customer_email: item.customer_email,
      requested_timezone: activeSalon.timezone,
      target_appointment_id: item.id,
      target_scheduling_authority: item.scheduling_authority,
      expected_target_authority_appointment_version: item.authority_appointment_version || 1,
      notes: "Cancelled from Tenant Business Console"
    };
    setBusy(`cancel-${item.id}`);
    setError("");
    try {
      const result = await executeSchedulingAction(activeSalon.id, payload);
      setSuccess(result.kind === "pending_owner_review"
        ? "A pending cancellation request was created; the appointment is not cancelled yet."
        : "Cancellation completed with durable scheduling evidence.");
      await load();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not cancel this appointment.");
    } finally {
      setBusy("");
    }
  }

  if (salonLoading || loading) return <div className="space-y-4"><Skeleton className="h-20" /><Skeleton className="h-72" /></div>;
  if (salonError || !activeSalon) return <Alert title="Salon unavailable" message={salonError || "No active salon is available."} />;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div><h1 className="text-2xl font-bold text-ink">Appointments</h1><p className="mt-1 text-sm text-muted">Manage scheduling work for {activeSalon.name}. Provider credentials and technical recovery stay in Platform UI.</p></div>
        <Button type="button" variant="secondary" onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Refresh</Button>
      </div>
      {error ? <Alert title="Appointment action needs attention" message={error} /> : null}
      {success ? <Alert type="success" title="Scheduling action saved" message={success} /> : null}
      <div className="grid gap-2 sm:grid-cols-3">
        <SectionButton active={section === "appointments"} onClick={() => setSection("appointments")} icon={<CalendarClock className="h-4 w-4" />} label={`Appointments (${appointments.length})`} />
        <SectionButton active={section === "requests"} onClick={() => setSection("requests")} icon={<CheckCircle2 className="h-4 w-4" />} label={`Owner review (${requests.filter((item) => item.status === "pending").length})`} />
        <SectionButton active={section === "create"} onClick={() => setSection("create")} icon={<Plus className="h-4 w-4" />} label="New scheduling work" />
      </div>
      {section === "appointments" ? <AppointmentList items={appointments} busy={busy} onCancel={cancelAppointment} /> : null}
      {section === "requests" ? <RequestList items={requests} busy={busy} onTransition={transitionRequest} /> : null}
      {section === "create" ? <CreateCard form={form} services={services} staff={eligibleStaff} availability={availability} selectedSlot={selectedSlot} busy={busy} onForm={updateForm} onCheck={checkAvailability} onSelectSlot={setSelectedSlot} onCreate={createSchedulingWork} /> : null}
    </div>
  );
}

function SectionButton({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return <button type="button" onClick={onClick} className={`flex items-center gap-2 rounded-lg border p-4 text-left text-sm font-semibold shadow-soft ${active ? "border-teal-300 bg-teal-50 text-brand" : "border-line bg-white text-slate-700"}`}>{icon}{label}</button>;
}

function AppointmentList({ items, busy, onCancel }: { items: AppointmentRecord[]; busy: string; onCancel: (item: AppointmentRecord) => void }) {
  if (!items.length) return <Alert title="No appointments" message="Confirmed appointments for this salon will appear here." />;
  return <div className="space-y-3">{items.map((item) => <Card key={item.id}><div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex flex-wrap gap-2"><Badge value={item.status} /><Badge value={item.scheduling_authority} /></div><CardTitle className="mt-3">{item.customer_name}</CardTitle><CardDescription>{new Date(item.start_time).toLocaleString()} · {item.segments?.map((segment) => segment.service_name).join(", ") || "Service"}</CardDescription></div>{item.status !== "cancelled" ? <Button type="button" variant="danger" disabled={Boolean(busy)} onClick={() => onCancel(item)}><XCircle className="h-4 w-4" />{busy === `cancel-${item.id}` ? "Cancelling…" : "Cancel"}</Button> : null}</div></Card>)}</div>;
}

function RequestList({ items, busy, onTransition }: { items: SchedulingRequest[]; busy: string; onTransition: (item: SchedulingRequest, status: SchedulingRequestStatus) => void }) {
  if (!items.length) return <Alert title="No owner-review requests" message="Request-only scheduling actions will appear here and are never presented as confirmed appointments." />;
  return <div className="space-y-3">{items.map((item) => <Card key={item.id}><div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div><div className="flex flex-wrap gap-2"><Badge value={item.status} /><Badge value={item.operation_type} /></div><CardTitle className="mt-3">{item.customer_name}</CardTitle><CardDescription>{item.segments?.map((segment) => segment.service_name).join(", ") || item.target_description || "Scheduling request"} · {new Date(item.created_at).toLocaleString()}</CardDescription></div>{item.status === "pending" || item.status === "contacted" ? <div className="flex gap-2"><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => onTransition(item, "contacted")}>Contacted</Button><Button type="button" disabled={Boolean(busy)} onClick={() => onTransition(item, "resolved")}>{busy === `request-${item.id}` ? "Saving…" : "Resolve"}</Button></div> : null}</div></Card>)}</div>;
}

function CreateCard({ form, services, staff, availability, selectedSlot, busy, onForm, onCheck, onSelectSlot, onCreate }: { form: CreateForm; services: BusinessService[]; staff: BusinessStaff[]; availability: SchedulingAvailabilityResponse | null; selectedSlot: AvailabilitySlot | null; busy: string; onForm: (patch: Partial<CreateForm>) => void; onCheck: () => void; onSelectSlot: (slot: AvailabilitySlot) => void; onCreate: () => void }) {
  return <Card><CardTitle>New scheduling work</CardTitle><CardDescription>The active authority decides whether this becomes a durable appointment, a pending owner-review request, or a safe provider fallback. The Tenant UI does not expose provider setup.</CardDescription><div className="mt-5 grid gap-4 sm:grid-cols-2"><Field label="Customer name"><input className="field" value={form.customerName} onChange={(event) => onForm({ customerName: event.target.value })} /></Field><Field label="Customer phone"><input className="field" value={form.customerPhone} onChange={(event) => onForm({ customerPhone: event.target.value })} /></Field><Field label="Customer email"><input className="field" type="email" value={form.customerEmail} onChange={(event) => onForm({ customerEmail: event.target.value })} /></Field><Field label="Preferred date"><input className="field" type="date" value={form.preferredDate} onChange={(event) => onForm({ preferredDate: event.target.value })} /></Field><Field label="Service"><select className="field" value={form.serviceID} onChange={(event) => onForm({ serviceID: event.target.value, staffID: "" })}><option value="">Choose service</option>{services.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></Field><Field label="Staff"><select className="field" value={form.staffID} onChange={(event) => onForm({ staffID: event.target.value })}><option value="">Anyone available</option>{staff.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></Field><label className="sm:col-span-2"><span className="text-sm font-semibold text-ink">Notes</span><textarea className="field mt-2 min-h-20" value={form.notes} onChange={(event) => onForm({ notes: event.target.value })} /></label></div><div className="mt-5 flex flex-wrap gap-3"><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={onCheck}>{busy === "availability" ? "Checking…" : "Check availability"}</Button>{availability ? <Button type="button" disabled={Boolean(busy)} onClick={onCreate}>{busy === "create" ? "Saving…" : availability.kind === "request_only" ? "Create pending request" : "Create appointment"}</Button> : null}</div>{availability?.kind === "request_only" ? <div className="mt-4"><Alert title="Request-only authority" message="Submitting creates a pending owner-review request. It does not confirm an appointment." /></div> : null}{availability?.kind === "verified_slots" ? <div className="mt-5 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{availability.verified_slots.slots.map((slot) => <button key={slot.fingerprint || slot.start_time} type="button" onClick={() => onSelectSlot(slot)} className={`rounded-md border p-3 text-left text-sm ${selectedSlot === slot ? "border-teal-400 bg-teal-50" : "border-line bg-white"}`}><div className="font-semibold text-ink">{new Date(slot.start_time).toLocaleString()}</div><div className="mt-1 text-xs text-muted">{slot.staff_name || "Assigned staff"}</div></button>)}</div> : null}</Card>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label><span className="text-sm font-semibold text-ink">{label}</span><div className="mt-2">{children}</div></label>; }
