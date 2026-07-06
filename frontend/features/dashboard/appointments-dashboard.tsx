"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  AlertTriangle,
  CalendarClock,
  CalendarPlus,
  CalendarSearch,
  ChevronLeft,
  ChevronRight,
  ClipboardList,
  RefreshCcw,
  X
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  assignedTechniciansLabel,
  bookingSummaryLabel,
  orderedSegments,
  serviceNamesLabel,
  technicianPreferenceLabel,
  technicianPreferenceValue
} from "@/features/dashboard/booking-display";
import { apiRequest } from "@/lib/api/client";
import type {
  AvailabilityResult,
  AvailabilitySlot,
  AppointmentRecord,
  BookingAttempt,
  BookingSegmentRequest,
  POSConnection,
  POSService,
  POSStaffMember,
  Salon,
  StaffSelectionMode,
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
  limit?: number;
  offset?: number;
  has_more?: boolean;
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

type AppointmentActionMode = "create" | "reschedule" | "cancel";

type AppointmentActionForm = {
  customerName: string;
  customerPhone: string;
  customerEmail: string;
  serviceID: string;
  staffID: string;
  preferredDate: string;
  selectedSlotKey: string;
  notes: string;
  cancelReason: string;
};

type ActionNotice = {
  tone: "success" | "warning";
  title: string;
  message: string;
};

const defaultAppointmentPageSize = 10;
const appointmentPageSizeOptions = [10, 25, 50] as const;
const appointmentOverviewLimit = 200;

export function AppointmentsDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [appointments, setAppointments] = useState<AppointmentRecord[]>([]);
  const [appointmentRows, setAppointmentRows] = useState<AppointmentRecord[]>([]);
  const [appointmentLimit, setAppointmentLimit] = useState(defaultAppointmentPageSize);
  const [appointmentOffset, setAppointmentOffset] = useState(0);
  const [appointmentHasMore, setAppointmentHasMore] = useState(false);
  const [appointmentOverviewHasMore, setAppointmentOverviewHasMore] = useState(false);
  const [attempts, setAttempts] = useState<BookingAttempt[]>([]);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [selectedDate, setSelectedDate] = useState(() => formatDateInput(new Date()));
  const [availabilityServiceID, setAvailabilityServiceID] = useState("");
  const [availabilityStaffID, setAvailabilityStaffID] = useState("");
  const [availabilityResult, setAvailabilityResult] = useState<AvailabilityResult | null>(null);
  const [checkingAvailability, setCheckingAvailability] = useState(false);
  const [availabilityError, setAvailabilityError] = useState("");
  const [availabilityChecked, setAvailabilityChecked] = useState(false);
  const [loading, setLoading] = useState(true);
  const [appointmentListLoading, setAppointmentListLoading] = useState(false);
  const [error, setError] = useState("");
  const [actionMode, setActionMode] = useState<AppointmentActionMode | null>(null);
  const [selectedAppointment, setSelectedAppointment] = useState<AppointmentRecord | null>(null);
  const [actionForm, setActionForm] = useState<AppointmentActionForm>(() =>
    emptyActionForm(formatDateInput(new Date()))
  );
  const [actionAvailabilityResult, setActionAvailabilityResult] = useState<AvailabilityResult | null>(null);
  const [actionAvailabilityChecked, setActionAvailabilityChecked] = useState(false);
  const [checkingActionAvailability, setCheckingActionAvailability] = useState(false);
  const [actionAvailabilityError, setActionAvailabilityError] = useState("");
  const [savingAction, setSavingAction] = useState(false);
  const [actionError, setActionError] = useState("");
  const [actionNotice, setActionNotice] = useState<ActionNotice | null>(null);

  function appointmentListPath(salonID: string, limit: number, offset: number) {
    const params = new URLSearchParams({
      limit: String(limit),
      offset: String(offset)
    });
    return `/api/salons/${salonID}/appointments?${params.toString()}`;
  }

  async function fetchAppointmentPage(salonID: string, limit: number, offset: number) {
    return apiRequest<AppointmentsResponse>(appointmentListPath(salonID, limit, offset));
  }

  async function fetchAppointmentPageWithFallback(salonID: string, limit: number, offset: number) {
    let response = await fetchAppointmentPage(salonID, limit, offset);
    if (response.appointments.length === 0 && offset > 0) {
      const previousOffset = Math.max(0, offset - limit);
      response = await fetchAppointmentPage(salonID, limit, previousOffset);
    }
    return response;
  }

  function applyAppointmentPage(response: AppointmentsResponse, requestedLimit: number, requestedOffset: number) {
    setAppointmentRows(response.appointments);
    setAppointmentLimit(response.limit ?? requestedLimit);
    setAppointmentOffset(response.offset ?? requestedOffset);
    setAppointmentHasMore(Boolean(response.has_more));
  }

  async function reloadAppointmentRows(salonID: string, offset = appointmentOffset, limit = appointmentLimit) {
    setAppointmentListLoading(true);
    try {
      const response = await fetchAppointmentPageWithFallback(salonID, limit, offset);
      applyAppointmentPage(response, limit, offset);
    } finally {
      setAppointmentListLoading(false);
    }
  }

  async function load({
    silent = false,
    offset = appointmentOffset,
    limit = appointmentLimit
  }: { silent?: boolean; offset?: number; limit?: number } = {}) {
    setError("");
    if (!silent) {
      setLoading(true);
    } else {
      setAppointmentListLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setAppointments([]);
        setAppointmentRows([]);
        setAppointmentHasMore(false);
        setAppointmentOverviewHasMore(false);
        setAttempts([]);
        setServices([]);
        setStaff([]);
        return;
      }

      const [statusResponse, appointmentResponse, attemptResponse, serviceResponse, staffResponse] =
        await Promise.all([
          apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
          fetchAppointmentPageWithFallback(firstSalon.id, limit, offset),
          apiRequest<AttemptsResponse>(`/api/salons/${firstSalon.id}/booking-attempts?limit=50`),
          apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
          apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
        ]);
      const overviewResponse = await fetchAppointmentPage(firstSalon.id, appointmentOverviewLimit, 0);
      setStatus(statusResponse);
      applyAppointmentPage(appointmentResponse, limit, offset);
      setAppointments(overviewResponse.appointments);
      setAppointmentOverviewHasMore(Boolean(overviewResponse.has_more));
      setAttempts(attemptResponse.booking_attempts);
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);
      setAvailabilityServiceID((current) => current || firstBookableServiceID(serviceResponse.services));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load appointment data.");
    } finally {
      if (!silent) {
        setLoading(false);
      } else {
        setAppointmentListLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const serviceNames = useMemo(
    () => new Map(services.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [services]
  );
  const staffNames = useMemo(
    () => new Map(staff.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [staff]
  );
  const bookableServices = useMemo(
    () => services.filter(serviceIsBookable),
    [services]
  );
  const bookableStaff = useMemo(
    () => staff.filter(staffIsBookable),
    [staff]
  );
  const pendingRequests = useMemo(
    () => attempts.filter((item) => item.status === "fallback_pending"),
    [attempts]
  );
  const dayAppointments = useMemo(
    () =>
      appointments
        .filter((item) => sameDateInput(item.start_time, selectedDate, salon?.timezone))
        .sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime()),
    [appointments, selectedDate, salon?.timezone]
  );
  const dayPendingRequests = useMemo(
    () =>
      pendingRequests
        .filter((item) => sameDateInput(item.requested_start_time, selectedDate, salon?.timezone))
        .sort((a, b) => new Date(a.requested_start_time).getTime() - new Date(b.requested_start_time).getTime()),
    [pendingRequests, selectedDate, salon?.timezone]
  );
  const upcomingCount = useMemo(() => {
    const now = Date.now();
    return appointmentCountLabel(
      appointments.filter((item) => item.status !== "cancelled" && new Date(item.start_time).getTime() >= now)
        .length,
      appointmentOverviewHasMore
    );
  }, [appointments, appointmentOverviewHasMore]);
  const confirmedCount = appointmentCountLabel(
    appointments.filter((item) => item.status === "confirmed").length,
    appointmentOverviewHasMore
  );
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
  const bookingDataReady = bookingPathReady(status);
  const bookingWriteReady = bookingDataReady && !status?.readiness?.booking_write_blocked;
  const readyForAvailability = bookingDataReady && bookableServices.length > 0;
  const readyForManualBooking = bookingWriteReady && bookableServices.length > 0 && bookableStaff.length > 0;
  const selectedActionSlot = useMemo(
    () => (actionAvailabilityResult?.slots ?? []).find((slot) => slotKey(slot) === actionForm.selectedSlotKey) ?? null,
    [actionAvailabilityResult, actionForm.selectedSlotKey]
  );

  async function checkAvailability() {
    if (!salon || !availabilityServiceID || !selectedDate || !readyForAvailability) return;
    setAvailabilityError("");
    setAvailabilityChecked(true);
    setCheckingAvailability(true);
    try {
      const staffSelectionMode = availabilityStaffID ? "specific" : "anyone";
      const result = await apiRequest<AvailabilityResult>(`/api/salons/${salon.id}/availability`, {
        method: "POST",
        body: JSON.stringify({
          service_id: availabilityServiceID,
          staff_id: availabilityStaffID,
          staff_selection_mode: staffSelectionMode,
          segments: [
            {
              service_id: availabilityServiceID,
              staff_id: availabilityStaffID,
              staff_selection_mode: staffSelectionMode
            }
          ],
          preferred_date: selectedDate,
          limit: 5
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

  function setDateFromShortcut(offsetDays: number) {
    const next = new Date();
    next.setDate(next.getDate() + offsetDays);
    setSelectedDate(formatDateInput(next));
    setAvailabilityResult(null);
    setAvailabilityError("");
    setAvailabilityChecked(false);
  }

  function updateActionForm(patch: Partial<AppointmentActionForm>) {
    setActionForm((current) => ({ ...current, ...patch }));
  }

  function resetActionAvailability() {
    setActionAvailabilityResult(null);
    setActionAvailabilityChecked(false);
    setCheckingActionAvailability(false);
    setActionAvailabilityError("");
    updateActionForm({ selectedSlotKey: "" });
  }

  function updateAppointmentPageSize(limit: number) {
    setAppointmentLimit(limit);
    setAppointmentOffset(0);
    if (salon) {
      void reloadAppointmentRows(salon.id, 0, limit);
    }
  }

  function goToPreviousAppointmentPage() {
    const previousOffset = Math.max(0, appointmentOffset - appointmentLimit);
    setAppointmentOffset(previousOffset);
    if (salon) {
      void reloadAppointmentRows(salon.id, previousOffset, appointmentLimit);
    }
  }

  function goToNextAppointmentPage() {
    if (!appointmentHasMore) return;
    const nextOffset = appointmentOffset + appointmentLimit;
    setAppointmentOffset(nextOffset);
    if (salon) {
      void reloadAppointmentRows(salon.id, nextOffset, appointmentLimit);
    }
  }

  function openCreateBooking() {
    setActionMode("create");
    setSelectedAppointment(null);
    setActionForm({
      ...emptyActionForm(selectedDate),
      serviceID: firstBookableServiceID(services)
    });
    setActionError("");
    setActionNotice(null);
    resetActionAvailability();
  }

  function openReschedule(appointment: AppointmentRecord) {
    setActionMode("reschedule");
    setSelectedAppointment(appointment);
    setActionForm({
      ...emptyActionForm(formatDateInput(new Date(appointment.start_time), salon?.timezone)),
      customerName: appointment.customer_name,
      customerPhone: appointment.customer_phone,
      customerEmail: appointment.customer_email ?? "",
      serviceID: appointmentPrimaryServiceID(appointment),
      staffID: "",
      notes: appointment.notes ?? ""
    });
    setActionError("");
    setActionNotice(null);
    resetActionAvailability();
  }

  function openCancel(appointment: AppointmentRecord) {
    setActionMode("cancel");
    setSelectedAppointment(appointment);
    setActionForm({
      ...emptyActionForm(formatDateInput(new Date(appointment.start_time), salon?.timezone)),
      customerName: appointment.customer_name,
      customerPhone: appointment.customer_phone,
      customerEmail: appointment.customer_email ?? "",
      serviceID: appointmentPrimaryServiceID(appointment),
      staffID: "",
      notes: appointment.notes ?? "",
      cancelReason: ""
    });
    setActionError("");
    setActionNotice(null);
    setActionAvailabilityResult(null);
    setActionAvailabilityChecked(false);
    setActionAvailabilityError("");
  }

  function closeActionPanel() {
    setActionMode(null);
    setSelectedAppointment(null);
    setActionError("");
    resetActionAvailability();
  }

  async function checkActionAvailability() {
    if (!salon || !actionMode || actionMode === "cancel" || !actionForm.preferredDate || !readyForManualBooking) {
      return;
    }
    setActionAvailabilityError("");
    setActionAvailabilityChecked(true);
    setCheckingActionAvailability(true);
    updateActionForm({ selectedSlotKey: "" });
    try {
      const segments = actionAvailabilitySegments(actionMode, actionForm, selectedAppointment);
      if (segments.length === 0 || segments.some((segment) => !segment.service_id)) {
        throw new Error("This appointment is missing service details needed to check availability.");
      }
      const staffSelectionMode = actionMode === "create" && !actionForm.staffID ? "anyone" : "specific";
      const result = await apiRequest<AvailabilityResult>(`/api/salons/${salon.id}/availability`, {
        method: "POST",
        body: JSON.stringify({
          service_id: segments[0].service_id,
          staff_id: actionMode === "create" ? actionForm.staffID : "",
          staff_selection_mode: staffSelectionMode,
          segments,
          preferred_date: actionForm.preferredDate,
          limit: 5
        })
      });
      setActionAvailabilityResult(result);
    } catch (err) {
      setActionAvailabilityResult(null);
      setActionAvailabilityError(
        err instanceof Error ? err.message : "Could not check Square Appointments availability."
      );
    } finally {
      setCheckingActionAvailability(false);
    }
  }

  async function submitCreateBooking() {
    if (!salon || !selectedActionSlot) return;
    setSavingAction(true);
    setActionError("");
    setActionNotice(null);
    try {
      const staffSelectionMode = actionForm.staffID ? "specific" : "anyone";
      const segments = slotBookingSegments(selectedActionSlot, actionForm.serviceID, staffSelectionMode);
      if (segments.length === 0 || segments.some((segment) => !segment.staff_id)) {
        throw new Error("Select a returned Square slot before creating the booking.");
      }
      const attempt = await apiRequest<BookingAttempt>(`/api/salons/${salon.id}/booking-attempts`, {
        method: "POST",
        body: JSON.stringify({
          source: "owner_dashboard",
          customer_name: actionForm.customerName,
          customer_phone: actionForm.customerPhone,
          customer_email: actionForm.customerEmail,
          service_id: segments[0].service_id,
          staff_id: segments[0].staff_id,
          staff_selection_mode: staffSelectionMode,
          segments,
          start_time: selectedActionSlot.start_time,
          notes: actionForm.notes
        })
      });
      if (attempt.status === "fallback_pending") {
        setActionNotice({
          tone: "warning",
          title: "Booking needs owner review",
          message: "Square Appointments did not confirm this booking. A pending request was created instead."
        });
      } else {
        setActionNotice({
          tone: "success",
          title: "Booking confirmed",
          message: "Square Appointments returned a booking ID, so the appointment is confirmed."
        });
      }
      closeActionPanel();
      await load({ silent: true });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not create booking.");
    } finally {
      setSavingAction(false);
    }
  }

  async function submitReschedule() {
    if (!salon || !selectedAppointment || !selectedActionSlot) return;
    setSavingAction(true);
    setActionError("");
    setActionNotice(null);
    try {
      const response = await apiRequest<AppointmentRecord | BookingAttempt>(
        `/api/salons/${salon.id}/appointments/${selectedAppointment.id}/reschedule`,
        {
          method: "POST",
          body: JSON.stringify({
            start_time: selectedActionSlot.start_time,
            staff_id: actionForm.staffID,
            notes: actionForm.notes
          })
        }
      );
      if (isBookingAttempt(response)) {
        setActionNotice({
          tone: "warning",
          title: "Reschedule needs owner review",
          message: "Square Appointments did not reschedule this booking. The original appointment was left unchanged."
        });
      } else {
        setActionNotice({
          tone: "success",
          title: "Appointment rescheduled",
          message: "Square Appointments confirmed the new time before the dashboard updated this appointment."
        });
      }
      closeActionPanel();
      await load({ silent: true });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not reschedule appointment.");
    } finally {
      setSavingAction(false);
    }
  }

  async function submitCancel() {
    if (!salon || !selectedAppointment) return;
    setSavingAction(true);
    setActionError("");
    setActionNotice(null);
    try {
      const response = await apiRequest<AppointmentRecord | BookingAttempt>(
        `/api/salons/${salon.id}/appointments/${selectedAppointment.id}/cancel`,
        {
          method: "POST",
          body: JSON.stringify({ reason: actionForm.cancelReason })
        }
      );
      if (isBookingAttempt(response)) {
        setActionNotice({
          tone: "warning",
          title: "Cancellation needs owner review",
          message: "Square Appointments did not cancel this booking. The original appointment was left unchanged."
        });
      } else {
        setActionNotice({
          tone: "success",
          title: "Appointment cancelled",
          message: "Square Appointments confirmed the cancellation before the dashboard updated this appointment."
        });
      }
      closeActionPanel();
      await load({ silent: true });
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not cancel appointment.");
    } finally {
      setSavingAction(false);
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
        <Skeleton className="h-96" />
        <div className="grid gap-4 xl:grid-cols-[1.25fr_0.75fr]">
          <Skeleton className="h-96" />
          <Skeleton className="h-96" />
        </div>
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
          <h1 className="text-2xl font-bold text-ink">Booking Calendar</h1>
          <p className="mt-1 text-sm text-muted">
            POS-confirmed bookings, pending requests, and Square Appointments availability used by the AI receptionist.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button
            type="button"
            onClick={openCreateBooking}
            disabled={!readyForManualBooking || savingAction}
          >
            <CalendarPlus className="h-4 w-4" />
            New booking
          </Button>
          <Button type="button" variant="secondary" onClick={() => void load({ silent: true })}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Appointments unavailable" message={error} /> : null}
      {actionNotice ? <ActionNoticePanel notice={actionNotice} /> : null}

      <ReadinessPanel status={status} />
      <BookingBoundaryPanel />

      {actionMode ? (
        <BookingActionPanel
          mode={actionMode}
          form={actionForm}
          selectedAppointment={selectedAppointment}
          bookableServices={bookableServices}
          bookableStaff={bookableStaff}
          readyForManualBooking={readyForManualBooking}
          timezone={salon.timezone}
          availabilityChecked={actionAvailabilityChecked}
          availabilityLoading={checkingActionAvailability}
          availabilityResult={actionAvailabilityResult}
          availabilityError={actionAvailabilityError}
          selectedSlotKey={actionForm.selectedSlotKey}
          selectedSlot={selectedActionSlot}
          saving={savingAction}
          actionError={actionError}
          onChange={updateActionForm}
          onClose={closeActionPanel}
          onCheckAvailability={() => void checkActionAvailability()}
          onSelectSlot={(slot) => updateActionForm({ selectedSlotKey: slotKey(slot) })}
          onCreate={() => void submitCreateBooking()}
          onReschedule={() => void submitReschedule()}
          onCancelAppointment={() => void submitCancel()}
        />
      ) : null}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="POS-confirmed appointments" value={confirmedCount} />
        <Metric label="Upcoming" value={upcomingCount} />
        <Metric label="Pending requests" value={String(pendingRequests.length)} />
        <Metric label="Last Square sync" value={formatOptionalDate(status?.connection.last_sync_at)} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.25fr_0.75fr]">
        <Card>
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <CardTitle>Calendar view</CardTitle>
              <CardDescription>
                POS-confirmed bookings and pending requests for the selected day.
              </CardDescription>
            </div>
            <Badge value={dayAppointments.length + dayPendingRequests.length > 0 ? "active" : "disabled"} />
          </div>

          <DateControls
            selectedDate={selectedDate}
            onShortcut={setDateFromShortcut}
            onChange={(value) => {
              setSelectedDate(value);
              setAvailabilityResult(null);
              setAvailabilityError("");
              setAvailabilityChecked(false);
            }}
          />

          <DaySchedule
            selectedDate={selectedDate}
            appointments={dayAppointments}
            pendingRequests={dayPendingRequests}
            serviceNames={serviceNames}
            staffNames={staffNames}
            timezone={salon.timezone}
          />
        </Card>

        <Card>
          <div className="flex items-start gap-3">
            <CalendarSearch className="mt-1 h-5 w-5 text-brand" />
            <div>
              <CardTitle>Find available slots</CardTitle>
              <CardDescription>
                Check Square Appointments availability before the AI offers times to a caller.
              </CardDescription>
            </div>
          </div>

          <div className="mt-5 grid gap-4">
            {!readyForAvailability ? (
              <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
                Connect Square Appointments, select a location, and sync AI-bookable services, staff, and business hours before checking availability.
              </div>
            ) : null}

            <label className="block">
              <span className="text-sm font-medium text-ink">Service</span>
              <select
                className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
                value={availabilityServiceID}
                onChange={(event) => {
                  setAvailabilityServiceID(event.target.value);
                  setAvailabilityResult(null);
                  setAvailabilityError("");
                  setAvailabilityChecked(false);
                }}
                disabled={!readyForAvailability || checkingAvailability}
              >
                {bookableServices.length === 0 ? <option value="">No AI-bookable services</option> : null}
                {bookableServices.map((item) => (
                  <option key={item.id} value={item.id ?? ""}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>

            <label className="block">
              <span className="text-sm font-medium text-ink">Staff</span>
              <select
                className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
                value={availabilityStaffID}
                onChange={(event) => {
                  setAvailabilityStaffID(event.target.value);
                  setAvailabilityResult(null);
                  setAvailabilityError("");
                  setAvailabilityChecked(false);
                }}
                disabled={!readyForAvailability || checkingAvailability}
              >
                <option value="">Anyone available</option>
                {bookableStaff.map((item) => (
                  <option key={item.id} value={item.id ?? ""}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>

            <Button
              type="button"
              onClick={() => void checkAvailability()}
              disabled={!readyForAvailability || checkingAvailability || !availabilityServiceID || !selectedDate}
            >
              <CalendarSearch className="h-4 w-4" />
              {checkingAvailability ? "Checking..." : "Check availability"}
            </Button>

            {availabilityError ? <Alert title="Availability check failed" message={availabilityError} /> : null}

            <AvailabilitySlotsPanel
              checked={availabilityChecked}
              loading={checkingAvailability}
              result={availabilityResult}
              slots={availabilityResult?.slots ?? []}
              timezone={availabilityResult?.timezone ?? salon.timezone}
            />
          </div>
        </Card>
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>POS-confirmed appointments</CardTitle>
            <CardDescription>
              These rows are confirmed only because Square Appointments returned a booking ID.
            </CardDescription>
          </div>
          <Badge value={appointments.length > 0 ? "active" : "disabled"} />
        </div>

        {appointmentListLoading ? (
          <div className="mt-4 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
            Loading POS-confirmed appointments...
          </div>
        ) : null}

        {appointments.length === 0 ? (
          <EmptyState
            icon={<CalendarClock className="h-5 w-5 text-muted" />}
            title="No appointments yet"
            message="POS-confirmed bookings will appear here after Square Appointments returns a booking ID."
          >
            <Button
              type="button"
              className="mt-4"
              onClick={openCreateBooking}
              disabled={!readyForManualBooking || savingAction}
            >
              <CalendarPlus className="h-4 w-4" />
              New booking
            </Button>
          </EmptyState>
        ) : (
          <>
            <AppointmentPaginationControls
              className="mt-5"
              count={appointmentRows.length}
              limit={appointmentLimit}
              offset={appointmentOffset}
              hasMore={appointmentHasMore}
              busy={appointmentListLoading || savingAction}
              onPrevious={goToPreviousAppointmentPage}
              onNext={goToNextAppointmentPage}
              onLimitChange={updateAppointmentPageSize}
            />
            <div className="mt-3 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[1220px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">When</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Services</th>
                    <th className="px-4 py-3">Technician preference</th>
                    <th className="px-4 py-3">Assigned technicians</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3">POS booking</th>
                    <th className="px-4 py-3">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {appointmentRows.map((item) => (
                    <tr key={item.id}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{formatDate(item.start_time, salon.timezone)}</div>
                        <div className="mt-1 text-xs text-muted">{formatTimeRange(item.start_time, item.end_time, salon.timezone)}</div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{item.customer_name}</div>
                        <div className="mt-1 text-xs text-muted">{item.customer_phone}</div>
                      </td>
                      <td className="px-4 py-3 text-muted">{serviceNamesLabel(item, serviceNames)}</td>
                      <td className="px-4 py-3">
                        <Badge value={technicianPreferenceValue(item)} />
                        <div className="mt-1 text-xs text-muted">{technicianPreferenceLabel(item)}</div>
                      </td>
                      <td className="px-4 py-3 text-muted">{assignedTechniciansLabel(item, staffNames)}</td>
                      <td className="px-4 py-3">
                        <Badge value={item.status} />
                      </td>
                      <td className="px-4 py-3 text-muted">{item.pos_appointment_id || "Not returned"}</td>
                      <td className="px-4 py-3">
                        <AppointmentActions
                          appointment={item}
                          disabled={savingAction || !canChangeAppointment(item)}
                          onReschedule={openReschedule}
                          onCancel={openCancel}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {appointmentRows.map((item) => (
                <AppointmentCard
                  key={item.id}
                  item={item}
                  serviceName={serviceNamesLabel(item, serviceNames)}
                  staffName={assignedTechniciansLabel(item, staffNames)}
                  technicianPreference={technicianPreferenceLabel(item)}
                  timezone={salon.timezone}
                  disabled={savingAction || !canChangeAppointment(item)}
                  onReschedule={openReschedule}
                  onCancel={openCancel}
                />
              ))}
            </div>
            <AppointmentPaginationControls
              className="mt-4"
              count={appointmentRows.length}
              limit={appointmentLimit}
              offset={appointmentOffset}
              hasMore={appointmentHasMore}
              busy={appointmentListLoading || savingAction}
              onPrevious={goToPreviousAppointmentPage}
              onNext={goToNextAppointmentPage}
              onLimitChange={updateAppointmentPageSize}
            />
          </>
        )}
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Fallback requests</CardTitle>
            <CardDescription>
              These requests are not POS-confirmed appointments. Review them when the POS path fails.
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
              <table className="w-full min-w-[1080px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Requested time</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Services</th>
                    <th className="px-4 py-3">Technician preference</th>
                    <th className="px-4 py-3">Assigned technicians</th>
                    <th className="px-4 py-3">Failure reason</th>
                    <th className="px-4 py-3">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {pendingRequests.map((item) => (
                    <tr key={item.id}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{formatDate(item.requested_start_time, salon.timezone)}</div>
                        <div className="mt-1 text-xs text-muted">
                          {formatTimeRange(item.requested_start_time, item.requested_end_time, salon.timezone)}
                        </div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{item.customer_name}</div>
                        <div className="mt-1 text-xs text-muted">{item.customer_phone}</div>
                      </td>
                      <td className="px-4 py-3 text-muted">{serviceNamesLabel(item, serviceNames)}</td>
                      <td className="px-4 py-3">
                        <Badge value={technicianPreferenceValue(item)} />
                        <div className="mt-1 text-xs text-muted">{technicianPreferenceLabel(item)}</div>
                      </td>
                      <td className="px-4 py-3 text-muted">{assignedTechniciansLabel(item, staffNames)}</td>
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
                  serviceName={serviceNamesLabel(item, serviceNames)}
                  staffName={assignedTechniciansLabel(item, staffNames)}
                  technicianPreference={technicianPreferenceLabel(item)}
                  timezone={salon.timezone}
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
  const bookingWriteBlocked = Boolean(readiness?.booking_write_blocked);
  const appointmentChangesBlocked = Boolean(readiness?.appointment_change_write_blocked);

  if (readyForBookings && bookingWriteBlocked) {
    return (
      <Card className="border-red-200 bg-red-50 shadow-none">
        <div className="flex gap-3">
          <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-accent" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>Square booking writes are blocked</CardTitle>
              <Badge value="fallback_pending" />
            </div>
            <CardDescription className="text-red-900">
              The AI can check Square availability, but new bookings cannot be POS-confirmed until Square accepts booking writes.
            </CardDescription>
            <div className="mt-3 rounded-md border border-red-200 bg-white p-3 text-sm leading-6 text-red-900">
              <div className="font-semibold">{readiness?.booking_write_blocked_code || "Square booking write blocked"}</div>
              <div className="mt-1">{readiness?.booking_write_blocked_reason || "Square Appointments rejected booking writes."}</div>
              <div className="mt-1 text-xs text-muted">
                Last seen: {formatOptionalDate(readiness?.booking_write_blocked_at)}
              </div>
            </div>
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

  if (readyForBookings && appointmentChangesBlocked) {
    return (
      <Card className="border-amber-200 bg-amber-50 shadow-none">
        <div className="flex gap-3">
          <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>Square appointment changes need owner review</CardTitle>
              <Badge value="fallback_pending" />
            </div>
            <CardDescription className="text-amber-900">
              New bookings can still be POS-confirmed, but Square rejected automatic reschedule or cancellation for this seller account.
            </CardDescription>
            <div className="mt-3 rounded-md border border-amber-200 bg-white p-3 text-sm leading-6 text-amber-900">
              <div className="font-semibold">{readiness?.appointment_change_write_blocked_code || "Square write blocked"}</div>
              <div className="mt-1">{readiness?.appointment_change_write_blocked_reason || "Square Appointments rejected appointment-change writes."}</div>
              <div className="mt-1 text-xs text-muted">
                Last seen: {formatOptionalDate(readiness?.appointment_change_write_blocked_at)}
              </div>
            </div>
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

  if (readyForBookings) {
    return (
      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <CardTitle>Square booking path is configured</CardTitle>
            <CardDescription className="text-emerald-800">
              POS-confirmed appointments require a successful Square Appointments booking ID.
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
            Connect Square Appointments, select a location, and sync services, staff, and business hours before AI booking can operate.
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

function BookingBoundaryPanel() {
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Booking boundary</CardTitle>
          <CardDescription>
            The dashboard separates POS-confirmed appointments from requests that still need owner review.
          </CardDescription>
        </div>
        <Badge value="pos_pending" />
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <BoundaryItem
          label="POS-confirmed appointment"
          value="The active POS provider returned a booking ID before the appointment was recorded as confirmed."
        />
        <BoundaryItem
          label="Pending request"
          value="The POS path did not confirm. Owner review is required and no appointment is marked confirmed."
        />
        <BoundaryItem
          label="Not bookable"
          value="Local-only, unmapped, archived, or sync-failed services and staff stay out of availability and booking."
        />
      </div>
    </Card>
  );
}

function BoundaryItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-line p-3">
      <div className="text-sm font-semibold text-ink">{label}</div>
      <div className="mt-1 text-xs leading-5 text-muted">{value}</div>
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

function ActionNoticePanel({ notice }: { notice: ActionNotice }) {
  if (notice.tone === "success") {
    return <Alert type="success" title={notice.title} message={notice.message} />;
  }
  return (
    <div className="flex gap-3 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
      <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
      <div>
        <div className="font-semibold">{notice.title}</div>
        <div className="mt-1 leading-6">{notice.message}</div>
      </div>
    </div>
  );
}

function BookingActionPanel({
  mode,
  form,
  selectedAppointment,
  bookableServices,
  bookableStaff,
  readyForManualBooking,
  timezone,
  availabilityChecked,
  availabilityLoading,
  availabilityResult,
  availabilityError,
  selectedSlotKey,
  selectedSlot,
  saving,
  actionError,
  onChange,
  onClose,
  onCheckAvailability,
  onSelectSlot,
  onCreate,
  onReschedule,
  onCancelAppointment
}: {
  mode: AppointmentActionMode;
  form: AppointmentActionForm;
  selectedAppointment: AppointmentRecord | null;
  bookableServices: POSService[];
  bookableStaff: POSStaffMember[];
  readyForManualBooking: boolean;
  timezone?: string;
  availabilityChecked: boolean;
  availabilityLoading: boolean;
  availabilityResult: AvailabilityResult | null;
  availabilityError: string;
  selectedSlotKey: string;
  selectedSlot: AvailabilitySlot | null;
  saving: boolean;
  actionError: string;
  onChange: (patch: Partial<AppointmentActionForm>) => void;
  onClose: () => void;
  onCheckAvailability: () => void;
  onSelectSlot: (slot: AvailabilitySlot) => void;
  onCreate: () => void;
  onReschedule: () => void;
  onCancelAppointment: () => void;
}) {
  const title =
    mode === "create" ? "New booking" : mode === "reschedule" ? "Reschedule appointment" : "Cancel appointment";
  const canCheckAvailability =
    readyForManualBooking &&
    !availabilityLoading &&
    !saving &&
    Boolean(form.preferredDate) &&
    (mode === "create" ? Boolean(form.serviceID) : Boolean(selectedAppointment));
  const canSubmitCreate =
    mode === "create" &&
    readyForManualBooking &&
    Boolean(selectedSlot) &&
    Boolean(form.customerName.trim()) &&
    Boolean(form.customerPhone.trim()) &&
    !saving;
  const canSubmitReschedule =
    mode === "reschedule" && readyForManualBooking && Boolean(selectedAppointment) && Boolean(selectedSlot) && !saving;

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>{title}</CardTitle>
          <CardDescription>
            {mode === "cancel"
              ? "Cancellation is applied only after Square Appointments confirms it."
              : "Check Square Appointments availability, choose a returned slot, then submit the POS action."}
          </CardDescription>
        </div>
        <Button type="button" variant="ghost" onClick={onClose} disabled={saving} aria-label="Close booking action">
          <X className="h-4 w-4" />
          Close
        </Button>
      </div>

      {!readyForManualBooking && mode !== "cancel" ? (
        <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
          Connect Square Appointments, select a location, and keep at least one AI-bookable service and staff member before creating or rescheduling bookings.
        </div>
      ) : null}

      {actionError ? (
        <div className="mt-5">
          <Alert title={`${title} failed`} message={actionError} />
        </div>
      ) : null}

      {mode === "cancel" ? (
        <CancelAppointmentForm
          appointment={selectedAppointment}
          reason={form.cancelReason}
          saving={saving}
          onReasonChange={(value) => onChange({ cancelReason: value })}
          onSubmit={onCancelAppointment}
        />
      ) : (
        <div className="mt-5 grid gap-5">
          {mode === "create" ? (
            <CustomerFields form={form} disabled={saving} onChange={onChange} />
          ) : (
            <AppointmentActionSummary appointment={selectedAppointment} timezone={timezone} />
          )}

          <div className="grid gap-4 lg:grid-cols-3">
            {mode === "create" ? (
              <label className="block">
                <span className="text-sm font-medium text-ink">Service</span>
                <select
                  className={selectClassName}
                  value={form.serviceID}
                  onChange={(event) =>
                    onChange({ serviceID: event.target.value, selectedSlotKey: "" })
                  }
                  disabled={!readyForManualBooking || saving || availabilityLoading}
                >
                  {bookableServices.length === 0 ? <option value="">No AI-bookable services</option> : null}
                  {bookableServices.map((item) => (
                    <option key={item.id} value={item.id ?? ""}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <ReadOnlyField label="Services" value={selectedAppointment ? serviceNamesLabel(selectedAppointment) : "-"} />
            )}

            <label className="block">
              <span className="text-sm font-medium text-ink">Staff</span>
              <select
                className={selectClassName}
                value={form.staffID}
                onChange={(event) => onChange({ staffID: event.target.value, selectedSlotKey: "" })}
                disabled={!readyForManualBooking || saving || availabilityLoading}
              >
                <option value="">{mode === "create" ? "Anyone available" : "Keep assigned technicians"}</option>
                {bookableStaff.map((item) => (
                  <option key={item.id} value={item.id ?? ""}>
                    {item.name}
                  </option>
                ))}
              </select>
            </label>

            <label className="block">
              <span className="text-sm font-medium text-ink">Date</span>
              <input
                className={inputClassName}
                type="date"
                value={form.preferredDate}
                onChange={(event) => onChange({ preferredDate: event.target.value, selectedSlotKey: "" })}
                disabled={!readyForManualBooking || saving || availabilityLoading}
              />
            </label>
          </div>

          <label className="block">
            <span className="text-sm font-medium text-ink">Notes</span>
            <textarea
              className={textareaClassName}
              value={form.notes}
              onChange={(event) => onChange({ notes: event.target.value })}
              placeholder={mode === "create" ? "First visit, preferred color, or owner notes" : "Reason for the new time"}
              disabled={saving}
            />
          </label>

          <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
            <Button type="button" variant="secondary" onClick={onCheckAvailability} disabled={!canCheckAvailability}>
              <CalendarSearch className="h-4 w-4" />
              {availabilityLoading ? "Checking..." : "Check availability"}
            </Button>
            <div className="text-sm leading-6 text-muted">
              {selectedSlot
                ? `Selected ${formatTimeRange(selectedSlot.start_time, selectedSlot.end_time, timezone)}`
                : "Select a returned Square slot before submitting."}
            </div>
          </div>

          {availabilityError ? <Alert title="Availability check failed" message={availabilityError} /> : null}

          <ActionAvailabilitySlots
            checked={availabilityChecked}
            loading={availabilityLoading}
            result={availabilityResult}
            selectedSlotKey={selectedSlotKey}
            timezone={timezone}
            onSelect={onSelectSlot}
          />

          <div className="flex flex-col gap-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="secondary" onClick={onClose} disabled={saving}>
              Keep current view
            </Button>
            {mode === "create" ? (
              <Button type="button" onClick={onCreate} disabled={!canSubmitCreate}>
                <CalendarPlus className="h-4 w-4" />
                {saving ? "Creating..." : "Create booking"}
              </Button>
            ) : (
              <Button type="button" onClick={onReschedule} disabled={!canSubmitReschedule}>
                {saving ? "Rescheduling..." : "Reschedule appointment"}
              </Button>
            )}
          </div>
        </div>
      )}
    </Card>
  );
}

function CustomerFields({
  form,
  disabled,
  onChange
}: {
  form: AppointmentActionForm;
  disabled: boolean;
  onChange: (patch: Partial<AppointmentActionForm>) => void;
}) {
  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <label className="block">
        <span className="text-sm font-medium text-ink">Customer name</span>
        <input
          className={inputClassName}
          value={form.customerName}
          onChange={(event) => onChange({ customerName: event.target.value })}
          placeholder="Customer name"
          disabled={disabled}
        />
      </label>
      <label className="block">
        <span className="text-sm font-medium text-ink">Customer phone</span>
        <input
          className={inputClassName}
          value={form.customerPhone}
          onChange={(event) => onChange({ customerPhone: event.target.value })}
          placeholder="+13125550101"
          disabled={disabled}
        />
      </label>
      <label className="block">
        <span className="text-sm font-medium text-ink">Customer email</span>
        <input
          className={inputClassName}
          value={form.customerEmail}
          onChange={(event) => onChange({ customerEmail: event.target.value })}
          placeholder="Optional"
          disabled={disabled}
        />
      </label>
    </div>
  );
}

function AppointmentActionSummary({
  appointment,
  timezone
}: {
  appointment: AppointmentRecord | null;
  timezone?: string;
}) {
  if (!appointment) return null;
  return (
    <div className="rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">{appointment.customer_name}</div>
          <div className="mt-1 text-sm leading-6 text-muted">
            {formatDate(appointment.start_time, timezone)} {formatTimeRange(appointment.start_time, appointment.end_time, timezone)}
          </div>
        </div>
        <Badge value={appointment.status} />
      </div>
      <InfoGrid
        items={[
          ["Services", serviceNamesLabel(appointment)],
          ["Assigned technicians", assignedTechniciansLabel(appointment)],
          ["POS booking", appointment.pos_appointment_id || "Not returned"],
          ["Technician preference", technicianPreferenceLabel(appointment)]
        ]}
      />
    </div>
  );
}

function CancelAppointmentForm({
  appointment,
  reason,
  saving,
  onReasonChange,
  onSubmit
}: {
  appointment: AppointmentRecord | null;
  reason: string;
  saving: boolean;
  onReasonChange: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <div className="mt-5 grid gap-5">
      <AppointmentActionSummary appointment={appointment} />
      <label className="block">
        <span className="text-sm font-medium text-ink">Cancellation reason</span>
        <textarea
          className={textareaClassName}
          value={reason}
          onChange={(event) => onReasonChange(event.target.value)}
          placeholder="Customer requested cancellation"
          disabled={saving}
        />
      </label>
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
        This does not delete appointment history. The dashboard marks it cancelled only after Square Appointments confirms the cancellation.
      </div>
      <div className="flex flex-col gap-3 sm:flex-row sm:justify-end">
        <Button type="button" variant="danger" onClick={onSubmit} disabled={!appointment || saving}>
          {saving ? "Cancelling..." : "Cancel appointment"}
        </Button>
      </div>
    </div>
  );
}

function ReadOnlyField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-sm font-medium text-ink">{label}</div>
      <div className="mt-2 min-h-10 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm leading-6 text-muted">
        {value}
      </div>
    </div>
  );
}

function ActionAvailabilitySlots({
  checked,
  loading,
  result,
  selectedSlotKey,
  timezone,
  onSelect
}: {
  checked: boolean;
  loading: boolean;
  result: AvailabilityResult | null;
  selectedSlotKey: string;
  timezone?: string;
  onSelect: (slot: AvailabilitySlot) => void;
}) {
  if (loading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-16" />
        <Skeleton className="h-16" />
      </div>
    );
  }
  if (!checked) {
    return (
      <div className="rounded-md border border-line bg-slate-50 p-4 text-sm leading-6 text-muted">
        Check availability to get Square Appointments slots for this action.
      </div>
    );
  }
  if (!result || result.slots.length === 0) {
    return (
      <EmptyState
        icon={<CalendarSearch className="h-5 w-5 text-muted" />}
        title="No available slots returned"
        message="Try another day, service, or technician before submitting this POS action."
      />
    );
  }
  return (
    <div className="space-y-3">
      {result.slots.map((slot) => {
        const key = slotKey(slot);
        const selected = key === selectedSlotKey;
        return (
          <div key={key} className="rounded-md border border-line p-4">
            <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
              <div>
                <div className="text-sm font-semibold text-ink">
                  {formatTimeRange(slot.start_time, slot.end_time, timezone)}
                </div>
                <div className="mt-1 text-sm leading-6 text-muted">
                  Assigned: {assignedTechniciansLabel(slot)}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Badge value={selected ? "selected" : "available"} />
                <Button type="button" variant={selected ? "primary" : "secondary"} onClick={() => onSelect(slot)}>
                  {selected ? "Selected" : "Use this slot"}
                </Button>
              </div>
            </div>
            <SegmentAssignmentList record={slot} />
          </div>
        );
      })}
    </div>
  );
}

function AppointmentActions({
  appointment,
  disabled,
  onReschedule,
  onCancel
}: {
  appointment: AppointmentRecord;
  disabled: boolean;
  onReschedule: (appointment: AppointmentRecord) => void;
  onCancel: (appointment: AppointmentRecord) => void;
}) {
  return (
    <div className="flex flex-col gap-2 sm:flex-row">
      <Button type="button" variant="secondary" onClick={() => onReschedule(appointment)} disabled={disabled}>
        Reschedule
      </Button>
      <Button type="button" variant="danger" onClick={() => onCancel(appointment)} disabled={disabled}>
        Cancel
      </Button>
    </div>
  );
}

function AppointmentPaginationControls({
  className = "",
  count,
  limit,
  offset,
  hasMore,
  busy,
  onPrevious,
  onNext,
  onLimitChange
}: {
  className?: string;
  count: number;
  limit: number;
  offset: number;
  hasMore: boolean;
  busy: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onLimitChange: (limit: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;

  return (
    <div
      className={`flex flex-col gap-3 rounded-md border border-line bg-slate-50 px-3 py-3 sm:flex-row sm:items-center sm:justify-between ${className}`}
    >
      <div className="text-sm leading-6 text-muted">{appointmentRangeLabel(count, offset, hasMore)}</div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <label className="flex items-center gap-2 text-sm text-muted">
          Rows per page
          <select
            className="h-9 rounded-md border border-line bg-white px-2 text-sm font-medium text-ink outline-none focus:border-brand disabled:text-slate-400"
            value={limit}
            onChange={(event) => onLimitChange(Number(event.target.value))}
            disabled={busy}
          >
            {appointmentPageSizeOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <div className="flex items-center gap-2">
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onPrevious} disabled={busy || offset === 0}>
            <ChevronLeft className="h-4 w-4" />
            Previous
          </Button>
          <span className="min-w-16 text-center text-sm font-semibold text-ink">Page {page}</span>
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onNext} disabled={busy || !hasMore}>
            Next
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function appointmentRangeLabel(count: number, offset: number, hasMore: boolean) {
  if (count === 0) {
    return "No POS-confirmed appointments";
  }
  const start = offset + 1;
  const end = offset + count;
  const total = hasMore ? `at least ${end + 1}` : String(end);
  return `Showing ${start}-${end} of ${total} POS-confirmed appointments`;
}

function appointmentCountLabel(count: number, hasMore: boolean) {
  return hasMore ? `${count}+` : String(count);
}

function DateControls({
  selectedDate,
  onShortcut,
  onChange
}: {
  selectedDate: string;
  onShortcut: (offsetDays: number) => void;
  onChange: (value: string) => void;
}) {
  return (
    <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-end">
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="secondary" onClick={() => onShortcut(0)}>
          Today
        </Button>
        <Button type="button" variant="secondary" onClick={() => onShortcut(1)}>
          Tomorrow
        </Button>
      </div>
      <label className="block sm:ml-auto">
        <span className="text-sm font-medium text-ink">Date</span>
        <input
          className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand sm:w-44"
          type="date"
          value={selectedDate}
          onChange={(event) => onChange(event.target.value)}
        />
      </label>
    </div>
  );
}

function DaySchedule({
  selectedDate,
  appointments,
  pendingRequests,
  serviceNames,
  staffNames,
  timezone
}: {
  selectedDate: string;
  appointments: AppointmentRecord[];
  pendingRequests: BookingAttempt[];
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
  timezone?: string;
}) {
  const items = [
    ...appointments.map((item) => ({
      id: `appointment-${item.id}`,
      start: item.start_time,
      end: item.end_time,
      title: item.customer_name,
      subtitle: bookingSummaryLabel(item, serviceNames, staffNames),
      status: item.status,
      detail: item.pos_appointment_id ? "POS-confirmed: Square booking ID returned" : "POS booking ID missing"
    })),
    ...pendingRequests.map((item) => ({
      id: `pending-${item.id}`,
      start: item.requested_start_time,
      end: item.requested_end_time,
      title: item.customer_name,
      subtitle: bookingSummaryLabel(item, serviceNames, staffNames),
      status: item.status,
      detail: item.error_code || "Pending owner review"
    }))
  ].sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());

  if (items.length === 0) {
    return (
      <EmptyState
        icon={<CalendarClock className="h-5 w-5 text-muted" />}
        title="No calendar items for this day"
        message="POS-confirmed bookings and pending requests for the selected date will appear here."
      />
    );
  }

  return (
    <div className="mt-5 space-y-3">
      <div className="text-sm font-semibold text-ink">{formatInputDateLabel(selectedDate)}</div>
      {items.map((item) => (
        <div
          key={item.id}
          className="grid gap-3 rounded-md border border-line p-4 sm:grid-cols-[8.5rem_1fr_auto] sm:items-start"
        >
          <div>
            <div className="text-sm font-semibold text-ink">{formatTimeRange(item.start, item.end, timezone)}</div>
            <div className="mt-1 text-xs text-muted">{formatDate(item.start, timezone)}</div>
          </div>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-ink">{item.title || "Unknown customer"}</div>
            <div className="mt-1 text-sm leading-6 text-muted">{item.subtitle}</div>
            <div className="mt-1 text-xs leading-5 text-muted">{item.detail}</div>
          </div>
          <Badge value={item.status} />
        </div>
      ))}
    </div>
  );
}

function AvailabilitySlotsPanel({
  checked,
  loading,
  result,
  slots,
  timezone
}: {
  checked: boolean;
  loading: boolean;
  result: AvailabilityResult | null;
  slots: AvailabilitySlot[];
  timezone?: string;
}) {
  if (loading) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-16" />
        <Skeleton className="h-16" />
        <Skeleton className="h-16" />
      </div>
    );
  }

  if (!checked) {
    return (
      <div className="rounded-md border border-line bg-slate-50 p-4 text-sm leading-6 text-muted">
        Select a service and date, then check Square Appointments availability.
      </div>
    );
  }

  if (!result || slots.length === 0) {
    return (
      <EmptyState
        icon={<CalendarSearch className="h-5 w-5 text-muted" />}
        title="No available slots returned"
        message="Try another day, service, or technician before the AI offers times to a caller."
      />
    );
  }

  return (
    <div className="space-y-3">
      <div>
        <div className="text-sm font-semibold text-ink">Available Square slots</div>
        <div className="mt-1 text-xs leading-5 text-muted">
          AI can offer these slots, but booking still requires Square Appointments confirmation
          {result.timezone ? ` (${result.timezone})` : ""}.
        </div>
      </div>
      {slots.map((slot) => (
        <div key={`${slot.start_time}-${slot.staff_id}`} className="rounded-md border border-line p-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="text-sm font-semibold text-ink">
                {formatTimeRange(slot.start_time, slot.end_time, timezone)}
              </div>
              <div className="mt-1 text-sm leading-6 text-muted">
                Customer-facing: {technicianPreferenceValue(slot) === "anyone" ? "Anyone available" : assignedTechniciansLabel(slot)}
              </div>
              <div className="mt-1 text-sm leading-6 text-muted">
                Assigned: {assignedTechniciansLabel(slot)}
              </div>
            </div>
            <Badge value="available" />
          </div>
          <SegmentAssignmentList record={slot} />
        </div>
      ))}
    </div>
  );
}

function SegmentAssignmentList({ record }: { record: AvailabilitySlot | AppointmentRecord | BookingAttempt }) {
  const segments = orderedSegments(record);
  if (segments.length === 0) {
    return null;
  }
  return (
    <div className="mt-3 space-y-2 border-t border-line pt-3">
      {segments.map((segment, index) => (
        <div key={`${segment.service_id ?? "service"}-${segment.staff_id ?? "staff"}-${index}`} className="text-xs leading-5 text-muted">
          <span className="font-semibold text-ink">{index + 1}. {segment.service_name || "Unknown service"}</span>
          {" -> "}
          <span>{segment.staff_name || "Unassigned technician"}</span>
        </div>
      ))}
    </div>
  );
}

function EmptyState({
  icon,
  title,
  message,
  children
}: {
  icon: ReactNode;
  title: string;
  message: string;
  children?: ReactNode;
}) {
  return (
    <div className="mt-5 rounded-md border border-line p-6 text-center">
      <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-md bg-slate-100">{icon}</div>
      <div className="mt-3 text-sm font-semibold text-ink">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted">{message}</div>
      {children}
    </div>
  );
}

function AppointmentCard({
  item,
  serviceName,
  staffName,
  technicianPreference,
  timezone,
  disabled,
  onReschedule,
  onCancel
}: {
  item: AppointmentRecord;
  serviceName: string;
  staffName: string;
  technicianPreference: string;
  timezone?: string;
  disabled: boolean;
  onReschedule: (appointment: AppointmentRecord) => void;
  onCancel: (appointment: AppointmentRecord) => void;
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
          ["When", `${formatDate(item.start_time, timezone)} ${formatTimeRange(item.start_time, item.end_time, timezone)}`],
          ["Services", serviceName],
          ["Technician preference", technicianPreference],
          ["Assigned technicians", staffName],
          ["POS booking", item.pos_appointment_id || "Not returned"]
        ]}
      />
      <SegmentAssignmentList record={item} />
      <div className="mt-4">
        <AppointmentActions
          appointment={item}
          disabled={disabled}
          onReschedule={onReschedule}
          onCancel={onCancel}
        />
      </div>
    </div>
  );
}

function FallbackCard({
  item,
  serviceName,
  staffName,
  technicianPreference,
  timezone
}: {
  item: BookingAttempt;
  serviceName: string;
  staffName: string;
  technicianPreference: string;
  timezone?: string;
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
          ["Requested", `${formatDate(item.requested_start_time, timezone)} ${formatTimeRange(item.requested_start_time, item.requested_end_time, timezone)}`],
          ["Services", serviceName],
          ["Technician preference", technicianPreference],
          ["Assigned technicians", staffName],
          ["Failure", item.error_code || "POS error"]
        ]}
      />
      <SegmentAssignmentList record={item} />
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

function formatOptionalDate(value?: string) {
  if (!value) return "Not synced";
  return formatDate(value);
}

function formatDate(value: string, timezone?: string) {
  return new Date(value).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: timezone
  });
}

function formatTimeRange(start: string, end: string, timezone?: string) {
  const options = { hour: "numeric", minute: "2-digit", timeZone: timezone } as const;
  const startLabel = new Date(start).toLocaleTimeString(undefined, options);
  const endLabel = new Date(end).toLocaleTimeString(undefined, options);
  return `${startLabel} - ${endLabel}`;
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

function sameDateInput(value: string, selectedDate: string, timezone?: string) {
  return formatDateInput(new Date(value), timezone) === selectedDate;
}

function formatInputDateLabel(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) return "Selected date";
  return new Date(year, month - 1, day).toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric"
  });
}

function firstBookableServiceID(services: POSService[]) {
  return services.find(serviceIsBookable)?.id ?? "";
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

function bookingPathReady(status: StatusResponse | null) {
  const connection = status?.connection;
  const readiness = status?.readiness;
  const connected = Boolean(connection?.id) && connection?.status !== "not_connected";
  const locationSelected = Boolean(connection?.location_id);
  return connected && locationSelected && (readiness?.service_count ?? 0) > 0 && (readiness?.staff_count ?? 0) > 0;
}

function emptyActionForm(preferredDate: string): AppointmentActionForm {
  return {
    customerName: "",
    customerPhone: "",
    customerEmail: "",
    serviceID: "",
    staffID: "",
    preferredDate,
    selectedSlotKey: "",
    notes: "",
    cancelReason: ""
  };
}

function actionAvailabilitySegments(
  mode: AppointmentActionMode,
  form: AppointmentActionForm,
  appointment: AppointmentRecord | null
): BookingSegmentRequest[] {
  if (mode === "create") {
    return [
      {
        service_id: form.serviceID,
        staff_id: form.staffID,
        staff_selection_mode: form.staffID ? "specific" : "anyone"
      }
    ];
  }
  if (mode !== "reschedule" || !appointment) {
    return [];
  }
  return appointmentRequestSegments(appointment, form.staffID);
}

function appointmentRequestSegments(appointment: AppointmentRecord, staffID: string): BookingSegmentRequest[] {
  const segments = orderedSegments(appointment);
  if (segments.length > 0) {
    return segments.map((segment) => {
      const nextStaffID = staffID || segment.staff_id || appointment.staff_id || "";
      return {
        service_id: segment.service_id ?? appointment.service_id ?? "",
        staff_id: nextStaffID,
        staff_selection_mode: staffID ? "specific" : staffMode(segment.staff_selection_mode, nextStaffID)
      };
    });
  }
  const nextStaffID = staffID || appointment.staff_id || "";
  return [
    {
      service_id: appointment.service_id ?? "",
      staff_id: nextStaffID,
      staff_selection_mode: staffID ? "specific" : staffMode(appointment.staff_selection_mode, nextStaffID)
    }
  ];
}

function slotBookingSegments(
  slot: AvailabilitySlot,
  fallbackServiceID: string,
  staffSelectionMode: StaffSelectionMode
): BookingSegmentRequest[] {
  const segments = slot.segments ?? [];
  if (segments.length > 0) {
    return segments.map((segment) => ({
      service_id: segment.service_id,
      staff_id: segment.staff_id ?? "",
      staff_selection_mode: staffSelectionMode
    }));
  }
  return [
    {
      service_id: fallbackServiceID,
      staff_id: slot.staff_id ?? "",
      staff_selection_mode: staffSelectionMode
    }
  ];
}

function staffMode(value: StaffSelectionMode | undefined, staffID: string): StaffSelectionMode {
  if (value === "anyone" || value === "specific") {
    return value;
  }
  return staffID ? "specific" : "anyone";
}

function appointmentPrimaryServiceID(appointment: AppointmentRecord) {
  return orderedSegments(appointment).find((segment) => segment.service_id)?.service_id ?? appointment.service_id ?? "";
}

function canChangeAppointment(appointment: AppointmentRecord) {
  return appointment.status !== "cancelled" && Boolean(appointment.pos_appointment_id);
}

function isBookingAttempt(response: AppointmentRecord | BookingAttempt): response is BookingAttempt {
  return "requested_start_time" in response;
}

function slotKey(slot: AvailabilitySlot) {
  return `${slot.start_time}-${slot.end_time}-${slot.staff_id || assignedTechniciansLabel(slot)}`;
}

const inputClassName =
  "mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";

const selectClassName =
  "mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";

const textareaClassName =
  "mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";
