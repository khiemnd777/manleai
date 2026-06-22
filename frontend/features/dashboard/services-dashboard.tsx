"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, Archive, Pencil, Plus, RefreshCcw, Settings2 } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type { POSConnection, POSService, Salon, SquareReadiness, SyncLog } from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type ServicesResponse = {
  services: POSService[];
};

type ServiceResponse = {
  service: POSService;
};

type ServiceFormState = {
  name: string;
  description: string;
  aiDescription: string;
  durationMinutes: string;
  priceFrom: string;
  active: boolean;
};

export function ServicesDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editingService, setEditingService] = useState<POSService | null>(null);
  const [form, setForm] = useState<ServiceFormState>(emptyServiceForm());

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
        setServices([]);
        return;
      }
      const [statusResponse, serviceResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`)
      ]);
      setStatus(statusResponse);
      setServices(serviceResponse.services);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load services.");
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const metrics = useMemo(() => serviceMetrics(services), [services]);
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);

  function openCreateForm() {
    setEditingService(null);
    setForm(emptyServiceForm());
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  function openEditForm(service: POSService) {
    setEditingService(service);
    setForm(serviceToForm(service));
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  async function saveService() {
    if (!salon) return;
    setBusy("save-service");
    setError("");
    setSuccess("");
    try {
      const body = JSON.stringify(servicePayload(form));
      const response = editingService?.id
        ? await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${editingService.id}`, {
            method: "PUT",
            body
          })
        : await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services`, {
            method: "POST",
            body
          });
      setServices((current) => upsertService(current, response.service));
      setSuccess(editingService ? "Service saved." : "Service created. Local-only services are not bookable until linked to Square Appointments.");
      setEditingService(response.service);
      setForm(serviceToForm(response.service));
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save service.");
    } finally {
      setBusy("");
    }
  }

  async function archiveService(service: POSService) {
    if (!salon || !service.id || service.archived_at) return;
    if (!window.confirm(`Archive ${service.name}? This will disable AI booking for this service.`)) return;
    setBusy(`archive-${service.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${service.id}/archive`, {
        method: "POST"
      });
      setServices((current) => upsertService(current, response.service));
      if (editingService?.id === response.service.id) {
        setEditingService(response.service);
        setForm(serviceToForm(response.service));
      }
      setSuccess("Service archived. It will not be used for new availability checks or bookings.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive service.");
    } finally {
      setBusy("");
    }
  }

  async function updateAIBookable(service: POSService, nextValue: boolean) {
    if (!salon || !service.id) return;
    setBusy(`ai-${service.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${service.id}/ai-bookable`, {
        method: "PATCH",
        body: JSON.stringify({ ai_bookable: nextValue })
      });
      setServices((current) => upsertService(current, response.service));
      if (editingService?.id === response.service.id) {
        setEditingService(response.service);
        setForm(serviceToForm(response.service));
      }
      setSuccess(nextValue ? "AI booking allowed for this synced service." : "AI booking blocked for this service.");
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
        <CardDescription>Services are scoped by salon, so the owner profile must exist first.</CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Services</h1>
          <p className="mt-1 text-sm text-muted">
            Manage ManleAI service records. Square Appointments remains the booking execution layer.
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
            New service
          </Button>
        </div>
      </div>

      {error ? <Alert title="Services unavailable" message={error} /> : null}
      {success ? <Alert type="success" title="Services updated" message={success} /> : null}

      <ServicesGate status={status} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Total services" value={String(metrics.total)} />
        <Metric label="Synced" value={String(metrics.synced)} />
        <Metric label="Local only" value={String(metrics.localOnly)} />
        <Metric label="AI bookable" value={String(metrics.aiBookable)} />
      </div>

      {formOpen ? (
        <ServiceForm
          form={form}
          service={editingService}
          busy={busy === "save-service"}
          onChange={setForm}
          onCancel={() => {
            setFormOpen(false);
            setEditingService(null);
            setForm(emptyServiceForm());
          }}
          onSave={() => void saveService()}
        />
      ) : null}

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Service catalog</CardTitle>
            <CardDescription>
              Local-only services are visible here but cannot be booked until linked to Square Appointments.
            </CardDescription>
          </div>
          <Badge value={services.length > 0 ? "active" : "disabled"} />
        </div>

        {services.length === 0 ? (
          <EmptyState onCreate={openCreateForm} />
        ) : (
          <ServicesTable
            services={services}
            busy={busy}
            onEdit={openEditForm}
            onArchive={(service) => void archiveService(service)}
            onUpdateAI={(service, nextValue) => void updateAIBookable(service, nextValue)}
          />
        )}
      </Card>
    </div>
  );
}

function ServicesGate({ status }: { status: StatusResponse | null }) {
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
              Last synced {new Date(lastSync).toLocaleString()}. Synced services can be enabled for AI booking.
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
            Local services can be managed now. Booking remains gated until Square Appointments is connected, a location is selected, and services are synced.
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

function ServiceForm({
  form,
  service,
  busy,
  onChange,
  onCancel,
  onSave
}: {
  form: ServiceFormState;
  service: POSService | null;
  busy: boolean;
  onChange: (next: ServiceFormState) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const archived = Boolean(service?.archived_at);
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>{service ? "Edit service" : "New service"}</CardTitle>
          <CardDescription>
            {service ? serviceGateReason(service) : "New services start as local only and cannot be booked until linked to Square Appointments."}
          </CardDescription>
        </div>
        {service ? <Badge value={service.sync_status || "local_only"} /> : <Badge value="local_only" />}
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
        <Field label="Duration minutes">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            type="number"
            min="1"
            value={form.durationMinutes}
            onChange={(event) => onChange({ ...form, durationMinutes: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="Price from">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            type="number"
            min="0"
            step="0.01"
            value={form.priceFrom}
            onChange={(event) => onChange({ ...form, priceFrom: event.target.value })}
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
        <Field label="Description">
          <textarea
            className="min-h-24 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand"
            value={form.description}
            onChange={(event) => onChange({ ...form, description: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="AI description">
          <textarea
            className="min-h-24 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand"
            value={form.aiDescription}
            onChange={(event) => onChange({ ...form, aiDescription: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
      </div>

      <div className="mt-5 flex flex-wrap justify-end gap-3">
        <Button type="button" variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="button" onClick={onSave} disabled={busy || archived}>
          {busy ? "Saving..." : "Save service"}
        </Button>
      </div>
    </Card>
  );
}

function ServicesTable({
  services,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  services: POSService[];
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
}) {
  return (
    <>
      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
        <table className="w-full min-w-[1040px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Service</th>
              <th className="px-4 py-3">Duration</th>
              <th className="px-4 py-3">Price</th>
              <th className="px-4 py-3">Source</th>
              <th className="px-4 py-3">Sync status</th>
              <th className="px-4 py-3">AI booking</th>
              <th className="px-4 py-3">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line bg-white">
            {services.map((service) => (
              <tr key={service.id || service.pos_service_id || service.name}>
                <td className="px-4 py-3">
                  <div className="font-medium text-ink">{service.name}</div>
                  <div className="mt-1 max-w-sm text-xs leading-5 text-muted">{service.description || "No description."}</div>
                </td>
                <td className="px-4 py-3 text-muted">{service.duration_minutes > 0 ? `${service.duration_minutes} min` : "Not set"}</td>
                <td className="px-4 py-3 text-muted">{service.price_display || formatPrice(service.price_from)}</td>
                <td className="px-4 py-3">
                  <Badge value={service.source || "local"} />
                </td>
                <td className="px-4 py-3">
                  <div className="space-y-1">
                    <Badge value={service.sync_status || "local_only"} />
                    {service.sync_error ? <div className="max-w-44 text-xs leading-5 text-red-700">{service.sync_error}</div> : null}
                  </div>
                </td>
                <td className="px-4 py-3">
                  <AIStatus service={service} />
                </td>
                <td className="px-4 py-3">
                  <ServiceActions service={service} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-5 space-y-3 lg:hidden">
        {services.map((service) => (
          <ServiceCard
            key={service.id || service.pos_service_id || service.name}
            service={service}
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

function ServiceCard({
  service,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  service: POSService;
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{service.name}</div>
          <div className="mt-1 text-xs leading-5 text-muted">{service.description || "No description."}</div>
        </div>
        <Badge value={service.sync_status || "local_only"} />
      </div>
      <InfoGrid
        items={[
          ["Duration", service.duration_minutes > 0 ? `${service.duration_minutes} min` : "Not set"],
          ["Price", service.price_display || formatPrice(service.price_from)],
          ["Source", service.source || "local"],
          ["POS link", service.pos_linked ? "Linked" : "Not linked"]
        ]}
      />
      <div className="mt-4">
        <AIStatus service={service} />
      </div>
      <div className="mt-4">
        <ServiceActions service={service} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
      </div>
    </div>
  );
}

function ServiceActions({
  service,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  service: POSService;
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
}) {
  const aiBusy = busy === `ai-${service.id}`;
  const archiveBusy = busy === `archive-${service.id}`;
  const archived = Boolean(service.archived_at);
  const canEnable = canEnableAI(service);
  const nextAI = !service.ai_bookable;
  return (
    <div className="flex flex-wrap gap-2">
      <Button type="button" variant="secondary" onClick={() => onEdit(service)} disabled={busy !== ""}>
        <Pencil className="h-4 w-4" />
        Edit
      </Button>
      <Button
        type="button"
        variant={service.ai_bookable ? "secondary" : "primary"}
        onClick={() => onUpdateAI(service, nextAI)}
        disabled={busy !== "" || !service.id || (!service.ai_bookable && !canEnable)}
      >
        {aiBusy ? "Saving..." : service.ai_bookable ? "Block AI booking" : canEnable ? "Allow AI booking" : "AI booking gated"}
      </Button>
      <Button type="button" variant="danger" onClick={() => onArchive(service)} disabled={busy !== "" || archived || !service.id}>
        <Archive className="h-4 w-4" />
        {archiveBusy ? "Archiving..." : "Archive"}
      </Button>
    </div>
  );
}

function AIStatus({ service }: { service: POSService }) {
  return (
    <div className="space-y-1">
      <Badge value={service.ai_bookable && canEnableAI(service) ? "allowed" : "blocked"} />
      <div className="max-w-56 text-xs leading-5 text-muted">{serviceGateReason(service)}</div>
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
        <Settings2 className="h-5 w-5 text-muted" />
      </div>
      <div className="mt-3 text-sm font-semibold text-ink">No services yet</div>
      <div className="mt-1 text-sm leading-6 text-muted">Create a local service or sync Square Appointments services.</div>
      <div className="mt-4 flex flex-wrap justify-center gap-3">
        <Button type="button" onClick={onCreate}>
          <Plus className="h-4 w-4" />
          New service
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

function serviceMetrics(services: POSService[]) {
  return {
    total: services.length,
    synced: services.filter((service) => service.sync_status === "synced" && service.pos_linked).length,
    localOnly: services.filter((service) => service.sync_status === "local_only").length,
    aiBookable: services.filter((service) => service.ai_bookable && canEnableAI(service)).length
  };
}

function emptyServiceForm(): ServiceFormState {
  return {
    name: "",
    description: "",
    aiDescription: "",
    durationMinutes: "45",
    priceFrom: "",
    active: true
  };
}

function serviceToForm(service: POSService): ServiceFormState {
  return {
    name: service.name,
    description: service.description ?? "",
    aiDescription: service.ai_description ?? "",
    durationMinutes: service.duration_minutes > 0 ? String(service.duration_minutes) : "",
    priceFrom: service.price_from ? String(service.price_from) : "",
    active: service.active
  };
}

function servicePayload(form: ServiceFormState) {
  const price = form.priceFrom.trim() === "" ? null : Number(form.priceFrom);
  return {
    name: form.name,
    description: form.description,
    ai_description: form.aiDescription,
    duration_minutes: Number(form.durationMinutes),
    price_from: Number.isFinite(price) ? price : null,
    active: form.active
  };
}

function upsertService(items: POSService[], service: POSService) {
  const exists = items.some((item) => item.id === service.id);
  const next = exists ? items.map((item) => (item.id === service.id ? service : item)) : [service, ...items];
  return next.sort(compareServices);
}

function compareServices(a: POSService, b: POSService) {
  const archivedA = a.archived_at ? 1 : 0;
  const archivedB = b.archived_at ? 1 : 0;
  if (archivedA !== archivedB) return archivedA - archivedB;
  if (a.active !== b.active) return a.active ? -1 : 1;
  return a.name.localeCompare(b.name);
}

function canEnableAI(service: POSService) {
  return (
    service.active &&
    !service.archived_at &&
    service.sync_status === "synced" &&
    service.pos_linked &&
    Boolean(service.pos_service_id) &&
    Boolean(service.pos_service_version) &&
    service.duration_minutes > 0
  );
}

function serviceGateReason(service: POSService) {
  if (service.archived_at || service.sync_status === "archived") return "Archived services are retained for history and cannot be booked.";
  if (!service.active) return "Inactive services cannot be offered by the AI receptionist.";
  if (!service.pos_linked || service.sync_status === "local_only") return "Local-only services need a Square Appointments link before AI booking.";
  if (service.sync_status === "sync_failed") return service.sync_error || "Latest POS sync failed.";
  if (service.sync_status === "unmapped") return "Service needs a provider mapping before AI booking.";
  if (!service.pos_service_version) return "Square booking metadata is incomplete.";
  if (service.ai_bookable) return "Synced and available for AI booking.";
  return "Synced service can be enabled for AI booking.";
}

function formatPrice(value?: number) {
  if (!value) return "Not set";
  return `$${value.toFixed(2)}`;
}
