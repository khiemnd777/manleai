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
  AvailabilityResult,
  AvailabilitySlot,
  POSConnection,
  POSLocation,
  ProviderSwitchMatch,
  ProviderSwitchRun,
  ProviderSwitchReadiness,
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

type ProviderSwitchRunResponse = {
  run?: ProviderSwitchRun | null;
};

type ProviderSwitchMatchUpdateResponse = {
  run?: ProviderSwitchRun | null;
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
  start_time: "",
  customer_name: "ManleAI Test Customer",
  customer_phone: "+13125550199",
  customer_email: "",
  notes: "AI booking readiness test. Cancel after verifying Square booking creation."
};

export function SquareIntegration() {
  const [salons, setSalons] = useState<Salon[]>([]);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [switchReadiness, setSwitchReadiness] = useState<ProviderSwitchReadiness | null>(null);
  const [switchRun, setSwitchRun] = useState<ProviderSwitchRun | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [locations, setLocations] = useState<POSLocation[]>([]);
  const [selectedLocationID, setSelectedLocationID] = useState("");
  const [form, setForm] = useState<TestBookingForm>(defaultForm);
  const [bookingDate, setBookingDate] = useState("");
  const [availabilityResult, setAvailabilityResult] = useState<AvailabilityResult | null>(null);
  const [availabilityError, setAvailabilityError] = useState("");
  const [availabilityChecked, setAvailabilityChecked] = useState(false);
  const [checkingAvailability, setCheckingAvailability] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const salon = salons[0];
  const connection = status?.connection;
  const readiness = status?.readiness;
  const latestTest = readiness?.latest_test_booking;
  const selectedLocation =
    locations.find((location) => location.id === (connection?.location_id || selectedLocationID)) ??
    locations.find((location) => location.id === selectedLocationID);
  const squareTimezone = selectedLocation?.timezone || salon?.timezone || "";
  const displayTimezone = squareTimezone || availabilityResult?.timezone || salon?.timezone || undefined;
  const timezoneMismatch =
    Boolean(selectedLocation?.timezone) && Boolean(salon?.timezone) && selectedLocation?.timezone !== salon?.timezone;

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      setSalons(salonResponse.salons);
      const firstSalon = salonResponse.salons[0];
      if (!firstSalon) {
        setStatus(null);
        setSwitchReadiness(null);
        setSwitchRun(null);
        setServices([]);
        setStaff([]);
        setLocations([]);
        return;
      }

      const [squareStatus, providerSwitchResponse, providerSwitchRunResponse, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<ProviderSwitchReadiness>(`/api/salons/${firstSalon.id}/pos/provider-switch-readiness`),
        apiRequest<ProviderSwitchRunResponse>(`/api/salons/${firstSalon.id}/pos/provider-switch-runs/latest`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(squareStatus);
      setSwitchReadiness(providerSwitchResponse);
      setSwitchRun(providerSwitchRunResponse.run ?? null);
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
        staff_id: current.staff_id || firstBookableStaff(staffResponse.staff)?.id || "",
        start_time: ""
      }));
      clearAvailability();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load integrations.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    if (!bookingDate && displayTimezone) {
      setBookingDate(nextBookingDate(displayTimezone));
    }
  }, [bookingDate, displayTimezone]);

  const bookableServices = useMemo(
    () => services.filter(serviceIsBookable),
    [services]
  );
  const bookableStaff = useMemo(
    () => staff.filter(staffIsBookable),
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

  async function checkAvailability() {
    if (!salon || !form.service_id || !form.staff_id || !bookingDate) return;
    setAvailabilityError("");
    setAvailabilityChecked(true);
    setCheckingAvailability(true);
    setForm((current) => ({ ...current, start_time: "" }));
    try {
      const result = await apiRequest<AvailabilityResult>(`/api/salons/${salon.id}/availability`, {
        method: "POST",
        body: JSON.stringify({
          service_id: form.service_id,
          staff_id: form.staff_id,
          preferred_date: bookingDate,
          limit: 20
        })
      });
      setAvailabilityResult(result);
    } catch (err) {
      setAvailabilityResult(null);
      setAvailabilityError(err instanceof Error ? err.message : "Could not check Square Appointments availability.");
    } finally {
      setCheckingAvailability(false);
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
          start_time: form.start_time
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

  async function updateSwitchMatch(matchID: string, matchStatus: string) {
    if (!salon || !switchRun) return;
    setBusy(`switch-match:${matchID}:${matchStatus}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ProviderSwitchMatchUpdateResponse>(
        `/api/salons/${salon.id}/pos/provider-switch-runs/${switchRun.id}/matches/${matchID}`,
        {
          method: "PATCH",
          body: JSON.stringify({ match_status: matchStatus })
        }
      );
      setSwitchRun(response.run ?? null);
      setSuccess("Provider switch match updated.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not update provider switch match.");
    } finally {
      setBusy("");
    }
  }

  function applyReadiness(next: SquareReadiness) {
    setStatus((current) => (current ? { ...current, readiness: next } : current));
  }

  function clearAvailability() {
    setAvailabilityResult(null);
    setAvailabilityError("");
    setAvailabilityChecked(false);
  }

  function updateService(value: string) {
    setForm((current) => ({ ...current, service_id: value, start_time: "" }));
    clearAvailability();
  }

  function updateStaff(value: string) {
    setForm((current) => ({ ...current, staff_id: value, start_time: "" }));
    clearAvailability();
  }

  function updateBookingDate(value: string) {
    setBookingDate(value);
    setForm((current) => ({ ...current, start_time: "" }));
    clearAvailability();
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
  const canCheckAvailability =
    Boolean(form.service_id) && Boolean(form.staff_id) && Boolean(bookingDate) && busy === "" && !checkingAvailability;

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

      <ProviderSwitchReadinessPanel
        readiness={switchReadiness}
        switchRun={switchRun}
        busy={busy}
        onRefresh={() => void load()}
        onUpdateMatch={(matchID, matchStatus) => void updateSwitchMatch(matchID, matchStatus)}
      />

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
                onChange={(event) => updateService(event.target.value)}
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
                onChange={(event) => updateStaff(event.target.value)}
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
            <Field label="Booking date">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="date"
                value={bookingDate}
                onChange={(event) => updateBookingDate(event.target.value)}
                disabled={busy !== "" || checkingAvailability}
              />
            </Field>
            <div className="md:col-span-2">
              <div className="rounded-md border border-line p-3">
                <div className="grid gap-3 md:grid-cols-2">
                  <Info label="Square timezone" value={squareTimezone || "Not loaded"} />
                  <Info label="Salon timezone" value={salon.timezone || "Not configured"} />
                </div>
                {timezoneMismatch ? (
                  <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
                    Square location timezone and salon profile timezone differ. Slots below are shown in Square location time.
                  </div>
                ) : null}
                <div className="mt-3">
                  <Button type="button" variant="secondary" onClick={() => void checkAvailability()} disabled={!canCheckAvailability}>
                    <RefreshCcw className="h-4 w-4" />
                    {checkingAvailability ? "Checking..." : "Check Square Availability"}
                  </Button>
                </div>
              </div>
            </div>
            <div className="md:col-span-2">
              <AvailabilityPicker
                checked={availabilityChecked}
                error={availabilityError}
                loading={checkingAvailability}
                selectedStartTime={form.start_time}
                slots={availabilityResult?.slots ?? []}
                timezone={displayTimezone}
                onSelect={(slot) => setForm((current) => ({ ...current, start_time: slot.start_time }))}
              />
            </div>
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

function ProviderSwitchReadinessPanel({
  busy,
  onRefresh,
  onUpdateMatch,
  readiness,
  switchRun
}: {
  busy: string;
  onRefresh: () => void;
  onUpdateMatch: (matchID: string, matchStatus: string) => void;
  readiness: ProviderSwitchReadiness | null;
  switchRun: ProviderSwitchRun | null;
}) {
  if (!readiness) {
    return (
      <Card>
        <CardTitle>Provider switch readiness</CardTitle>
        <CardDescription>Provider switch checks load after a salon and active POS provider are available.</CardDescription>
      </Card>
    );
  }

  const allProviders = [...readiness.installed_providers, ...readiness.unavailable_providers];
  const workflowSteps = providerSwitchWorkflowSteps(readiness, switchRun);
  const matchSummary = switchRun?.match_summary;
  const workflowBlockedReason =
    switchRun?.blocked_reason ||
    readiness.blocked_reason ||
    "No alternate POS adapter is installed in this pilot deployment.";
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-start">
        <div>
          <CardTitle>Provider switch readiness</CardTitle>
          <CardDescription>
            Keep canonical services, staff, and customers safe before any future POS provider switch.
          </CardDescription>
        </div>
        <Badge value={readiness.can_start_switch ? "ready" : "disabled"} />
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-5">
        <Info label="Current provider" value={readiness.active_provider_label || readiness.active_provider} />
        <Info label="Bookable services" value={String(readiness.mapping.bookable_service_count)} />
        <Info label="Bookable staff" value={String(readiness.mapping.bookable_staff_count)} />
        <Info label="Customer links" value={String(readiness.mapping.linked_customer_count)} />
        <Info label="Unmapped records" value={String(readiness.mapping.unmapped_service_count + readiness.mapping.unmapped_staff_count)} />
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-[0.95fr_1.05fr]">
        <div className="rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Available adapters</div>
          <div className="mt-3 space-y-3">
            {allProviders.map((provider) => (
              <div key={provider.provider || provider.label} className="rounded-md border border-line p-3">
                <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
                  <div>
                    <div className="text-sm font-medium text-ink">{provider.label}</div>
                    {provider.blocked_reason ? <div className="mt-1 text-xs leading-5 text-muted">{provider.blocked_reason}</div> : null}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Badge value={provider.active ? "active" : provider.installed ? "installed" : "disabled"} />
                    <Badge value={provider.status || "not_connected"} />
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Readiness checks</div>
          <div className="mt-3 space-y-3">
            {readiness.checks.map((check) => (
              <div key={check.key} className="rounded-md border border-line p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex min-w-0 gap-3">
                    <CheckCircle2 className={check.complete ? "mt-0.5 h-5 w-5 flex-none text-brand" : "mt-0.5 h-5 w-5 flex-none text-slate-300"} />
                    <div>
                      <div className="text-sm font-medium text-ink">{check.label}</div>
                      {!check.complete && check.message ? <div className="mt-1 text-xs leading-5 text-muted">{check.message}</div> : null}
                    </div>
                  </div>
                  <Badge value={check.complete ? "active" : "disabled"} />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-5 rounded-md border border-line p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">Switch workflow</div>
            <div className="mt-1 text-xs leading-5 text-muted">
              {switchRun
                ? `Latest run targets ${switchRun.to_provider}. Activation remains disabled until a real alternate adapter and required mappings are ready.`
                : "No switch run has been created. The workflow is blocked until an alternate native POS adapter is installed."}
            </div>
          </div>
          <Badge value={switchRun?.status ?? "blocked"} />
        </div>

        <div className="mt-4 grid gap-3 md:grid-cols-3">
          <Info label="Suggested matches" value={String(matchSummary?.suggested ?? 0)} />
          <Info label="Conflicts" value={String(matchSummary?.conflicts ?? 0)} />
          <Info label="Unmatched" value={String(matchSummary?.unmatched ?? 0)} />
        </div>

        <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {workflowSteps.map((step) => (
            <div key={step.key} className="rounded-md border border-line p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-ink">{step.label}</div>
                  <div className="mt-1 text-xs leading-5 text-muted">{step.message}</div>
                </div>
                <Badge value={step.complete ? "active" : step.status} />
              </div>
            </div>
          ))}
        </div>

        {workflowBlockedReason ? (
          <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
            {workflowBlockedReason}
          </div>
        ) : null}
      </div>

      <ProviderSwitchMatchReview
        busy={busy}
        readiness={readiness}
        switchRun={switchRun}
        onUpdateMatch={onUpdateMatch}
      />

      {readiness.blocked_reason ? (
        <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900">
          {readiness.blocked_reason}
        </div>
      ) : null}

      <div className="mt-5 flex flex-wrap gap-3">
        <Button type="button" variant="secondary" onClick={onRefresh} disabled={busy !== ""}>
          <RefreshCcw className="h-4 w-4" />
          Refresh readiness
        </Button>
        <Button type="button" disabled>
          Start switch run
        </Button>
      </div>
    </Card>
  );
}

function ProviderSwitchMatchReview({
  busy,
  onUpdateMatch,
  readiness,
  switchRun
}: {
  busy: string;
  onUpdateMatch: (matchID: string, matchStatus: string) => void;
  readiness: ProviderSwitchReadiness;
  switchRun: ProviderSwitchRun | null;
}) {
  const matches = switchRun?.matches ?? [];
  const actionsDisabled = busy !== "" || !readiness.can_start_switch || !switchRun || switchRun.status === "blocked";
  const disabledMessage = !readiness.can_start_switch
    ? "Match actions are gated until an alternate native POS adapter is installed."
    : switchRun?.status === "blocked"
      ? "Blocked switch runs cannot be edited."
      : "";

  return (
    <div className="mt-5 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Match review</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            Review provider records before any future provider activation. Confirming a match does not activate a POS provider.
          </div>
        </div>
        <Badge value={matches.length > 0 ? "needs_review" : "disabled"} />
      </div>

      {disabledMessage ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
          {disabledMessage}
        </div>
      ) : null}

      {matches.length === 0 ? (
        <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">
          No switch matches to review.
        </div>
      ) : (
        <>
          <div className="mt-4 hidden overflow-x-auto rounded-md border border-line md:block">
            <table className="w-full min-w-[880px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Provider record</th>
                  <th className="px-4 py-3">Canonical match</th>
                  <th className="px-4 py-3">Confidence</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Reason</th>
                  <th className="px-4 py-3">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {matches.map((match) => (
                  <tr key={match.id}>
                    <td className="px-4 py-3">
                      <Badge value={match.entity_type} />
                    </td>
                    <td className="px-4 py-3 font-medium text-ink">{providerSwitchProviderLabel(match)}</td>
                    <td className="px-4 py-3 text-muted">{match.canonical_name || "No canonical match"}</td>
                    <td className="px-4 py-3 text-muted">{match.match_confidence}%</td>
                    <td className="px-4 py-3">
                      <Badge value={match.match_status} />
                    </td>
                    <td className="px-4 py-3 text-muted">{match.match_reason || "-"}</td>
                    <td className="px-4 py-3">
                      <ProviderSwitchMatchActions
                        busy={busy}
                        disabled={actionsDisabled}
                        match={match}
                        onUpdateMatch={onUpdateMatch}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-4 space-y-3 md:hidden">
            {matches.map((match) => (
              <div key={match.id} className="rounded-md border border-line p-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-ink">{providerSwitchProviderLabel(match)}</div>
                    <div className="mt-1 text-xs text-muted">{match.canonical_name || "No canonical match"}</div>
                  </div>
                  <Badge value={match.match_status} />
                </div>
                <div className="mt-3 grid gap-2 text-sm">
                  <Info label="Type" value={match.entity_type} />
                  <Info label="Confidence" value={`${match.match_confidence}%`} />
                  <Info label="Reason" value={match.match_reason || "-"} />
                </div>
                <div className="mt-3">
                  <ProviderSwitchMatchActions
                    busy={busy}
                    disabled={actionsDisabled}
                    match={match}
                    onUpdateMatch={onUpdateMatch}
                  />
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function ProviderSwitchMatchActions({
  busy,
  disabled,
  match,
  onUpdateMatch
}: {
  busy: string;
  disabled: boolean;
  match: ProviderSwitchMatch;
  onUpdateMatch: (matchID: string, matchStatus: string) => void;
}) {
  const matchBusy = busy.startsWith(`switch-match:${match.id}:`);
  return (
    <div className="flex flex-wrap gap-2">
      <Button
        type="button"
        variant="secondary"
        onClick={() => onUpdateMatch(match.id, "confirmed")}
        disabled={disabled || !match.canonical_entity_id || match.match_status === "confirmed"}
      >
        <CheckCircle2 className="h-4 w-4" />
        {matchBusy && busy.endsWith(":confirmed") ? "Saving..." : "Confirm"}
      </Button>
      <Button
        type="button"
        variant="secondary"
        onClick={() => onUpdateMatch(match.id, "unmatched")}
        disabled={disabled || match.match_status === "unmatched"}
      >
        <XCircle className="h-4 w-4" />
        {matchBusy && busy.endsWith(":unmatched") ? "Saving..." : "Mark unmatched"}
      </Button>
      <Button
        type="button"
        variant="secondary"
        onClick={() => onUpdateMatch(match.id, "skipped")}
        disabled={disabled || match.match_status === "skipped"}
      >
        <Ban className="h-4 w-4" />
        {matchBusy && busy.endsWith(":skipped") ? "Saving..." : "Skip"}
      </Button>
    </div>
  );
}

function providerSwitchProviderLabel(match: ProviderSwitchMatch) {
  const details = [];
  if (match.provider_duration_minutes) {
    details.push(`${match.provider_duration_minutes} min`);
  }
  if (match.provider_phone) {
    details.push(match.provider_phone);
  }
  if (match.provider_email) {
    details.push(match.provider_email);
  }
  return details.length ? `${match.provider_name} (${details.join(", ")})` : match.provider_name;
}

function providerSwitchWorkflowSteps(readiness: ProviderSwitchReadiness, switchRun: ProviderSwitchRun | null) {
  const summary = switchRun?.match_summary;
  const hasMatches = Boolean(summary && summary.total > 0);
  const hasUnresolved = Boolean(summary && (summary.conflicts > 0 || summary.unmatched > 0));
  return [
    {
      key: "active_provider_source",
      label: "Active provider source",
      complete: readiness.active_provider !== "",
      status: "disabled",
      message: readiness.active_provider_label || readiness.active_provider || "No active provider selected."
    },
    {
      key: "alternate_adapter",
      label: "Connect alternate provider",
      complete: readiness.can_start_switch,
      status: "blocked",
      message: readiness.can_start_switch
        ? "Alternate adapter is installed."
        : "Square Appointments is the only native POS adapter in this pilot."
    },
    {
      key: "import_records",
      label: "Import provider records",
      complete: hasMatches,
      status: switchRun ? "pending" : "blocked",
      message: hasMatches ? `${summary?.total ?? 0} imported records have match candidates.` : "No imported records from an alternate provider."
    },
    {
      key: "auto_match",
      label: "Auto-match records",
      complete: hasMatches,
      status: switchRun ? "pending" : "blocked",
      message: hasMatches
        ? `${summary?.suggested ?? 0} suggested, ${summary?.conflicts ?? 0} conflicts, ${summary?.unmatched ?? 0} unmatched.`
        : "Matching starts after provider import."
    },
    {
      key: "resolve_conflicts",
      label: "Resolve conflicts",
      complete: hasMatches && !hasUnresolved,
      status: hasMatches ? "pending" : "blocked",
      message: hasMatches && !hasUnresolved ? "No unresolved matches remain." : "Manual conflict resolution is not enabled in this slice."
    },
    {
      key: "dry_run",
      label: "Dry-run booking",
      complete: Boolean(switchRun?.dry_run_ready),
      status: readiness.dry_run_booking_ready ? "pending" : "blocked",
      message: readiness.dry_run_booking_ready
        ? "Current provider is ready; alternate-provider dry-run is still gated."
        : "Current provider must be synced with bookable service and staff links first."
    },
    {
      key: "activate_provider",
      label: "Activate provider",
      complete: Boolean(switchRun?.can_activate && readiness.can_activate_provider),
      status: "blocked",
      message: "Activation stays disabled until a real adapter, mappings, and dry-run are complete."
    }
  ];
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

function AvailabilityPicker({
  checked,
  error,
  loading,
  onSelect,
  selectedStartTime,
  slots,
  timezone
}: {
  checked: boolean;
  error: string;
  loading: boolean;
  onSelect: (slot: AvailabilitySlot) => void;
  selectedStartTime: string;
  slots: AvailabilitySlot[];
  timezone?: string;
}) {
  if (error) {
    return <Alert title="Availability check failed" message={error} />;
  }
  if (loading) {
    return (
      <div className="rounded-md border border-line p-4 text-sm text-muted">
        Checking Square Appointments availability...
      </div>
    );
  }
  if (!checked) {
    return (
      <div className="rounded-md border border-line p-4 text-sm text-muted">
        Check Square availability to select a real bookable slot.
      </div>
    );
  }
  if (slots.length === 0) {
    return (
      <div className="rounded-md border border-line p-4 text-sm text-muted">
        No Square slots returned for this service, staff, and date.
      </div>
    );
  }

  const selected = slots.find((slot) => slot.start_time === selectedStartTime);
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Available slots</div>
          <div className="mt-1 text-xs text-muted">
            Times are shown in {timezone || "the selected location timezone"}.
          </div>
        </div>
        {selected ? <Badge value="selected" /> : null}
      </div>
      <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
        {slots.map((slot) => {
          const active = slot.start_time === selectedStartTime;
          return (
            <button
              key={`${slot.start_time}-${slot.staff_id ?? ""}`}
              type="button"
              onClick={() => onSelect(slot)}
              className={`min-h-10 rounded-md border px-3 py-2 text-left text-sm font-medium transition ${
                active
                  ? "border-brand bg-emerald-50 text-brand"
                  : "border-line bg-white text-ink hover:border-brand hover:bg-emerald-50"
              }`}
            >
              {formatTime(slot.start_time, timezone)}
            </button>
          );
        })}
      </div>
      {selected ? (
        <div className="mt-4 rounded-md bg-slate-50 p-3 text-sm text-ink">
          Selected: {formatDate(selected.start_time, timezone)} {formatTimeRange(selected.start_time, selected.end_time, timezone)}
        </div>
      ) : (
        <div className="mt-4 text-sm text-muted">Select one Square slot before creating the test booking.</div>
      )}
    </div>
  );
}

function firstBookableService(items: POSService[]) {
  return items.find(serviceIsBookable);
}

function serviceIsBookable(service: POSService) {
  return (
    Boolean(service.id) &&
    service.active &&
    service.ai_bookable &&
    service.sync_status === "synced" &&
    service.pos_linked &&
    Boolean(service.pos_service_id) &&
    Boolean(service.pos_service_version) &&
    service.duration_minutes > 0
  );
}

function firstBookableStaff(items: POSStaffMember[]) {
  return items.find(staffIsBookable);
}

function staffIsBookable(member: POSStaffMember) {
  return (
    Boolean(member.id) &&
    member.active &&
    member.ai_bookable &&
    member.sync_status === "synced" &&
    member.pos_linked &&
    Boolean(member.pos_staff_id)
  );
}

function nextBookingDate(timezone?: string) {
  const date = new Date();
  date.setDate(date.getDate() + 1);
  return formatDateInput(date, timezone);
}

function formatDate(value: string, timezone?: string) {
  return new Date(value).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: timezone
  });
}

function formatTime(value: string, timezone?: string) {
  return new Date(value).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timezone
  });
}

function formatTimeRange(start: string, end: string, timezone?: string) {
  return `${formatTime(start, timezone)} - ${formatTime(end, timezone)}`;
}

function formatDateInput(date: Date, timezone?: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    day: "2-digit",
    month: "2-digit",
    timeZone: timezone,
    year: "numeric"
  }).formatToParts(date);
  const values = new Map(parts.map((part) => [part.type, part.value]));
  return `${values.get("year")}-${values.get("month")}-${values.get("day")}`;
}
