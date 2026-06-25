"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, Archive, Pencil, Plus, RefreshCcw, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type { POSConnection, POSStaffMember, Salon, SquareReadiness, SyncLog } from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type StaffResponse = {
  staff: POSStaffMember[];
};

type StaffMutationResponse = {
  staff_member: POSStaffMember;
};

type StaffFormState = {
  name: string;
  phone: string;
  email: string;
  active: boolean;
};

export function StaffDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editingStaff, setEditingStaff] = useState<POSStaffMember | null>(null);
  const [form, setForm] = useState<StaffFormState>(emptyStaffForm());

  async function load({ silent = false }: { silent?: boolean } = {}) {
    setError("");
    if (!silent) {
      setLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setStaff([]);
        return;
      }
      const [statusResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(statusResponse);
      setStaff(staffResponse.staff);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load staff.");
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const metrics = useMemo(() => staffMetrics(staff), [staff]);
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);

  function openCreateForm() {
    setEditingStaff(null);
    setForm(emptyStaffForm());
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  function openEditForm(member: POSStaffMember) {
    setEditingStaff(member);
    setForm(staffToForm(member));
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  async function saveStaff() {
    if (!salon) return;
    setBusy("save-staff");
    setError("");
    setSuccess("");
    try {
      const body = JSON.stringify(staffPayload(form));
      const response = editingStaff?.id
        ? await apiRequest<StaffMutationResponse>(`/api/salons/${salon.id}/staff/${editingStaff.id}`, {
            method: "PUT",
            body
          })
        : await apiRequest<StaffMutationResponse>(`/api/salons/${salon.id}/staff`, {
            method: "POST",
            body
          });
      setStaff((current) => upsertStaff(current, response.staff_member));
      setSuccess(editingStaff ? "Staff member saved." : "Staff member created. Local-only staff are not booking-ready until linked to Square Appointments.");
      setEditingStaff(response.staff_member);
      setForm(staffToForm(response.staff_member));
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save staff member.");
    } finally {
      setBusy("");
    }
  }

  async function archiveStaff(member: POSStaffMember) {
    if (!salon || !member.id || member.archived_at) return;
    if (!window.confirm(`Archive ${member.name}? This will disable AI booking for this staff member.`)) return;
    setBusy(`archive-${member.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<StaffMutationResponse>(`/api/salons/${salon.id}/staff/${member.id}/archive`, {
        method: "POST"
      });
      setStaff((current) => upsertStaff(current, response.staff_member));
      if (editingStaff?.id === response.staff_member.id) {
        setEditingStaff(response.staff_member);
        setForm(staffToForm(response.staff_member));
      }
      setSuccess("Staff member archived. They will not be used for new availability checks or bookings.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive staff member.");
    } finally {
      setBusy("");
    }
  }

  async function updateAIBookable(member: POSStaffMember, nextValue: boolean) {
    if (!salon || !member.id) return;
    setBusy(`ai-${member.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<StaffMutationResponse>(`/api/salons/${salon.id}/staff/${member.id}/ai-bookable`, {
        method: "PATCH",
        body: JSON.stringify({ ai_bookable: nextValue })
      });
      setStaff((current) => upsertStaff(current, response.staff_member));
      if (editingStaff?.id === response.staff_member.id) {
        setEditingStaff(response.staff_member);
        setForm(staffToForm(response.staff_member));
      }
      setSuccess(nextValue ? "Staff member marked booking-ready for the AI receptionist." : "Staff member removed from AI booking.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not update AI booking eligibility.");
    } finally {
      setBusy("");
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <div className="grid gap-4 md:grid-cols-4">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <Skeleton className="h-[34rem]" />
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>Staff records are scoped by salon, so the owner profile must exist first.</CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Staff</h1>
          <p className="mt-1 text-sm text-muted">
            Manage ManleAI-owned staff records. Square Appointments executes availability and booking.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={openCreateForm} disabled={busy !== ""}>
            <Plus className="h-4 w-4" />
            New staff
          </Button>
        </div>
      </div>

      {error ? <Alert title="Staff unavailable" message={error} /> : null}
      {success ? <Alert type="success" title="Staff updated" message={success} /> : null}

      <StaffGate status={status} />
      <BookingEligibilityPanel />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Total staff" value={String(metrics.total)} />
        <Metric label="Synced" value={String(metrics.synced)} />
        <Metric label="Local only" value={String(metrics.localOnly)} />
        <Metric label="Booking-ready" value={String(metrics.aiBookable)} />
      </div>

      {formOpen ? (
        <StaffForm
          form={form}
          member={editingStaff}
          busy={busy === "save-staff"}
          onChange={setForm}
          onCancel={() => {
            setFormOpen(false);
            setEditingStaff(null);
            setForm(emptyStaffForm());
          }}
          onSave={() => void saveStaff()}
        />
      ) : null}

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Staff directory</CardTitle>
            <CardDescription>
              Only active, synced, POS-linked, AI-bookable staff are used for availability and booking.
            </CardDescription>
          </div>
          <Badge value={staff.length > 0 ? "active" : "disabled"} />
        </div>

        {staff.length === 0 ? (
          <EmptyState onCreate={openCreateForm} />
        ) : (
          <StaffTable
            staff={staff}
            busy={busy}
            onEdit={openEditForm}
            onArchive={(member) => void archiveStaff(member)}
            onUpdateAI={(member, nextValue) => void updateAIBookable(member, nextValue)}
          />
        )}
      </Card>
    </div>
  );
}

function StaffGate({ status }: { status: StatusResponse | null }) {
  const connection = status?.connection;
  const connected = Boolean(connection?.id) && connection?.status !== "not_connected";
  const locationSelected = Boolean(connection?.location_id);
  const lastSync = connection?.last_sync_at;

  if (connected && locationSelected && lastSync) {
    return (
      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <CardTitle>Square sync is ready</CardTitle>
            <CardDescription className="text-emerald-800">
              Last synced {new Date(lastSync).toLocaleString()}. Synced staff can become booking-ready after AI booking is allowed.
            </CardDescription>
          </div>
          <Badge value="active" />
        </div>
      </Card>
    );
  }

  return (
    <Card className="border-amber-200 bg-amber-50 shadow-none">
      <div className="flex gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
        <div>
          <CardTitle>Square sync required for AI booking</CardTitle>
          <CardDescription className="text-amber-900">
            Local staff can be managed now. Booking remains gated until Square Appointments is connected, a location is selected, and staff are synced.
          </CardDescription>
          <a
            className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/integrations"
          >
            Open Square integration
          </a>
        </div>
      </div>
    </Card>
  );
}

function BookingEligibilityPanel() {
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Booking eligibility</CardTitle>
          <CardDescription>
            Staff records can exist locally, but booking stays gated until the active POS link is ready.
          </CardDescription>
        </div>
        <Badge value="booking" />
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <EligibilityItem
          label="Booking-ready staff"
          value="Active, synced, POS linked, and allowed for AI booking."
        />
        <EligibilityItem
          label="Not bookable"
          value="Local-only, unmapped, sync-failed, archived, or inactive staff stay out of availability and booking."
        />
        <EligibilityItem
          label="Booking execution"
          value="Square Appointments returns availability and booking IDs before appointments can be confirmed."
        />
      </div>
    </Card>
  );
}

function EligibilityItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-line p-3">
      <div className="text-sm font-semibold text-ink">{label}</div>
      <div className="mt-1 text-xs leading-5 text-muted">{value}</div>
    </div>
  );
}

function StaffForm({
  form,
  member,
  busy,
  onChange,
  onCancel,
  onSave
}: {
  form: StaffFormState;
  member: POSStaffMember | null;
  busy: boolean;
  onChange: (next: StaffFormState) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const archived = Boolean(member?.archived_at);
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>{member ? "Edit staff member" : "New staff member"}</CardTitle>
          <CardDescription>
            {member ? staffGateReason(member) : "New staff start as ManleAI local records and are not booking-ready until linked to Square Appointments."}
          </CardDescription>
        </div>
        {member ? <Badge value={member.sync_status || "local_only"} /> : <Badge value="local_only" />}
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-2">
        <Field label="Name">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            value={form.name}
            onChange={(event) => onChange({ ...form, name: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="Phone">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            value={form.phone}
            onChange={(event) => onChange({ ...form, phone: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="Email">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            type="email"
            value={form.email}
            onChange={(event) => onChange({ ...form, email: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <label className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm font-medium text-ink">
          <input
            type="checkbox"
            checked={form.active}
            onChange={(event) => onChange({ ...form, active: event.target.checked })}
            disabled={busy || archived}
          />
          Active
        </label>
      </div>

      <div className="mt-5 flex flex-wrap justify-end gap-3">
        <Button type="button" variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="button" onClick={onSave} disabled={busy || archived}>
          {busy ? "Saving..." : "Save staff member"}
        </Button>
      </div>
    </Card>
  );
}

function StaffTable({
  staff,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  staff: POSStaffMember[];
  busy: string;
  onEdit: (member: POSStaffMember) => void;
  onArchive: (member: POSStaffMember) => void;
  onUpdateAI: (member: POSStaffMember, nextValue: boolean) => void;
}) {
  return (
    <>
      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
        <table className="w-full min-w-[960px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Staff</th>
              <th className="px-4 py-3">Contact</th>
              <th className="px-4 py-3">Source</th>
              <th className="px-4 py-3">Sync status</th>
              <th className="px-4 py-3">Booking readiness</th>
              <th className="w-56 px-4 py-3">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line bg-white">
            {staff.map((member) => (
              <tr key={member.id || member.pos_staff_id || member.name}>
                <td className="px-4 py-3 font-medium text-ink">{member.name}</td>
                <td className="px-4 py-3 text-muted">
                  <div>{member.phone || "No phone"}</div>
                  <div className="mt-1 text-xs">{member.email || "No email"}</div>
                </td>
                <td className="px-4 py-3">
                  <Badge value={member.source || "local"} />
                </td>
                <td className="px-4 py-3">
                  <div className="space-y-1">
                    <Badge value={member.sync_status || "local_only"} />
                    {member.sync_error ? <div className="max-w-44 text-xs leading-5 text-red-700">{member.sync_error}</div> : null}
                  </div>
                </td>
                <td className="px-4 py-3">
                  <AIStatus member={member} />
                </td>
                <td className="w-56 px-4 py-3 align-top">
                  <StaffActions member={member} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-5 space-y-3 lg:hidden">
        {staff.map((member) => (
          <StaffCard
            key={member.id || member.pos_staff_id || member.name}
            member={member}
            busy={busy}
            onEdit={onEdit}
            onArchive={onArchive}
            onUpdateAI={onUpdateAI}
          />
        ))}
      </div>
    </>
  );
}

function StaffCard({
  member,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  member: POSStaffMember;
  busy: string;
  onEdit: (member: POSStaffMember) => void;
  onArchive: (member: POSStaffMember) => void;
  onUpdateAI: (member: POSStaffMember, nextValue: boolean) => void;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{member.name}</div>
          <div className="mt-1 text-xs leading-5 text-muted">{member.email || member.phone || "No contact details."}</div>
        </div>
        <Badge value={member.sync_status || "local_only"} />
      </div>
      <InfoGrid
        items={[
          ["Phone", member.phone || "-"],
          ["Email", member.email || "-"],
          ["Source", member.source || "local"],
          ["POS link", member.pos_linked ? "Linked" : "Not linked"]
        ]}
      />
      <div className="mt-4">
        <AIStatus member={member} />
      </div>
      <div className="mt-4">
        <StaffActions member={member} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
      </div>
    </div>
  );
}

function StaffActions({
  member,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  member: POSStaffMember;
  busy: string;
  onEdit: (member: POSStaffMember) => void;
  onArchive: (member: POSStaffMember) => void;
  onUpdateAI: (member: POSStaffMember, nextValue: boolean) => void;
}) {
  const aiBusy = busy === `ai-${member.id}`;
  const archiveBusy = busy === `archive-${member.id}`;
  const archived = Boolean(member.archived_at);
  const canEnable = canEnableAI(member);
  const nextAI = !member.ai_bookable;
  return (
    <div className="grid w-full gap-2">
      <Button
        type="button"
        variant={member.ai_bookable ? "secondary" : "primary"}
        className="w-full whitespace-nowrap px-3"
        onClick={() => onUpdateAI(member, nextAI)}
        disabled={busy !== "" || !member.id || (!member.ai_bookable && !canEnable)}
      >
        {aiBusy ? "Saving..." : member.ai_bookable ? "Block AI booking" : canEnable ? "Allow AI booking" : "AI booking gated"}
      </Button>
      <div className="grid grid-cols-2 gap-2">
        <Button type="button" variant="secondary" className="h-9 px-3 text-xs" onClick={() => onEdit(member)} disabled={busy !== ""}>
          <Pencil className="h-4 w-4" />
          Edit
        </Button>
        <Button
          type="button"
          variant="danger"
          className="h-9 px-3 text-xs"
          onClick={() => onArchive(member)}
          disabled={busy !== "" || archived || !member.id}
        >
          {archiveBusy ? null : <Archive className="h-4 w-4" />}
          {archiveBusy ? "Archiving" : "Archive"}
        </Button>
      </div>
    </div>
  );
}

function AIStatus({ member }: { member: POSStaffMember }) {
  return (
    <div className="space-y-1">
      <Badge value={member.ai_bookable && canEnableAI(member) ? "allowed" : "blocked"} />
      <div className="max-w-56 text-xs leading-5 text-muted">{staffGateReason(member)}</div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-2 text-2xl font-bold text-ink">{value}</div>
    </Card>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="mt-5 rounded-md border border-line p-6 text-center">
      <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-md bg-slate-100">
        <Users className="h-5 w-5 text-muted" />
      </div>
      <div className="mt-3 text-sm font-semibold text-ink">No staff yet</div>
      <div className="mt-1 text-sm leading-6 text-muted">
        Create a local staff member or sync Square Appointments staff. Local records are not bookable until linked.
      </div>
      <div className="mt-4 flex flex-wrap justify-center gap-3">
        <Button type="button" onClick={onCreate}>
          <Plus className="h-4 w-4" />
          New staff
        </Button>
        <a
          className="inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
          href="/dashboard/integrations"
        >
          Open Square integration
        </a>
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <span className="mt-2 block">{children}</span>
    </label>
  );
}

function InfoGrid({ items }: { items: [string, string][] }) {
  return (
    <div className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
      {items.map(([label, value]) => (
        <div key={label}>
          <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
          <div className="mt-1 break-words font-medium text-ink">{value}</div>
        </div>
      ))}
    </div>
  );
}

function staffMetrics(staff: POSStaffMember[]) {
  return {
    total: staff.length,
    synced: staff.filter((member) => member.sync_status === "synced" && member.pos_linked).length,
    localOnly: staff.filter((member) => member.sync_status === "local_only").length,
    aiBookable: staff.filter((member) => member.ai_bookable && canEnableAI(member)).length
  };
}

function emptyStaffForm(): StaffFormState {
  return {
    name: "",
    phone: "",
    email: "",
    active: true
  };
}

function staffToForm(member: POSStaffMember): StaffFormState {
  return {
    name: member.name,
    phone: member.phone ?? "",
    email: member.email ?? "",
    active: member.active
  };
}

function staffPayload(form: StaffFormState) {
  return {
    name: form.name,
    phone: form.phone,
    email: form.email,
    active: form.active
  };
}

function upsertStaff(items: POSStaffMember[], member: POSStaffMember) {
  const exists = items.some((item) => item.id === member.id);
  const next = exists ? items.map((item) => (item.id === member.id ? member : item)) : [member, ...items];
  return next.sort(compareStaff);
}

function compareStaff(a: POSStaffMember, b: POSStaffMember) {
  const archivedA = a.archived_at ? 1 : 0;
  const archivedB = b.archived_at ? 1 : 0;
  if (archivedA !== archivedB) return archivedA - archivedB;
  if (a.active !== b.active) return a.active ? -1 : 1;
  return a.name.localeCompare(b.name);
}

function canEnableAI(member: POSStaffMember) {
  return (
    member.active &&
    !member.archived_at &&
    member.sync_status === "synced" &&
    member.pos_linked &&
    Boolean(member.pos_staff_id)
  );
}

function staffGateReason(member: POSStaffMember) {
  if (member.archived_at || member.sync_status === "archived") return "Archived staff stay visible for history and are not bookable.";
  if (!member.active) return "Inactive staff are not bookable by the AI receptionist.";
  if (!member.pos_linked || member.sync_status === "local_only") return "Local-only staff need a Square Appointments link before they are booking-ready.";
  if (member.sync_status === "sync_failed") return member.sync_error || "Latest POS sync failed; staff member is not bookable.";
  if (member.sync_status === "unmapped") return "Staff member needs an active-provider mapping before they are bookable.";
  if (member.ai_bookable) return "Booking-ready: synced, POS linked, and allowed for AI booking.";
  return "Synced staff member can be allowed for AI booking.";
}
