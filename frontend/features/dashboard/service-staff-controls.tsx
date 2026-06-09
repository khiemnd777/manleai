"use client";

import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, RefreshCcw, Settings2, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  POSConnection,
  POSService,
  POSStaffMember,
  Salon,
  SquareReadiness,
  SyncLog
} from "@/types/api";

type CatalogMode = "services" | "staff";

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

type StaffResponse = {
  staff: POSStaffMember[];
};

export function ServicesDashboard() {
  return <CatalogControls mode="services" />;
}

export function StaffDashboard() {
  return <CatalogControls mode="staff" />;
}

function CatalogControls({ mode }: { mode: CatalogMode }) {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyID, setBusyID] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

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
        setStaff([]);
        return;
      }

      if (mode === "services") {
        const [statusResponse, serviceResponse] = await Promise.all([
          apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
          apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`)
        ]);
        setStatus(statusResponse);
        setServices(serviceResponse.services);
        return;
      }

      const [statusResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(statusResponse);
      setStaff(staffResponse.staff);
    } catch (err) {
      setError(err instanceof Error ? err.message : `Could not load ${mode}.`);
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const items = mode === "services" ? services : staff;
  const activeCount = items.filter((item) => item.active).length;
  const aiBookableCount = items.filter((item) => item.active && item.ai_bookable).length;
  const blockedCount = items.filter((item) => item.active && !item.ai_bookable).length;
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
  const labels = catalogLabels(mode);

  async function updateAIBookable(id: string | undefined, nextValue: boolean) {
    if (!salon || !id) return;
    setBusyID(id);
    setError("");
    setSuccess("");
    try {
      if (mode === "services") {
        const response = await apiRequest<{ service: POSService }>(
          `/api/salons/${salon.id}/services/${id}/ai-bookable`,
          {
            method: "PATCH",
            body: JSON.stringify({ ai_bookable: nextValue })
          }
        );
        setServices((current) => current.map((item) => (item.id === id ? response.service : item)));
      } else {
        const response = await apiRequest<{ staff_member: POSStaffMember }>(
          `/api/salons/${salon.id}/staff/${id}/ai-bookable`,
          {
            method: "PATCH",
            body: JSON.stringify({ ai_bookable: nextValue })
          }
        );
        setStaff((current) => current.map((item) => (item.id === id ? response.staff_member : item)));
      }
      setSuccess(nextValue ? "AI booking allowed for this record." : "AI booking blocked for this record.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : `Could not update ${labels.singular}.`);
    } finally {
      setBusyID("");
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
        <Skeleton className="h-[32rem]" />
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>
          {labels.title} controls are scoped by salon, so the owner profile must exist first.
        </CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">{labels.title}</h1>
          <p className="mt-1 text-sm text-muted">{labels.description}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busyID !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title={`${labels.title} unavailable`} message={error} /> : null}
      {success ? <Alert type="success" title={`${labels.singularTitle} updated`} message={success} /> : null}

      <CatalogGate status={status} mode={mode} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label={`Total ${labels.plural}`} value={String(items.length)} />
        <Metric label="Active in Square" value={String(activeCount)} />
        <Metric label="AI bookable" value={String(aiBookableCount)} />
        <Metric label="Blocked" value={String(blockedCount)} />
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>{labels.tableTitle}</CardTitle>
            <CardDescription>
              These controls only change AI booking eligibility in this dashboard. Square records are not edited.
            </CardDescription>
          </div>
          <Badge value={items.length > 0 ? "active" : "disabled"} />
        </div>

        {items.length === 0 ? (
          <EmptyState
            icon={mode === "services" ? <Settings2 className="h-5 w-5 text-muted" /> : <Users className="h-5 w-5 text-muted" />}
            title={`No ${labels.plural} synced yet`}
            message={`Sync Square Appointments ${labels.plural} before configuring AI booking eligibility.`}
          />
        ) : mode === "services" ? (
          <ServicesTable
            services={services}
            busyID={busyID}
            onUpdate={(id, nextValue) => void updateAIBookable(id, nextValue)}
          />
        ) : (
          <StaffTable
            staff={staff}
            busyID={busyID}
            onUpdate={(id, nextValue) => void updateAIBookable(id, nextValue)}
          />
        )}
      </Card>
    </div>
  );
}

function CatalogGate({ status, mode }: { status: StatusResponse | null; mode: CatalogMode }) {
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
              Last synced {new Date(lastSync).toLocaleString()}. Active and AI-bookable records can be used by the AI receptionist.
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
          <CardTitle>Square sync required</CardTitle>
          <CardDescription className="text-amber-900">
            Connect Square Appointments, select a location, and sync {mode} before these controls can affect booking readiness.
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

function ServicesTable({
  services,
  busyID,
  onUpdate
}: {
  services: POSService[];
  busyID: string;
  onUpdate: (id: string | undefined, nextValue: boolean) => void;
}) {
  return (
    <>
      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
        <table className="w-full min-w-[880px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Service</th>
              <th className="px-4 py-3">Duration</th>
              <th className="px-4 py-3">Price</th>
              <th className="px-4 py-3">Square status</th>
              <th className="px-4 py-3">AI booking</th>
              <th className="px-4 py-3">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line bg-white">
            {services.map((service) => (
              <tr key={service.id || service.pos_service_id}>
                <td className="px-4 py-3">
                  <div className="font-medium text-ink">{service.name}</div>
                  <div className="mt-1 max-w-sm text-xs leading-5 text-muted">{service.description || "No description synced."}</div>
                </td>
                <td className="px-4 py-3 text-muted">{service.duration_minutes > 0 ? `${service.duration_minutes} min` : "Not set"}</td>
                <td className="px-4 py-3 text-muted">{service.price_display || formatPrice(service.price_from)}</td>
                <td className="px-4 py-3">
                  <Badge value={service.active ? "active" : "disabled"} />
                </td>
                <td className="px-4 py-3">
                  <Badge value={aiStatus(service)} />
                </td>
                <td className="px-4 py-3">
                  <ToggleButton item={service} busyID={busyID} onUpdate={onUpdate} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-5 space-y-3 lg:hidden">
        {services.map((service) => (
          <CatalogCard
            key={service.id || service.pos_service_id}
            title={service.name}
            subtitle={service.description || "No description synced."}
            facts={[
              ["Duration", service.duration_minutes > 0 ? `${service.duration_minutes} min` : "Not set"],
              ["Price", service.price_display || formatPrice(service.price_from)]
            ]}
            item={service}
            busyID={busyID}
            onUpdate={onUpdate}
          />
        ))}
      </div>
    </>
  );
}

function StaffTable({
  staff,
  busyID,
  onUpdate
}: {
  staff: POSStaffMember[];
  busyID: string;
  onUpdate: (id: string | undefined, nextValue: boolean) => void;
}) {
  return (
    <>
      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
        <table className="w-full min-w-[760px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Staff</th>
              <th className="px-4 py-3">Contact</th>
              <th className="px-4 py-3">Square status</th>
              <th className="px-4 py-3">AI booking</th>
              <th className="px-4 py-3">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line bg-white">
            {staff.map((member) => (
              <tr key={member.id || member.pos_staff_id}>
                <td className="px-4 py-3 font-medium text-ink">{member.name}</td>
                <td className="px-4 py-3 text-muted">
                  <div>{member.phone || "No phone synced"}</div>
                  <div className="mt-1 text-xs">{member.email || "No email synced"}</div>
                </td>
                <td className="px-4 py-3">
                  <Badge value={member.active ? "active" : "disabled"} />
                </td>
                <td className="px-4 py-3">
                  <Badge value={aiStatus(member)} />
                </td>
                <td className="px-4 py-3">
                  <ToggleButton item={member} busyID={busyID} onUpdate={onUpdate} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-5 space-y-3 lg:hidden">
        {staff.map((member) => (
          <CatalogCard
            key={member.id || member.pos_staff_id}
            title={member.name}
            subtitle={member.email || member.phone || "No contact synced."}
            facts={[
              ["Phone", member.phone || "-"],
              ["Email", member.email || "-"]
            ]}
            item={member}
            busyID={busyID}
            onUpdate={onUpdate}
          />
        ))}
      </div>
    </>
  );
}

function ToggleButton({
  item,
  busyID,
  onUpdate
}: {
  item: POSService | POSStaffMember;
  busyID: string;
  onUpdate: (id: string | undefined, nextValue: boolean) => void;
}) {
  const disabled = busyID !== "" || !item.id || !item.active;
  const busy = busyID === item.id;
  const nextValue = !item.ai_bookable;
  return (
    <Button
      type="button"
      variant={item.ai_bookable ? "secondary" : "primary"}
      onClick={() => onUpdate(item.id, nextValue)}
      disabled={disabled}
      className="w-full sm:w-auto"
    >
      {busy ? "Saving..." : item.active ? (item.ai_bookable ? "Block AI booking" : "Allow AI booking") : "Unavailable"}
    </Button>
  );
}

function CatalogCard({
  title,
  subtitle,
  facts,
  item,
  busyID,
  onUpdate
}: {
  title: string;
  subtitle: string;
  facts: [string, string][];
  item: POSService | POSStaffMember;
  busyID: string;
  onUpdate: (id: string | undefined, nextValue: boolean) => void;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{title}</div>
          <div className="mt-1 text-xs leading-5 text-muted">{subtitle}</div>
        </div>
        <Badge value={aiStatus(item)} />
      </div>
      <InfoGrid
        items={[
          ["Square status", item.active ? "Active" : "Inactive"],
          ...facts
        ]}
      />
      <div className="mt-4">
        <ToggleButton item={item} busyID={busyID} onUpdate={onUpdate} />
      </div>
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

function EmptyState({ icon, title, message }: { icon: ReactNode; title: string; message: string }) {
  return (
    <div className="mt-5 rounded-md border border-line p-6 text-center">
      <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-md bg-slate-100">{icon}</div>
      <div className="mt-3 text-sm font-semibold text-ink">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted">{message}</div>
      <a
        className="mt-4 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
        href="/dashboard/integrations"
      >
        Open Square integration
      </a>
    </div>
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

function aiStatus(item: POSService | POSStaffMember) {
  if (!item.active) return "disabled";
  return item.ai_bookable ? "allowed" : "blocked";
}

function formatPrice(value?: number) {
  if (!value) return "Not set";
  return `$${value.toFixed(2)}`;
}

function catalogLabels(mode: CatalogMode) {
  if (mode === "services") {
    return {
      title: "Services",
      singularTitle: "Service",
      tableTitle: "Service AI booking controls",
      singular: "service",
      plural: "services",
      description: "Square-synced services that the AI receptionist may offer for booking."
    };
  }
  return {
    title: "Staff",
    singularTitle: "Staff",
    tableTitle: "Staff AI booking controls",
    singular: "staff member",
    plural: "staff",
    description: "Square-synced staff that the AI receptionist may assign to bookings."
  };
}
