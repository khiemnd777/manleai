"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, CalendarClock, ClipboardList, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  AppointmentRecord,
  BookingAttempt,
  POSConnection,
  POSService,
  POSStaffMember,
  Salon,
  SquareReadiness,
  SyncLog
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type AppointmentsResponse = {
  appointments: AppointmentRecord[];
};

type AttemptsResponse = {
  booking_attempts: BookingAttempt[];
};

type ServicesResponse = {
  services: POSService[];
};

type StaffResponse = {
  staff: POSStaffMember[];
};

export function AppointmentsDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [appointments, setAppointments] = useState<AppointmentRecord[]>([]);
  const [attempts, setAttempts] = useState<BookingAttempt[]>([]);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setAppointments([]);
        setAttempts([]);
        setServices([]);
        setStaff([]);
        return;
      }

      const [statusResponse, appointmentResponse, attemptResponse, serviceResponse, staffResponse] =
        await Promise.all([
          apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
          apiRequest<AppointmentsResponse>(`/api/salons/${firstSalon.id}/appointments?limit=50`),
          apiRequest<AttemptsResponse>(`/api/salons/${firstSalon.id}/booking-attempts?limit=50`),
          apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
          apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
        ]);
      setStatus(statusResponse);
      setAppointments(appointmentResponse.appointments);
      setAttempts(attemptResponse.booking_attempts);
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load appointment data.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const serviceNames = useMemo(() => new Map(services.map((item) => [item.id, item.name])), [services]);
  const staffNames = useMemo(() => new Map(staff.map((item) => [item.id, item.name])), [staff]);
  const pendingRequests = useMemo(
    () => attempts.filter((item) => item.status === "fallback_pending"),
    [attempts]
  );
  const upcomingCount = useMemo(() => {
    const now = Date.now();
    return appointments.filter((item) => item.status !== "cancelled" && new Date(item.start_time).getTime() >= now)
      .length;
  }, [appointments]);
  const confirmedCount = appointments.filter((item) => item.status === "confirmed").length;
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);

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
        <Skeleton className="h-96" />
        <Skeleton className="h-80" />
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>
          Appointments and fallback requests are scoped by salon, so the owner profile must exist first.
        </CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Appointments</h1>
          <p className="mt-1 text-sm text-muted">
            Confirmed Square Appointments bookings and pending requests that need owner review.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Appointments unavailable" message={error} /> : null}

      <ReadinessPanel status={status} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Confirmed appointments" value={String(confirmedCount)} />
        <Metric label="Upcoming" value={String(upcomingCount)} />
        <Metric label="Pending requests" value={String(pendingRequests.length)} />
        <Metric label="Last Square sync" value={formatOptionalDate(status?.connection.last_sync_at)} />
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Appointments confirmed by POS</CardTitle>
            <CardDescription>
              These rows are confirmed only because Square Appointments returned a booking ID.
            </CardDescription>
          </div>
          <Badge value={appointments.length > 0 ? "active" : "disabled"} />
        </div>

        {appointments.length === 0 ? (
          <EmptyState
            icon={<CalendarClock className="h-5 w-5 text-muted" />}
            title="No appointments yet"
            message="Confirmed bookings will appear here after Square returns a booking ID."
          />
        ) : (
          <>
            <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[860px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">When</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Service</th>
                    <th className="px-4 py-3">Staff</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3">POS booking</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {appointments.map((item) => (
                    <tr key={item.id}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{formatDate(item.start_time)}</div>
                        <div className="mt-1 text-xs text-muted">{formatTimeRange(item.start_time, item.end_time)}</div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{item.customer_name}</div>
                        <div className="mt-1 text-xs text-muted">{item.customer_phone}</div>
                      </td>
                      <td className="px-4 py-3 text-muted">{lookupName(serviceNames, item.service_id)}</td>
                      <td className="px-4 py-3 text-muted">{lookupName(staffNames, item.staff_id)}</td>
                      <td className="px-4 py-3">
                        <Badge value={item.status} />
                      </td>
                      <td className="px-4 py-3 text-muted">{item.pos_appointment_id || "Not returned"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {appointments.map((item) => (
                <AppointmentCard
                  key={item.id}
                  item={item}
                  serviceName={lookupName(serviceNames, item.service_id)}
                  staffName={lookupName(staffNames, item.staff_id)}
                />
              ))}
            </div>
          </>
        )}
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Fallback requests</CardTitle>
            <CardDescription>
              These requests are not confirmed appointments. Review them when the POS path fails.
            </CardDescription>
          </div>
          <Badge value={pendingRequests.length > 0 ? "fallback_pending" : "disabled"} />
        </div>

        {pendingRequests.length === 0 ? (
          <EmptyState
            icon={<ClipboardList className="h-5 w-5 text-muted" />}
            title="No pending requests"
            message="POS failures will create pending requests here for owner review."
          />
        ) : (
          <>
            <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[900px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Requested time</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Service</th>
                    <th className="px-4 py-3">Staff</th>
                    <th className="px-4 py-3">Failure reason</th>
                    <th className="px-4 py-3">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {pendingRequests.map((item) => (
                    <tr key={item.id}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{formatDate(item.requested_start_time)}</div>
                        <div className="mt-1 text-xs text-muted">
                          {formatTimeRange(item.requested_start_time, item.requested_end_time)}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{item.customer_name}</div>
                        <div className="mt-1 text-xs text-muted">{item.customer_phone}</div>
                      </td>
                      <td className="px-4 py-3 text-muted">{lookupName(serviceNames, item.service_id)}</td>
                      <td className="px-4 py-3 text-muted">{lookupName(staffNames, item.staff_id)}</td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{item.error_code || "POS error"}</div>
                        <div className="mt-1 max-w-xs text-xs leading-5 text-muted">
                          {item.error_message || "Review POS logs for details."}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={item.status} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {pendingRequests.map((item) => (
                <FallbackCard
                  key={item.id}
                  item={item}
                  serviceName={lookupName(serviceNames, item.service_id)}
                  staffName={lookupName(staffNames, item.staff_id)}
                />
              ))}
            </div>
          </>
        )}
      </Card>
    </div>
  );
}

function ReadinessPanel({ status }: { status: StatusResponse | null }) {
  const connection = status?.connection;
  const readiness = status?.readiness;
  const connected = Boolean(connection?.id) && connection?.status !== "not_connected";
  const locationSelected = Boolean(connection?.location_id);
  const readyForBookings =
    connected && locationSelected && (readiness?.service_count ?? 0) > 0 && (readiness?.staff_count ?? 0) > 0;

  if (readyForBookings) {
    return (
      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <CardTitle>Square booking path is configured</CardTitle>
            <CardDescription className="text-emerald-800">
              Confirmed appointments require a successful Square Appointments booking ID.
            </CardDescription>
          </div>
          <Badge value={readiness?.ai_enabled ? "active" : "disabled"} />
        </div>
      </Card>
    );
  }

  return (
    <Card className="border-amber-200 bg-amber-50 shadow-none">
      <div className="flex gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
        <div>
          <CardTitle>Booking workflow is gated</CardTitle>
          <CardDescription className="text-amber-900">
            Connect Square Appointments, select a location, and sync services and staff before AI booking can operate.
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
    </div>
  );
}

function AppointmentCard({
  item,
  serviceName,
  staffName
}: {
  item: AppointmentRecord;
  serviceName: string;
  staffName: string;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{item.customer_name}</div>
          <div className="mt-1 text-xs text-muted">{item.customer_phone}</div>
        </div>
        <Badge value={item.status} />
      </div>
      <InfoGrid
        items={[
          ["When", `${formatDate(item.start_time)} ${formatTimeRange(item.start_time, item.end_time)}`],
          ["Service", serviceName],
          ["Staff", staffName],
          ["POS booking", item.pos_appointment_id || "Not returned"]
        ]}
      />
    </div>
  );
}

function FallbackCard({
  item,
  serviceName,
  staffName
}: {
  item: BookingAttempt;
  serviceName: string;
  staffName: string;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{item.customer_name}</div>
          <div className="mt-1 text-xs text-muted">{item.customer_phone}</div>
        </div>
        <Badge value={item.status} />
      </div>
      <InfoGrid
        items={[
          ["Requested", `${formatDate(item.requested_start_time)} ${formatTimeRange(item.requested_start_time, item.requested_end_time)}`],
          ["Service", serviceName],
          ["Staff", staffName],
          ["Failure", item.error_code || "POS error"]
        ]}
      />
      <div className="mt-3 text-sm leading-6 text-muted">
        {item.error_message || "Review POS logs for details."}
      </div>
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

function lookupName(items: Map<string | undefined, string>, id?: string) {
  if (!id) return "-";
  return items.get(id) || "Unknown";
}

function formatOptionalDate(value?: string) {
  if (!value) return "Not synced";
  return formatDate(value);
}

function formatDate(value: string) {
  return new Date(value).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric"
  });
}

function formatTimeRange(start: string, end: string) {
  const startLabel = new Date(start).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  const endLabel = new Date(end).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  return `${startLabel} - ${endLabel}`;
}
