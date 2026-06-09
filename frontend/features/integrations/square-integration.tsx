"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Ban,
  CalendarCheck,
  CheckCircle2,
  ExternalLink,
  Power,
  PowerOff,
  RefreshCcw,
  XCircle
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  POSConnection,
  POSLocation,
  POSService,
  POSStaffMember,
  Salon,
  SquareReadiness,
  SyncLog,
  TestBookingRecord
} from "@/types/api";

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

type LocationsResponse = {
  locations: POSLocation[];
};

type TestBookingResponse = {
  booking_attempt?: {
    status: string;
    error_code?: string;
    error_message?: string;
  };
  appointment?: {
    status: string;
  };
  latest_test_booking?: TestBookingRecord;
  readiness: SquareReadiness;
};

type GateResponse = {
  readiness: SquareReadiness;
};

type TestBookingForm = {
  service_id: string;
  staff_id: string;
  start_time: string;
  customer_name: string;
  customer_phone: string;
  customer_email: string;
  notes: string;
};

const defaultForm: TestBookingForm = {
  service_id: "",
  staff_id: "",
  start_time: nextTestStartTime(),
  customer_name: "ManleAI Test Customer",
  customer_phone: "+15555550199",
  customer_email: "",
  notes: "AI booking readiness test. Cancel after verifying Square booking creation."
};

export function SquareIntegration() {
  const [salons, setSalons] = useState<Salon[]>([]);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [locations, setLocations] = useState<POSLocation[]>([]);
  const [selectedLocationID, setSelectedLocationID] = useState("");
  const [form, setForm] = useState<TestBookingForm>(defaultForm);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const salon = salons[0];
  const connection = status?.connection;
  const readiness = status?.readiness;
  const latestTest = readiness?.latest_test_booking;

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      setSalons(salonResponse.salons);
      const firstSalon = salonResponse.salons[0];
      if (!firstSalon) {
        setStatus(null);
        setServices([]);
        setStaff([]);
        setLocations([]);
        return;
      }

      const [squareStatus, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(squareStatus);
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);
      setSelectedLocationID(squareStatus.connection.location_id || "");

      if (squareStatus.connection.id) {
        const locationResponse = await apiRequest<LocationsResponse>(
          `/api/integrations/square/locations?salon_id=${firstSalon.id}`
        );
        setLocations(locationResponse.locations);
        if (!squareStatus.connection.location_id && locationResponse.locations[0]) {
          setSelectedLocationID(locationResponse.locations[0].id);
        }
      } else {
        setLocations([]);
      }

      setForm((current) => ({
        ...current,
        service_id: current.service_id || firstBookableService(serviceResponse.services)?.id || "",
        staff_id: current.staff_id || firstBookableStaff(staffResponse.staff)?.id || ""
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load integrations.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const bookableServices = useMemo(
    () =>
      services.filter(
        (service) =>
          service.id &&
          service.active &&
          service.ai_bookable &&
          service.pos_service_version &&
          service.duration_minutes > 0
      ),
    [services]
  );
  const bookableStaff = useMemo(
    () => staff.filter((member) => member.id && member.active && member.ai_bookable),
    [staff]
  );

  async function connectSquare() {
    if (!salon) return;
    setBusy("connect");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<{ url: string }>(
        `/api/integrations/square/connect-url?salon_id=${salon.id}`
      );
      window.location.href = response.url;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not start Square OAuth.");
    } finally {
      setBusy("");
    }
  }

  async function selectLocation() {
    if (!salon || !selectedLocationID) return;
    setBusy("location");
    setError("");
    setSuccess("");
    try {
      await apiRequest<POSConnection>("/api/integrations/square/select-location", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id, location_id: selectedLocationID })
      });
      setSuccess("Square location selected.");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not select Square location.");
    } finally {
      setBusy("");
    }
  }

  async function syncSquare() {
    if (!salon) return;
    setBusy("sync");
    setError("");
    setSuccess("");
    try {
      await apiRequest<{ ok: boolean }>("/api/integrations/square/sync", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      setSuccess("Services and staff sync completed.");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Square sync failed.");
    } finally {
      setBusy("");
    }
  }

  async function createTestBooking() {
    if (!salon) return;
    setBusy("test");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<TestBookingResponse>("/api/integrations/square/test-booking", {
        method: "POST",
        body: JSON.stringify({
          salon_id: salon.id,
          ...form,
          start_time: new Date(form.start_time).toISOString()
        })
      });
      applyReadiness(response.readiness);
      if (response.booking_attempt?.status === "fallback_pending") {
        setError(response.booking_attempt.error_message || "Test booking is pending owner review.");
      } else {
        setSuccess("Square test booking created. Cancel it before enabling AI booking.");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create Square test booking.");
    } finally {
      setBusy("");
    }
  }

  async function cancelTestBooking() {
    if (!salon || !latestTest?.appointment_id) return;
    setBusy("cancel-test");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<TestBookingResponse>(
        "/api/integrations/square/cancel-test-booking",
        {
          method: "POST",
          body: JSON.stringify({
            salon_id: salon.id,
            appointment_id: latestTest.appointment_id,
            reason: "AI booking readiness test cleanup"
          })
        }
      );
      applyReadiness(response.readiness);
      if (response.booking_attempt?.status === "fallback_pending") {
        setError(response.booking_attempt.error_message || "Test booking cancellation needs owner review.");
      } else {
        setSuccess("Square test booking cancelled.");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not cancel Square test booking.");
    } finally {
      setBusy("");
    }
  }

  async function enableAI() {
    if (!salon) return;
    setBusy("enable");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<GateResponse>("/api/integrations/square/enable-ai-booking", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      applyReadiness(response.readiness);
      setSalons((items) => items.map((item) => (item.id === salon.id ? { ...item, ai_enabled: true } : item)));
      setSuccess("AI booking enabled for this salon.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not enable AI booking.");
    } finally {
      setBusy("");
    }
  }

  async function disableAI() {
    if (!salon) return;
    setBusy("disable");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<GateResponse>("/api/integrations/square/disable-ai-booking", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      applyReadiness(response.readiness);
      setSalons((items) => items.map((item) => (item.id === salon.id ? { ...item, ai_enabled: false } : item)));
      setSuccess("AI booking disabled.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not disable AI booking.");
    } finally {
      setBusy("");
    }
  }

  function applyReadiness(next: SquareReadiness) {
    setStatus((current) => (current ? { ...current, readiness: next } : current));
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <Skeleton className="h-64" />
        <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
          <Skeleton className="h-96" />
          <Skeleton className="h-96" />
        </div>
      </div>
    );
  }

  if (error && !status) {
    return <Alert title="Integration unavailable" message={error} />;
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>
          Square connection is scoped by salon, so the owner profile must exist before OAuth.
        </CardDescription>
      </Card>
    );
  }

  const canCreateTest =
    Boolean(readiness?.can_test_booking) &&
    Boolean(form.service_id) &&
    Boolean(form.staff_id) &&
    Boolean(form.start_time) &&
    busy === "";
  const canCancelTest = Boolean(readiness?.can_cancel_test_booking && latestTest?.appointment_id) && busy === "";
  const canEnable = Boolean(readiness?.can_enable_ai_booking) && busy === "";
  const aiEnabled = Boolean(readiness?.ai_enabled ?? salon.ai_enabled);

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Integrations</h1>
          <p className="mt-1 text-sm text-muted">
            Square Appointments is the only native POS integration in this pilot release.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Square action failed" message={error} /> : null}
      {success ? <Alert type="success" title="Square updated" message={success} /> : null}

      <Card>
        <div className="flex flex-col justify-between gap-5 lg:flex-row lg:items-start">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-md bg-slate-900 text-sm font-bold text-white">
              SQ
            </div>
            <div>
              <CardTitle>Square Appointments</CardTitle>
              <CardDescription>
                Connect OAuth, choose a location, then sync services and staff into this system.
              </CardDescription>
            </div>
          </div>
          <Badge value={connection?.status ?? "not_connected"} />
        </div>

        <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-5">
          <Info label="Provider" value="Square" />
          <Info label="Merchant ID" value={connection?.merchant_id || "Not connected"} />
          <Info label="Location ID" value={connection?.location_id || "Not selected"} />
          <Info label="Bookable services" value={String(readiness?.service_count ?? 0)} />
          <Info label="Bookable staff" value={String(readiness?.staff_count ?? 0)} />
        </div>

        {connection?.error_message ? (
          <div className="mt-5">
            <Alert title="Last Square error" message={connection.error_message} />
          </div>
        ) : null}

        <div className="mt-6 grid gap-3 lg:grid-cols-[1fr_1fr_auto]">
          <Button type="button" onClick={() => void connectSquare()} disabled={busy !== ""}>
            <ExternalLink className="h-4 w-4" />
            {busy === "connect" ? "Opening..." : "Connect Square"}
          </Button>
          <div className="flex min-w-0 gap-2">
            <select
              className="h-10 min-w-0 flex-1 rounded-md border border-line bg-white px-3 text-sm text-ink"
              value={selectedLocationID}
              onChange={(event) => setSelectedLocationID(event.target.value)}
              disabled={!connection?.id || locations.length === 0 || busy !== ""}
            >
              {locations.length === 0 ? <option value="">No locations loaded</option> : null}
              {locations.map((location) => (
                <option key={location.id} value={location.id}>
                  {location.name || location.id}
                </option>
              ))}
            </select>
            <Button
              type="button"
              variant="secondary"
              onClick={() => void selectLocation()}
              disabled={!connection?.id || !selectedLocationID || selectedLocationID === connection.location_id || busy !== ""}
            >
              Save
            </Button>
          </div>
          <Button
            type="button"
            variant="secondary"
            onClick={() => void syncSquare()}
            disabled={!connection?.id || busy !== ""}
          >
            <RefreshCcw className="h-4 w-4" />
            {busy === "sync" ? "Syncing..." : "Sync Services and Staff"}
          </Button>
        </div>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
        <Card>
          <div className="flex items-start justify-between gap-3">
            <div>
              <CardTitle>AI booking readiness</CardTitle>
              <CardDescription>
                AI booking stays disabled until Square creates and cancels a real test booking.
              </CardDescription>
            </div>
            <Badge value={aiEnabled ? "active" : "disabled"} />
          </div>
          <div className="mt-5 space-y-3">
            {readiness?.checks.map((step) => (
              <div key={step.key} className="rounded-md border border-line p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3">
                    <CheckCircle2
                      className={step.complete ? "h-5 w-5 text-brand" : "h-5 w-5 text-slate-300"}
                    />
                    <span className="text-sm font-medium text-ink">{step.label}</span>
                  </div>
                  <Badge value={step.complete ? "active" : "disabled"} />
                </div>
                {!step.complete && step.message ? (
                  <div className="mt-2 text-xs leading-5 text-muted">{step.message}</div>
                ) : null}
              </div>
            ))}
          </div>
          <div className="mt-5 flex flex-wrap gap-3">
            {aiEnabled ? (
              <Button type="button" variant="danger" onClick={() => void disableAI()} disabled={busy !== ""}>
                <PowerOff className="h-4 w-4" />
                {busy === "disable" ? "Disabling..." : "Disable AI Booking"}
              </Button>
            ) : (
              <Button type="button" onClick={() => void enableAI()} disabled={!canEnable}>
                <Power className="h-4 w-4" />
                {busy === "enable" ? "Enabling..." : "Enable AI Booking"}
              </Button>
            )}
          </div>
        </Card>

        <Card>
          <CardTitle>Test booking</CardTitle>
          <CardDescription>
            Create one Square test booking, then cancel it before enabling AI booking.
          </CardDescription>

          <div className="mt-5 grid gap-4 md:grid-cols-2">
            <Field label="Service">
              <select
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
                value={form.service_id}
                onChange={(event) => setForm((current) => ({ ...current, service_id: event.target.value }))}
                disabled={bookableServices.length === 0 || busy !== ""}
              >
                {bookableServices.length === 0 ? <option value="">No bookable services</option> : null}
                {bookableServices.map((service) => (
                  <option key={service.id} value={service.id}>
                    {service.name} ({service.duration_minutes} min)
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Staff">
              <select
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
                value={form.staff_id}
                onChange={(event) => setForm((current) => ({ ...current, staff_id: event.target.value }))}
                disabled={bookableStaff.length === 0 || busy !== ""}
              >
                {bookableStaff.length === 0 ? <option value="">No bookable staff</option> : null}
                {bookableStaff.map((member) => (
                  <option key={member.id} value={member.id}>
                    {member.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Start time">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="datetime-local"
                value={form.start_time}
                onChange={(event) => setForm((current) => ({ ...current, start_time: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Customer phone">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={form.customer_phone}
                onChange={(event) => setForm((current) => ({ ...current, customer_phone: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Customer name">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={form.customer_name}
                onChange={(event) => setForm((current) => ({ ...current, customer_name: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Customer email">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={form.customer_email}
                onChange={(event) => setForm((current) => ({ ...current, customer_email: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <div className="md:col-span-2">
              <Field label="Notes">
                <textarea
                  className="min-h-20 w-full rounded-md border border-line px-3 py-2 text-sm text-ink"
                  value={form.notes}
                  onChange={(event) => setForm((current) => ({ ...current, notes: event.target.value }))}
                  disabled={busy !== ""}
                />
              </Field>
            </div>
          </div>

          <div className="mt-5 flex flex-wrap gap-3">
            <Button type="button" onClick={() => void createTestBooking()} disabled={!canCreateTest}>
              <CalendarCheck className="h-4 w-4" />
              {busy === "test" ? "Creating..." : "Create Test Booking"}
            </Button>
            <Button type="button" variant="danger" onClick={() => void cancelTestBooking()} disabled={!canCancelTest}>
              <Ban className="h-4 w-4" />
              {busy === "cancel-test" ? "Cancelling..." : "Cancel Test Booking"}
            </Button>
          </div>

          <LatestTest latest={latestTest} />
        </Card>
      </div>

      <Card>
        <CardTitle>Recent sync logs</CardTitle>
        <CardDescription>Provider sync activity and failures are stored for troubleshooting.</CardDescription>
        <div className="mt-5 overflow-x-auto rounded-md border border-line">
          <table className="w-full min-w-[680px] text-left text-sm">
            <thead className="bg-slate-50 text-xs uppercase text-muted">
              <tr>
                <th className="px-4 py-3">Type</th>
                <th className="px-4 py-3">Status</th>
                <th className="px-4 py-3">Started</th>
                <th className="px-4 py-3">Message</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line bg-white">
              {status?.sync_logs?.length ? (
                status.sync_logs.map((log) => (
                  <tr key={log.id}>
                    <td className="px-4 py-3 font-medium text-ink">{log.sync_type}</td>
                    <td className="px-4 py-3">
                      <Badge value={log.status === "succeeded" ? "active" : log.status} />
                    </td>
                    <td className="px-4 py-3 text-muted">{new Date(log.started_at).toLocaleString()}</td>
                    <td className="px-4 py-3 text-muted">{log.message || "-"}</td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td className="px-4 py-8 text-center text-muted" colSpan={4}>
                    No sync logs yet.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  );
}

function LatestTest({ latest }: { latest?: TestBookingRecord }) {
  if (!latest) {
    return (
      <div className="mt-5 rounded-md border border-line p-4 text-sm text-muted">
        No Square test booking has been created yet.
      </div>
    );
  }

  const failed = latest.status === "fallback_pending" || latest.error_code;
  return (
    <div className="mt-5 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Latest test</div>
          <div className="mt-1 text-sm text-muted">
            {latest.start_time ? new Date(latest.start_time).toLocaleString() : "No time recorded"}
          </div>
        </div>
        <Badge value={latest.appointment_status || latest.status} />
      </div>
      <div className="mt-4 grid gap-3 text-sm md:grid-cols-2">
        <Info label="POS booking" value={latest.pos_booking_id || "Not returned"} />
        <Info label="Appointment" value={latest.appointment_id || "Not created"} />
      </div>
      {failed ? (
        <div className="mt-4 flex gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
          <XCircle className="mt-0.5 h-4 w-4 flex-none" />
          <div>
            <div className="font-semibold">{latest.error_code || "Test booking failed"}</div>
            <div className="mt-1">{latest.error_message || "Review Square and try again."}</div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</span>
      <div className="mt-1">{children}</div>
    </label>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 break-words text-sm font-medium text-ink">{value}</div>
    </div>
  );
}

function firstBookableService(items: POSService[]) {
  return items.find(
    (service) =>
      service.id &&
      service.active &&
      service.ai_bookable &&
      service.pos_service_version &&
      service.duration_minutes > 0
  );
}

function firstBookableStaff(items: POSStaffMember[]) {
  return items.find((member) => member.id && member.active && member.ai_bookable);
}

function nextTestStartTime() {
  const date = new Date();
  date.setDate(date.getDate() + 1);
  date.setHours(10, 0, 0, 0);
  const offset = date.getTimezoneOffset();
  const local = new Date(date.getTime() - offset * 60 * 1000);
  return local.toISOString().slice(0, 16);
}
