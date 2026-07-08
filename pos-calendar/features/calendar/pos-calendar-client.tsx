"use client";

import { useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import {
  AlertTriangle,
  CalendarClock,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Clock,
  LogOut,
  Pencil,
  Plus,
  RefreshCcw,
  Trash2
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest, getAccessToken, logoutSession } from "@/lib/api/client";
import { cn } from "@/lib/utils/cn";
import type {
  AvailabilityResult,
  AvailabilitySlot,
  AppointmentRecord,
  BookingAttempt,
  BookingSegmentRequest,
  CalendarRangeResponse,
  CalendarSyncResponse,
  CalendarView,
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

type ServicesResponse = {
  services: POSService[];
};

type StaffResponse = {
  staff: POSStaffMember[];
};

type ActionMode = "create" | "edit" | "delete";

type ActionForm = {
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

type Notice = {
  type: "success" | "warning" | "error";
  title: string;
  message: string;
};

type CalendarItem = {
  id: string;
  kind: "appointment" | "pending";
  start: string;
  end: string;
  status: string;
  customerName: string;
  subtitle: string;
  detail: string;
  warning: string;
  appointment?: AppointmentRecord;
  request?: BookingAttempt;
};

type PositionedCalendarItem = {
  item: CalendarItem;
  top: number;
  height: number;
  lane: number;
  laneCount: number;
};

type DisplaySegment = {
  service_id?: string;
  service_name?: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode?: StaffSelectionMode;
  duration_minutes?: number;
  sort_order?: number;
};

const views: CalendarView[] = ["day", "week", "month", "agenda"];
const weekdayLabels = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const schedulerRowHeight = 62;
const defaultSchedulerStartHour = 9;
const defaultSchedulerEndHour = 18;
const inputClassName =
  "mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400";
const textareaClassName =
  "mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400";

export function POSCalendarClient() {
  const router = useRouter();
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [calendar, setCalendar] = useState<CalendarRangeResponse | null>(null);
  const [view, setView] = useState<CalendarView>("week");
  const [anchorDate, setAnchorDate] = useState(() => formatDateInput(new Date()));
  const [loadingShell, setLoadingShell] = useState(true);
  const [loadingCalendar, setLoadingCalendar] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState<Notice | null>(null);
  const [actionMode, setActionMode] = useState<ActionMode | null>(null);
  const [selectedAppointment, setSelectedAppointment] = useState<AppointmentRecord | null>(null);
  const [actionForm, setActionForm] = useState<ActionForm>(() => emptyActionForm(formatDateInput(new Date())));
  const [availabilityResult, setAvailabilityResult] = useState<AvailabilityResult | null>(null);
  const [availabilityChecked, setAvailabilityChecked] = useState(false);
  const [availabilityError, setAvailabilityError] = useState("");
  const [checkingAvailability, setCheckingAvailability] = useState(false);
  const [savingAction, setSavingAction] = useState(false);
  const [actionError, setActionError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.replace("/login");
      return;
    }
    void loadShell();
  }, [router]);

  useEffect(() => {
    if (!salon) return;
    void loadCalendarRange(salon.id, view, anchorDate);
  }, [anchorDate, salon, view]);

  const serviceNames = useMemo(
    () => new Map(services.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [services]
  );
  const staffNames = useMemo(
    () => new Map(staff.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [staff]
  );
  const bookableServices = useMemo(() => services.filter(serviceIsBookable), [services]);
  const bookableStaff = useMemo(() => staff.filter(staffIsBookable), [staff]);
  const readyForBooking = bookingPathReady(status) && bookableServices.length > 0 && bookableStaff.length > 0;
  const range = useMemo(() => rangeForView(view, anchorDate), [anchorDate, view]);
  const items = useMemo(
    () => buildCalendarItems(calendar, serviceNames, staffNames, salon?.timezone),
    [calendar, salon?.timezone, serviceNames, staffNames]
  );
  const selectedActionSlot = useMemo(
    () => (availabilityResult?.slots ?? []).find((slot) => slotKey(slot) === actionForm.selectedSlotKey) ?? null,
    [actionForm.selectedSlotKey, availabilityResult]
  );
  const lastSyncAt = status?.connection?.last_sync_at || calendarLatestSync(calendar);
  const activeProvider = status?.connection?.provider || salon?.active_pos_provider || "square";
  const warningTotal = calendar
    ? calendar.warnings.total_warnings ??
      calendar.warnings.not_synced + calendar.warnings.sync_failed + calendar.warnings.pending_pos_sync + calendar.warnings.fallback_pending
    : 0;

  async function loadShell() {
    setLoadingShell(true);
    setError("");
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

      const [statusResponse, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(statusResponse);
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load calendar workspace.");
    } finally {
      setLoadingShell(false);
    }
  }

  async function loadStatus(salonID: string) {
    const statusResponse = await apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${salonID}`);
    setStatus(statusResponse);
  }

  async function loadCalendarRange(salonID: string, nextView: CalendarView, date: string) {
    setLoadingCalendar(true);
    setError("");
    try {
      const nextRange = rangeForView(nextView, date);
      const params = new URLSearchParams({
        start: nextRange.start,
        end: nextRange.end,
        view: nextView
      });
      const response = await apiRequest<CalendarRangeResponse>(`/api/salons/${salonID}/calendar?${params.toString()}`);
      setCalendar(response);
    } catch (err) {
      setCalendar(null);
      setError(err instanceof Error ? err.message : "Could not load calendar range.");
    } finally {
      setLoadingCalendar(false);
    }
  }

  async function syncCalendar() {
    if (!salon) return;
    setBusy("sync");
    setNotice(null);
    setError("");
    try {
      const response = await apiRequest<CalendarSyncResponse>(`/api/salons/${salon.id}/calendar/sync`, {
        method: "POST",
        body: JSON.stringify({
          start_time: range.start,
          end_time: range.end
        })
      });
      setNotice({
        type: "success",
        title: "Square calendar synced",
        message: `Imported ${response.summary.imported}, updated ${response.summary.updated}, skipped ${response.summary.skipped}.`
      });
      await Promise.all([loadCalendarRange(salon.id, view, anchorDate), loadStatus(salon.id)]);
    } catch (err) {
      setNotice({
        type: "error",
        title: "Calendar sync failed",
        message: err instanceof Error ? err.message : "Could not sync calendar from the active POS provider."
      });
    } finally {
      setBusy("");
    }
  }

  async function signOut() {
    setBusy("logout");
    await logoutSession();
    router.replace("/login");
  }

  function updateActionForm(patch: Partial<ActionForm>) {
    setActionForm((current) => ({ ...current, ...patch }));
  }

  function resetAvailability() {
    setAvailabilityResult(null);
    setAvailabilityChecked(false);
    setAvailabilityError("");
    setCheckingAvailability(false);
    updateActionForm({ selectedSlotKey: "" });
  }

  function openCreate() {
    setActionMode("create");
    setSelectedAppointment(null);
    setActionForm({
      ...emptyActionForm(anchorDate),
      serviceID: firstBookableServiceID(services)
    });
    setActionError("");
    resetAvailability();
  }

  function openEdit(appointment: AppointmentRecord) {
    setActionMode("edit");
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
    resetAvailability();
  }

  function openDelete(appointment: AppointmentRecord) {
    setActionMode("delete");
    setSelectedAppointment(appointment);
    setActionForm({
      ...emptyActionForm(formatDateInput(new Date(appointment.start_time), salon?.timezone)),
      customerName: appointment.customer_name,
      customerPhone: appointment.customer_phone,
      customerEmail: appointment.customer_email ?? "",
      serviceID: appointmentPrimaryServiceID(appointment),
      notes: appointment.notes ?? ""
    });
    setActionError("");
    setAvailabilityResult(null);
    setAvailabilityChecked(false);
    setAvailabilityError("");
  }

  function closeActionDialog() {
    setActionMode(null);
    setSelectedAppointment(null);
    setActionError("");
    resetAvailability();
  }

  async function checkAvailability() {
    if (!salon || !actionMode || actionMode === "delete" || !actionForm.preferredDate || !readyForBooking) return;
    setAvailabilityError("");
    setAvailabilityChecked(true);
    setCheckingAvailability(true);
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
          limit: 10
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

  async function submitCreate() {
    if (!salon || !selectedActionSlot) return;
    setSavingAction(true);
    setActionError("");
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
        setNotice({
          type: "warning",
          title: "Booking needs owner review",
          message: "Square Appointments did not confirm this booking. A pending request was created instead."
        });
      } else {
        setNotice({
          type: "success",
          title: "Booking confirmed",
          message: "Square Appointments returned a booking ID before the calendar updated."
        });
      }
      closeActionDialog();
      await loadCalendarRange(salon.id, view, anchorDate);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not create booking.");
    } finally {
      setSavingAction(false);
    }
  }

  async function submitEdit() {
    if (!salon || !selectedAppointment || !selectedActionSlot) return;
    setSavingAction(true);
    setActionError("");
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
        setNotice({
          type: "warning",
          title: "Edit needs owner review",
          message: "Square Appointments did not reschedule this booking. The original appointment was left unchanged."
        });
      } else {
        setNotice({
          type: "success",
          title: "Appointment updated",
          message: "Square Appointments confirmed the new time before the calendar updated."
        });
      }
      closeActionDialog();
      await loadCalendarRange(salon.id, view, anchorDate);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not update appointment.");
    } finally {
      setSavingAction(false);
    }
  }

  async function submitDelete() {
    if (!salon || !selectedAppointment) return;
    setSavingAction(true);
    setActionError("");
    try {
      const response = await apiRequest<AppointmentRecord | BookingAttempt>(
        `/api/salons/${salon.id}/appointments/${selectedAppointment.id}/cancel`,
        {
          method: "POST",
          body: JSON.stringify({ reason: actionForm.cancelReason })
        }
      );
      if (isBookingAttempt(response)) {
        setNotice({
          type: "warning",
          title: "Delete needs owner review",
          message: "Square Appointments did not cancel this booking. The appointment remains unchanged."
        });
      } else {
        setNotice({
          type: "success",
          title: "Appointment cancelled",
          message: "Square Appointments confirmed cancellation before the calendar updated."
        });
      }
      closeActionDialog();
      await loadCalendarRange(salon.id, view, anchorDate);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not cancel appointment.");
    } finally {
      setSavingAction(false);
    }
  }

  function moveRange(direction: -1 | 1) {
    const days = view === "day" ? 1 : view === "week" ? 7 : view === "agenda" ? 14 : 32;
    const next = addDaysInput(anchorDate, days * direction);
    if (view === "month") {
      const [year, month] = anchorDate.split("-").map(Number);
      const nextMonth = new Date(year, month - 1 + direction, 1);
      setAnchorDate(formatDateInput(nextMonth));
      return;
    }
    setAnchorDate(next);
  }

  function setShortcut(offsetDays: number) {
    const next = new Date();
    next.setDate(next.getDate() + offsetDays);
    setAnchorDate(formatDateInput(next));
  }

  if (loadingShell) {
    return (
      <main className="min-h-screen bg-shell px-4 py-5 sm:px-6 lg:px-8">
        <div className="mx-auto max-w-7xl space-y-5">
          <Skeleton className="h-20" />
          <Skeleton className="h-16" />
          <div className="grid gap-4 lg:grid-cols-4">
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
            <Skeleton className="h-28" />
          </div>
          <Skeleton className="h-96" />
        </div>
      </main>
    );
  }

  if (!salon) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-shell px-4">
        <Card className="max-w-md">
          <CardTitle>No salon workspace</CardTitle>
          <CardDescription>
            Create or assign an admin salon workspace in the main frontend before opening the POS calendar.
          </CardDescription>
          <Button type="button" className="mt-5" onClick={signOut}>
            Sign out
          </Button>
        </Card>
      </main>
    );
  }

  return (
    <main className="min-h-screen bg-shell px-4 py-5 sm:px-6 lg:px-8">
      <div className="mx-auto max-w-7xl space-y-5">
        <header className="rounded-lg border border-line bg-panel p-4 shadow-soft">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <CalendarDays className="h-5 w-5 text-brand" />
                <Badge value={activeProvider} />
                <Badge value={status?.connection?.status ?? "not_connected"} />
              </div>
              <h1 className="mt-3 text-2xl font-bold text-ink sm:text-3xl">POS Calendar</h1>
              <p className="mt-1 text-sm leading-6 text-muted">
                {salon.name} · Last sync {formatOptionalDate(lastSyncAt, salon.timezone)}
              </p>
            </div>
            <div className="flex flex-col gap-2 sm:flex-row lg:items-center">
              <Button type="button" variant="secondary" onClick={syncCalendar} disabled={busy === "sync"}>
                <RefreshCcw className={cn("h-4 w-4", busy === "sync" ? "animate-spin" : "")} />
                {busy === "sync" ? "Syncing..." : "Sync Square"}
              </Button>
              <Button type="button" onClick={openCreate} disabled={!readyForBooking}>
                <Plus className="h-4 w-4" />
                Add
              </Button>
              <Button type="button" variant="ghost" onClick={signOut} disabled={busy === "logout"}>
                <LogOut className="h-4 w-4" />
                Sign out
              </Button>
            </div>
          </div>
        </header>

        <section className="rounded-lg border border-line bg-panel p-4 shadow-soft">
          <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
            <div className="grid grid-cols-2 gap-2 sm:flex">
              {views.map((item) => (
                <Button
                  key={item}
                  type="button"
                  variant={view === item ? "primary" : "secondary"}
                  onClick={() => setView(item)}
                >
                  {capitalize(item)}
                </Button>
              ))}
            </div>
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
              <div className="flex gap-2">
                <Button type="button" variant="secondary" onClick={() => setShortcut(0)}>
                  Today
                </Button>
                <Button type="button" variant="secondary" onClick={() => setShortcut(1)}>
                  Tomorrow
                </Button>
              </div>
              <div className="flex items-center gap-2">
                <Button type="button" variant="ghost" className="h-10 px-3" onClick={() => moveRange(-1)} aria-label="Previous range">
                  <ChevronLeft className="h-4 w-4" />
                </Button>
                <input
                  className="h-10 w-40 rounded-md border border-line bg-white px-3 text-sm font-semibold text-ink outline-none focus:border-brand"
                  type="date"
                  value={anchorDate}
                  onChange={(event) => setAnchorDate(event.target.value)}
                />
                <Button type="button" variant="ghost" className="h-10 px-3" onClick={() => moveRange(1)} aria-label="Next range">
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </div>
          <div className="mt-3 text-sm font-semibold text-ink">{rangeLabel(view, range, salon.timezone)}</div>
        </section>

        {error ? <Alert title="Calendar error" message={error} /> : null}
        {notice ? <Alert type={notice.type} title={notice.title} message={notice.message} /> : null}
        {!readyForBooking ? (
          <Alert
            type="warning"
            title="Booking actions gated"
            message="Connect Square Appointments, select a location, and sync bookable services and staff before adding or editing appointments."
          />
        ) : null}

        <section className="grid gap-4 md:grid-cols-4">
          <MetricCard label="Appointments" value={String(calendar?.appointments.length ?? 0)} detail="POS-confirmed records" />
          <MetricCard label="Pending" value={String(calendar?.pending_requests.length ?? 0)} detail="Owner-review fallback requests" />
          <MetricCard label="Warnings" value={String(warningTotal)} detail="Sync gaps in this range" tone={warningTotal ? "warning" : "normal"} />
          <MetricCard label="Bookable staff" value={String(bookableStaff.length)} detail={`${bookableServices.length} bookable services`} />
        </section>

        <Card className="p-0">
          <div className="border-b border-line px-5 py-4">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <CardTitle>{capitalize(view)} view</CardTitle>
                <CardDescription>
                  POS-confirmed appointments and fallback requests from the selected range.
                </CardDescription>
              </div>
              {calendar ? (
                <div className="flex flex-wrap gap-2">
                  <Badge value={`not_synced ${calendar.warnings.not_synced}`} />
                  <Badge value={`sync_failed ${calendar.warnings.sync_failed}`} />
                  <Badge value={`pending ${calendar.warnings.pending_pos_sync}`} />
                </div>
              ) : null}
            </div>
          </div>
          <div className="p-5">
            {loadingCalendar ? (
              <div className="space-y-3">
                <Skeleton className="h-12" />
                <Skeleton className="h-[34rem]" />
              </div>
            ) : view === "agenda" && items.length === 0 ? (
              <EmptyState
                title="No calendar items"
                message="POS-confirmed appointments and pending fallback requests for this range will appear here."
              />
            ) : view === "day" ? (
              <DayScheduler
                items={items}
                anchorDate={anchorDate}
                timezone={salon.timezone}
                onEdit={openEdit}
                onDelete={openDelete}
              />
            ) : view === "agenda" ? (
              <AgendaList
                items={items}
                timezone={salon.timezone}
                onEdit={openEdit}
                onDelete={openDelete}
              />
            ) : view === "week" ? (
              <WeekScheduler
                items={items}
                anchorDate={anchorDate}
                timezone={salon.timezone}
                onEdit={openEdit}
                onDelete={openDelete}
              />
            ) : (
              <MonthGrid
                items={items}
                anchorDate={anchorDate}
                timezone={salon.timezone}
                onEdit={openEdit}
                onDelete={openDelete}
              />
            )}
          </div>
        </Card>

        <ActionDialog
          mode={actionMode}
          appointment={selectedAppointment}
          form={actionForm}
          services={bookableServices}
          staff={bookableStaff}
          serviceNames={serviceNames}
          staffNames={staffNames}
          readyForBooking={readyForBooking}
          availabilityResult={availabilityResult}
          availabilityChecked={availabilityChecked}
          checkingAvailability={checkingAvailability}
          selectedSlotKey={actionForm.selectedSlotKey}
          availabilityError={availabilityError}
          saving={savingAction}
          error={actionError}
          timezone={salon.timezone}
          onChange={updateActionForm}
          onCheckAvailability={checkAvailability}
          onSlotSelect={(slot) => updateActionForm({ selectedSlotKey: slotKey(slot) })}
          onSubmitCreate={submitCreate}
          onSubmitEdit={submitEdit}
          onSubmitDelete={submitDelete}
          onClose={closeActionDialog}
        />
      </div>
    </main>
  );
}

function MetricCard({
  label,
  value,
  detail,
  tone = "normal"
}: {
  label: string;
  value: string;
  detail: string;
  tone?: "normal" | "warning";
}) {
  return (
    <Card className={tone === "warning" ? "border-amber-200 bg-amber-50" : ""}>
      <div className="text-sm font-medium text-muted">{label}</div>
      <div className="mt-2 text-3xl font-bold text-ink">{value}</div>
      <div className="mt-1 text-sm leading-6 text-muted">{detail}</div>
    </Card>
  );
}

function AgendaList({
  items,
  timezone,
  onEdit,
  onDelete
}: {
  items: CalendarItem[];
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  return (
    <div className="space-y-3">
      {items.map((item) => (
        <CalendarItemRow key={item.id} item={item} timezone={timezone} onEdit={onEdit} onDelete={onDelete} />
      ))}
    </div>
  );
}

function DayScheduler({
  items,
  anchorDate,
  timezone,
  onEdit,
  onDelete
}: {
  items: CalendarItem[];
  anchorDate: string;
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const dayItems = items
    .filter((item) => dateKey(item.start, timezone) === anchorDate)
    .sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());
  const hours = schedulerHours(dayItems, timezone);
  const positionedItems = positionSchedulerItems(dayItems, hours, timezone);

  return (
    <div className="space-y-4">
      <CalendarViewSummary label="Day view" title={formatFullInputDateLabel(anchorDate)} items={dayItems} />

      <div className="overflow-x-auto rounded-md border border-line bg-white shadow-sm">
        <div className="min-w-[720px]">
          <div className="grid grid-cols-[4.75rem_1fr] border-b border-line bg-slate-50">
            <div className="border-r border-line px-3 py-3 text-xs font-semibold uppercase text-muted">Day</div>
            <div
              className={cn(
                "px-4 py-3 text-center",
                isTodayInput(anchorDate, timezone) ? "bg-teal-50 text-brand" : "text-ink"
              )}
            >
              <div className="text-xs font-semibold uppercase text-muted">{weekdayLabel(anchorDate)}</div>
              <div
                className={cn(
                  "mx-auto mt-1 flex h-7 w-7 items-center justify-center rounded-full text-sm font-bold",
                  isTodayInput(anchorDate, timezone) ? "bg-brand text-white" : "text-ink"
                )}
              >
                {dayNumberLabel(anchorDate)}
              </div>
            </div>
          </div>
          <div className="grid grid-cols-[4.75rem_1fr] border-b border-line">
            <div className="border-r border-line bg-slate-50 px-3 py-2 text-xs font-semibold text-muted">All-day</div>
            <div className="min-h-9 bg-white px-3 py-2" />
          </div>
          <div className="grid grid-cols-[4.75rem_1fr]">
            <div>
              {hours.map((hour) => (
                <div
                  key={hour}
                  className="border-r border-line bg-slate-50 px-3 pt-2 text-right text-xs font-semibold text-muted"
                  style={{ height: schedulerRowHeight }}
                >
                  {hourLabel(hour)}
                </div>
              ))}
            </div>
            <div className="relative bg-white" style={{ height: hours.length * schedulerRowHeight }}>
              {hours.map((hour) => (
                <div
                  key={hour}
                  className="border-b border-dashed border-line last:border-b-0"
                  style={{ height: schedulerRowHeight }}
                />
              ))}
              {positionedItems.length === 0 ? (
                <div className="absolute inset-0 flex items-center justify-center text-sm font-medium text-muted">
                  No appointments scheduled
                </div>
              ) : null}
              <div className="absolute inset-x-3 top-0 bottom-0">
                {positionedItems.map((positioned) => (
                  <SchedulerEventBlock
                    key={positioned.item.id}
                    positioned={positioned}
                    timezone={timezone}
                    onEdit={onEdit}
                    onDelete={onDelete}
                  />
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function WeekScheduler({
  items,
  anchorDate,
  timezone,
  onEdit,
  onDelete
}: {
  items: CalendarItem[];
  anchorDate: string;
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const weekStart = startOfWeekInput(anchorDate);
  const days = Array.from({ length: 7 }, (_, index) => addDaysInput(weekStart, index));
  const weekItems = items.filter((item) => days.includes(dateKey(item.start, timezone)));
  const hours = schedulerHours(weekItems, timezone);
  const gridHeight = hours.length * schedulerRowHeight;

  return (
    <div className="space-y-4">
      <CalendarViewSummary label="Week view" title={rangeLabel("week", rangeForView("week", anchorDate), timezone)} items={weekItems} />

      <div className="overflow-x-auto rounded-md border border-line bg-white shadow-sm">
        <div className="min-w-[1080px]">
          <div className="grid grid-cols-[4.75rem_repeat(7,minmax(8.75rem,1fr))] border-b border-line bg-slate-50">
            <div className="border-r border-line px-3 py-3 text-xs font-semibold uppercase text-muted">Week</div>
            {days.map((day) => (
              <div
                key={day}
                className={cn(
                  "border-r border-line px-3 py-2 text-center last:border-r-0",
                  isTodayInput(day, timezone) ? "bg-teal-50 text-brand" : "text-ink"
                )}
              >
                <div className="text-xs font-semibold uppercase text-muted">{weekdayLabel(day)}</div>
                <div
                  className={cn(
                    "mx-auto mt-1 flex h-7 w-7 items-center justify-center rounded-full text-sm font-bold",
                    isTodayInput(day, timezone) ? "bg-brand text-white" : "text-ink"
                  )}
                >
                  {dayNumberLabel(day)}
                </div>
              </div>
            ))}
          </div>
          <div className="grid grid-cols-[4.75rem_repeat(7,minmax(8.75rem,1fr))] border-b border-line">
            <div className="border-r border-line bg-slate-50 px-3 py-2 text-xs font-semibold text-muted">All-day</div>
            {days.map((day) => (
              <div
                key={day}
                className={cn(
                  "min-h-9 border-r border-line px-2 py-2 last:border-r-0",
                  isTodayInput(day, timezone) ? "bg-teal-50/40" : "bg-white"
                )}
              />
            ))}
          </div>
          <div className="grid grid-cols-[4.75rem_repeat(7,minmax(8.75rem,1fr))]">
            <div>
              {hours.map((hour) => (
                <div
                  key={hour}
                  className="border-r border-line bg-slate-50 px-3 pt-2 text-right text-xs font-semibold text-muted"
                  style={{ height: schedulerRowHeight }}
                >
                  {hourLabel(hour)}
                </div>
              ))}
            </div>
            {days.map((day) => {
              const dayItems = items.filter((item) => dateKey(item.start, timezone) === day);
              const positionedItems = positionSchedulerItems(dayItems, hours, timezone);
              return (
                <div
                  key={day}
                  className={cn(
                    "relative border-r border-line last:border-r-0",
                    isTodayInput(day, timezone) ? "bg-teal-50/30" : "bg-white"
                  )}
                  style={{ height: gridHeight }}
                >
                  {hours.map((hour) => (
                    <div
                      key={hour}
                      className="border-b border-dashed border-line last:border-b-0"
                      style={{ height: schedulerRowHeight }}
                    />
                  ))}
                  <div className="absolute inset-x-2 top-0 bottom-0">
                    {positionedItems.map((positioned) => (
                      <SchedulerEventBlock
                        key={positioned.item.id}
                        positioned={positioned}
                        timezone={timezone}
                        compact
                        onEdit={onEdit}
                        onDelete={onDelete}
                      />
                    ))}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

function MonthGrid({
  items,
  anchorDate,
  timezone,
  onEdit,
  onDelete
}: {
  items: CalendarItem[];
  anchorDate: string;
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const days = monthGridDays(anchorDate);
  const monthItems = items.filter((item) => dateKey(item.start, timezone).slice(0, 7) === anchorDate.slice(0, 7));
  return (
    <div className="space-y-4">
      <CalendarViewSummary label="Month view" title={monthTitle(anchorDate)} items={monthItems} />

      <div className="overflow-x-auto rounded-md border border-line bg-white shadow-sm">
        <div className="min-w-[940px]">
          <div className="grid grid-cols-7 border-b border-line bg-slate-50 text-xs font-semibold uppercase text-muted">
            {weekdayLabels.map((label) => (
              <div key={label} className="border-r border-line px-3 py-2 last:border-r-0">
                {label}
              </div>
            ))}
          </div>
          <div className="grid grid-cols-7">
            {days.map((day) => {
              const dayItems = items.filter((item) => dateKey(item.start, timezone) === day);
              const muted = day.slice(0, 7) !== anchorDate.slice(0, 7);
              const warnings = warningCount(dayItems);
              return (
                <div
                  key={day}
                  className={cn(
                    "min-h-36 border-r border-b border-line bg-white p-3 last:border-r-0",
                    muted ? "bg-slate-50 text-muted" : "",
                    isTodayInput(day, timezone) ? "relative z-10 ring-2 ring-brand ring-inset" : ""
                  )}
                >
                  <div className="mb-2 flex items-start justify-between gap-2">
                    <div
                      className={cn(
                        "flex h-7 min-w-7 items-center justify-center rounded-full px-2 text-sm font-bold",
                        isTodayInput(day, timezone) ? "bg-brand text-white" : muted ? "text-slate-400" : "text-ink"
                      )}
                    >
                      {monthCellDayLabel(day)}
                    </div>
                    {warnings > 0 ? <AlertTriangle className="h-4 w-4 flex-none text-amber-600" /> : null}
                  </div>
                  <div className="space-y-1.5">
                    {dayItems.slice(0, 3).map((item) => (
                      <MonthEventChip
                        key={item.id}
                        item={item}
                        timezone={timezone}
                        onEdit={onEdit}
                        onDelete={onDelete}
                      />
                    ))}
                    {dayItems.length > 3 ? (
                      <div className="rounded-md bg-slate-100 px-2 py-1 text-xs font-semibold text-muted">
                        +{dayItems.length - 3} more
                      </div>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

function CalendarViewSummary({
  label,
  title,
  items
}: {
  label: string;
  title: string;
  items: CalendarItem[];
}) {
  const warnings = warningCount(items);
  return (
    <div className="flex flex-col gap-3 rounded-md border border-line bg-slate-50 px-4 py-3 lg:flex-row lg:items-center lg:justify-between">
      <div>
        <div className="text-xs font-bold uppercase tracking-wide text-muted">{label}</div>
        <div className="mt-1 text-lg font-bold text-ink">{title}</div>
      </div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <CalendarLegend />
        <div className="flex flex-wrap gap-2">
          <CountPill value={String(items.length)} label={items.length === 1 ? "event" : "events"} />
          <CountPill value={String(warnings)} label="warnings" tone={warnings > 0 ? "warning" : "neutral"} />
        </div>
      </div>
    </div>
  );
}

function CalendarLegend() {
  return (
    <div className="flex flex-wrap items-center gap-3 text-xs font-semibold text-muted">
      <span className="inline-flex items-center gap-1.5">
        <span className="h-2 w-2 rounded-full bg-emerald-500" />
        confirmed
      </span>
      <span className="inline-flex items-center gap-1.5">
        <span className="h-2 w-2 rounded-full bg-amber-500" />
        pending
      </span>
      <span className="inline-flex items-center gap-1.5">
        <span className="h-2 w-2 rounded-full bg-red-500" />
        warning
      </span>
    </div>
  );
}

function SchedulerEventBlock({
  positioned,
  timezone,
  compact = false,
  onEdit,
  onDelete
}: {
  positioned: PositionedCalendarItem;
  timezone?: string;
  compact?: boolean;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const item = positioned.item;
  const appointment = item.appointment;
  return (
    <div
      className={cn(
        "absolute overflow-hidden rounded-md border p-2 text-left shadow-sm transition hover:shadow-md",
        item.warning ? "border-amber-300 bg-amber-50" : item.kind === "pending" ? "border-amber-200 bg-amber-50" : "border-emerald-200 bg-emerald-50"
      )}
      style={{
        top: positioned.top,
        height: positioned.height,
        left: `calc(${(positioned.lane / positioned.laneCount) * 100}% + 2px)`,
        width: `calc(${100 / positioned.laneCount}% - 4px)`
      }}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-xs font-bold text-ink">
            {formatTime(item.start, timezone)} · {item.customerName || "Unknown"}
          </div>
          <div className={cn("mt-1 text-xs leading-5 text-muted", compact ? "line-clamp-2" : "line-clamp-3")}>
            {compactServiceLabel(item)}
          </div>
        </div>
        {item.warning ? <AlertTriangle className="h-4 w-4 flex-none text-amber-600" /> : null}
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <Badge value={item.kind === "pending" ? "fallback_pending" : item.status} className="px-2 py-0.5" />
        {appointment ? (
          <>
            <Button
              type="button"
              variant="secondary"
              className="h-6 w-6 px-0"
              onClick={() => onEdit(appointment)}
              disabled={!canEditAppointment(appointment)}
              aria-label="Edit appointment"
            >
              <Pencil className="h-3 w-3" />
            </Button>
            <Button
              type="button"
              variant="danger"
              className="h-6 w-6 px-0"
              onClick={() => onDelete(appointment)}
              disabled={!canDeleteAppointment(appointment)}
              aria-label="Delete appointment"
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </>
        ) : null}
      </div>
    </div>
  );
}

function MonthEventChip({
  item,
  timezone,
  onEdit,
  onDelete
}: {
  item: CalendarItem;
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const appointment = item.appointment;
  return (
    <div
      className={cn(
        "group flex items-start gap-1.5 rounded-md border px-2 py-1.5 text-xs",
        item.warning ? "border-amber-300 bg-amber-50" : item.kind === "pending" ? "border-amber-200 bg-amber-50" : "border-emerald-200 bg-emerald-50"
      )}
    >
      {appointment ? (
        <button
          type="button"
          className="min-w-0 flex-1 text-left"
          onClick={() => onEdit(appointment)}
          disabled={!canEditAppointment(appointment)}
          aria-label="Edit appointment"
        >
          <span className="block truncate font-bold text-ink">
            {formatTime(item.start, timezone)} · {item.customerName || "Unknown"}
          </span>
          <span className="block truncate leading-5 text-muted">{compactServiceLabel(item)}</span>
        </button>
      ) : (
        <div className="min-w-0 flex-1">
          <div className="truncate font-bold text-ink">
            {formatTime(item.start, timezone)} · {item.customerName || "Unknown"}
          </div>
          <div className="truncate leading-5 text-muted">{compactServiceLabel(item)}</div>
        </div>
      )}
      {item.warning ? <AlertTriangle className="mt-0.5 h-3.5 w-3.5 flex-none text-amber-600" /> : null}
      {appointment ? (
        <button
          type="button"
          className="hidden h-5 w-5 flex-none items-center justify-center rounded border border-red-200 bg-white text-red-700 hover:bg-red-50 group-hover:flex disabled:text-slate-300"
          onClick={() => onDelete(appointment)}
          disabled={!canDeleteAppointment(appointment)}
          aria-label="Delete appointment"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      ) : null}
    </div>
  );
}

function CalendarItemRow({
  item,
  timezone,
  onEdit,
  onDelete
}: {
  item: CalendarItem;
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const appointment = item.appointment;
  return (
    <div className="grid gap-3 rounded-md border border-line bg-white p-4 md:grid-cols-[11rem_1fr_auto] md:items-start">
      <div>
        <div className="text-sm font-semibold text-ink">{formatTimeRange(item.start, item.end, timezone)}</div>
        <div className="mt-1 text-xs text-muted">{formatDate(item.start, timezone)}</div>
      </div>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <div className="text-sm font-semibold text-ink">{item.customerName || "Unknown customer"}</div>
          <Badge value={item.status} />
          {item.kind === "pending" ? <Badge value="fallback_pending" /> : null}
        </div>
        <div className="mt-1 text-sm leading-6 text-muted">{item.subtitle}</div>
        <div className="mt-1 text-xs leading-5 text-muted">{item.detail}</div>
        {item.warning ? (
          <div className="mt-3 flex gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-900">
            <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
            <span>{item.warning}</span>
          </div>
        ) : null}
      </div>
      <div className="flex flex-wrap gap-2 md:justify-end">
        {appointment ? (
          <>
            <Button type="button" variant="secondary" className="h-9 px-3" onClick={() => onEdit(appointment)} disabled={!canEditAppointment(appointment)}>
              <Pencil className="h-4 w-4" />
              Edit
            </Button>
            <Button type="button" variant="danger" className="h-9 px-3" onClick={() => onDelete(appointment)} disabled={!canDeleteAppointment(appointment)}>
              <Trash2 className="h-4 w-4" />
              Delete
            </Button>
          </>
        ) : (
          <Badge value="needs_review" />
        )}
      </div>
    </div>
  );
}

function CalendarItemActions({
  appointment,
  onEdit,
  onDelete
}: {
  appointment?: AppointmentRecord;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  if (!appointment) {
    return <Badge value="needs_review" />;
  }
  return (
    <div className="flex flex-wrap gap-2 lg:justify-end">
      <Button type="button" variant="secondary" className="h-9 px-3" onClick={() => onEdit(appointment)} disabled={!canEditAppointment(appointment)}>
        <Pencil className="h-4 w-4" />
        Edit
      </Button>
      <Button type="button" variant="danger" className="h-9 px-3" onClick={() => onDelete(appointment)} disabled={!canDeleteAppointment(appointment)}>
        <Trash2 className="h-4 w-4" />
        Delete
      </Button>
    </div>
  );
}

function CountPill({
  value,
  label,
  tone = "neutral"
}: {
  value: string;
  label: string;
  tone?: "neutral" | "warning";
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold ring-1",
        tone === "warning" ? "bg-amber-50 text-amber-700 ring-amber-200" : "bg-white text-muted ring-line"
      )}
    >
      <span className="text-ink">{value}</span>
      {label}
    </span>
  );
}

function ActionDialog({
  mode,
  appointment,
  form,
  services,
  staff,
  serviceNames,
  staffNames,
  readyForBooking,
  availabilityResult,
  availabilityChecked,
  checkingAvailability,
  selectedSlotKey,
  availabilityError,
  saving,
  error,
  timezone,
  onChange,
  onCheckAvailability,
  onSlotSelect,
  onSubmitCreate,
  onSubmitEdit,
  onSubmitDelete,
  onClose
}: {
  mode: ActionMode | null;
  appointment: AppointmentRecord | null;
  form: ActionForm;
  services: POSService[];
  staff: POSStaffMember[];
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
  readyForBooking: boolean;
  availabilityResult: AvailabilityResult | null;
  availabilityChecked: boolean;
  checkingAvailability: boolean;
  selectedSlotKey: string;
  availabilityError: string;
  saving: boolean;
  error: string;
  timezone?: string;
  onChange: (patch: Partial<ActionForm>) => void;
  onCheckAvailability: () => void;
  onSlotSelect: (slot: AvailabilitySlot) => void;
  onSubmitCreate: () => void;
  onSubmitEdit: () => void;
  onSubmitDelete: () => void;
  onClose: () => void;
}) {
  if (!mode) return null;

  const title = mode === "create" ? "Add appointment" : mode === "edit" ? "Edit appointment" : "Delete appointment";
  const selectedServiceName = form.serviceID ? serviceNames.get(form.serviceID) || "Unknown service" : "No service";
  const selectedStaffName = appointment ? assignedTechniciansLabel(appointment, staffNames) : "Not assigned";

  return (
    <Dialog
      open={Boolean(mode)}
      title={title}
      description={actionDescription(mode)}
      onClose={onClose}
      closeDisabled={saving}
      className={mode === "delete" ? "max-w-2xl" : "max-w-4xl"}
    >
      {error ? <Alert title="Action failed" message={error} /> : null}
      {mode === "delete" ? (
        <div className="mt-5 grid gap-5">
          <AppointmentSummary appointment={appointment} serviceNames={serviceNames} staffNames={staffNames} timezone={timezone} />
          <label className="block">
            <span className="text-sm font-medium text-ink">Cancellation reason</span>
            <textarea
              className={textareaClassName}
              value={form.cancelReason}
              onChange={(event) => onChange({ cancelReason: event.target.value })}
              placeholder="Customer requested cancellation"
              disabled={saving}
            />
          </label>
          <Alert
            type="warning"
            title="POS cancel required"
            message="The appointment history is kept. The calendar marks it cancelled only after Square Appointments confirms the cancellation."
          />
          <div className="flex flex-col gap-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="ghost" onClick={onClose} disabled={saving}>
              Close
            </Button>
            <Button type="button" variant="danger" onClick={onSubmitDelete} disabled={!appointment || saving}>
              {saving ? "Cancelling..." : "Delete"}
            </Button>
          </div>
        </div>
      ) : (
        <div className="mt-5 grid gap-5">
          {mode === "edit" ? (
            <Alert
              type="warning"
              title="Limited edit mode"
              message="This slice supports POS-backed time, staff, and note changes. Customer or service changes stay gated until the POS update contract is verified."
            />
          ) : null}
          <div className="grid gap-4 md:grid-cols-3">
            <label className="block">
              <span className="text-sm font-medium text-ink">Customer name</span>
              <input
                className={inputClassName}
                value={form.customerName}
                onChange={(event) => onChange({ customerName: event.target.value })}
                disabled={saving || mode === "edit"}
                required
              />
            </label>
            <label className="block">
              <span className="text-sm font-medium text-ink">Phone</span>
              <input
                className={inputClassName}
                value={form.customerPhone}
                onChange={(event) => onChange({ customerPhone: event.target.value })}
                disabled={saving || mode === "edit"}
                required
              />
            </label>
            <label className="block">
              <span className="text-sm font-medium text-ink">Email</span>
              <input
                className={inputClassName}
                value={form.customerEmail}
                onChange={(event) => onChange({ customerEmail: event.target.value })}
                disabled={saving || mode === "edit"}
                type="email"
              />
            </label>
          </div>
          <div className="grid gap-4 md:grid-cols-3">
            {mode === "create" ? (
              <label className="block">
                <span className="text-sm font-medium text-ink">Service</span>
                <select
                  className={inputClassName}
                  value={form.serviceID}
                  onChange={(event) => onChange({ serviceID: event.target.value, selectedSlotKey: "" })}
                  disabled={saving}
                  required
                >
                  <option value="">Select service</option>
                  {services.map((service) => (
                    <option key={service.id} value={service.id}>
                      {service.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <ReadOnlyField label="Service" value={selectedServiceName} />
            )}
            <label className="block">
              <span className="text-sm font-medium text-ink">Technician</span>
              <select
                className={inputClassName}
                value={form.staffID}
                onChange={(event) => onChange({ staffID: event.target.value, selectedSlotKey: "" })}
                disabled={saving}
              >
                <option value="">{mode === "create" ? "Anyone available" : selectedStaffName}</option>
                {staff.map((member) => (
                  <option key={member.id} value={member.id}>
                    {member.name}
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
                disabled={saving}
                required
              />
            </label>
          </div>
          <label className="block">
            <span className="text-sm font-medium text-ink">Notes</span>
            <textarea
              className={textareaClassName}
              value={form.notes}
              onChange={(event) => onChange({ notes: event.target.value })}
              disabled={saving}
            />
          </label>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="text-sm leading-6 text-muted">
              Availability is returned by Square Appointments before any booking action is submitted.
            </div>
            <Button type="button" variant="secondary" onClick={onCheckAvailability} disabled={!readyForBooking || checkingAvailability || saving}>
              <Clock className="h-4 w-4" />
              {checkingAvailability ? "Checking..." : "Check availability"}
            </Button>
          </div>
          {availabilityError ? <Alert title="Availability failed" message={availabilityError} /> : null}
          <AvailabilitySlots
            checked={availabilityChecked}
            loading={checkingAvailability}
            result={availabilityResult}
            selectedSlotKey={selectedSlotKey}
            timezone={timezone}
            onSelect={onSlotSelect}
          />
          <div className="flex flex-col gap-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="ghost" onClick={onClose} disabled={saving}>
              Close
            </Button>
            <Button
              type="button"
              onClick={mode === "create" ? onSubmitCreate : onSubmitEdit}
              disabled={saving || !selectedSlotKey}
            >
              {saving ? "Saving..." : mode === "create" ? "Add" : "Save"}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}

function AvailabilitySlots({
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
    return <EmptyState title="No available slots returned" message="Try another date, service, or technician before submitting this POS action." />;
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
                <div className="text-sm font-semibold text-ink">{formatTimeRange(slot.start_time, slot.end_time, timezone)}</div>
                <div className="mt-1 text-sm leading-6 text-muted">Assigned: {assignedTechniciansLabel(slot)}</div>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Badge value={selected ? "selected" : "available"} />
                <Button type="button" variant={selected ? "primary" : "secondary"} onClick={() => onSelect(slot)}>
                  {selected ? "Selected" : "Use this slot"}
                </Button>
              </div>
            </div>
            <SegmentList record={slot} />
          </div>
        );
      })}
    </div>
  );
}

function AppointmentSummary({
  appointment,
  serviceNames,
  staffNames,
  timezone
}: {
  appointment: AppointmentRecord | null;
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
  timezone?: string;
}) {
  if (!appointment) {
    return <EmptyState title="No appointment selected" message="Close this dialog and select an appointment from the calendar." />;
  }
  return (
    <div className="rounded-md border border-line bg-slate-50 p-4 text-sm leading-6">
      <div className="font-semibold text-ink">{appointment.customer_name || "Unknown customer"}</div>
      <div className="text-muted">{formatTimeRange(appointment.start_time, appointment.end_time, timezone)}</div>
      <div className="text-muted">{serviceNamesLabel(appointment, serviceNames)} · {assignedTechniciansLabel(appointment, staffNames)}</div>
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

function SegmentList({ record }: { record: AppointmentRecord | BookingAttempt | AvailabilitySlot }) {
  const segments = orderedSegments(record);
  if (segments.length === 0) return null;
  return (
    <div className="mt-3 grid gap-2">
      {segments.map((segment, index) => (
        <div key={`${segment.service_id ?? "service"}-${segment.staff_id ?? "staff"}-${index}`} className="text-xs leading-5 text-muted">
          {segment.service_name || "Service"} · {segment.staff_name || "Assigned technician"} · {segment.duration_minutes ?? 0} min
        </div>
      ))}
    </div>
  );
}

function EmptyState({ title, message }: { title: string; message: string }) {
  return (
    <div className="rounded-md border border-dashed border-line bg-slate-50 p-6 text-center">
      <CalendarClock className="mx-auto h-6 w-6 text-muted" />
      <div className="mt-3 text-sm font-semibold text-ink">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted">{message}</div>
    </div>
  );
}

function buildCalendarItems(
  calendar: CalendarRangeResponse | null,
  serviceNames: Map<string, string>,
  staffNames: Map<string, string>,
  timezone?: string
): CalendarItem[] {
  if (!calendar) return [];
  const appointments: CalendarItem[] = calendar.appointments.map((item) => ({
    id: `appointment-${item.id}`,
    kind: "appointment",
    start: item.start_time,
    end: item.end_time,
    status: item.status,
    customerName: item.customer_name,
    subtitle: bookingSummaryLabel(item, serviceNames, staffNames),
    detail: item.pos_appointment_id ? `${item.pos_provider}: ${item.pos_appointment_id}` : "POS booking ID missing",
    warning: appointmentWarning(item),
    appointment: item
  }));
  const pending: CalendarItem[] = calendar.pending_requests.map((item) => ({
    id: `pending-${item.id}`,
    kind: "pending",
    start: item.requested_start_time,
    end: item.requested_end_time,
    status: item.status,
    customerName: item.customer_name,
    subtitle: bookingSummaryLabel(item, serviceNames, staffNames),
    detail: item.error_code || formatDate(item.requested_start_time, timezone),
    warning: item.sync_warning || item.error_message || "This request is not confirmed in POS and needs owner review.",
    request: item
  }));
  return [...appointments, ...pending].sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());
}

function appointmentWarning(item: AppointmentRecord) {
  if (item.sync_warning) return item.sync_warning;
  if (item.pos_sync_status === "sync_failed") {
    return item.pos_sync_error || "Latest POS calendar sync failed for this appointment.";
  }
  if (item.pos_sync_status === "not_synced") {
    return "This appointment has not been synced from the active POS calendar yet.";
  }
  if (item.pos_sync_status === "pending") {
    return "This appointment is waiting for POS sync verification.";
  }
  if (!item.pos_appointment_id) {
    return "POS booking ID is missing, so this appointment is not treated as confirmed.";
  }
  return "";
}

function canEditAppointment(appointment: AppointmentRecord) {
  if (appointment.can_edit === false) return false;
  return appointment.status !== "cancelled" && Boolean(appointment.pos_appointment_id);
}

function canDeleteAppointment(appointment: AppointmentRecord) {
  if (appointment.can_delete === false) return false;
  return appointment.status !== "cancelled" && Boolean(appointment.pos_appointment_id);
}

function actionDescription(mode: ActionMode) {
  if (mode === "create") {
    return "Check Square availability, select a returned slot, then submit the booking.";
  }
  if (mode === "edit") {
    return "Change time, technician, or notes through the POS-backed reschedule flow.";
  }
  return "Cancel through the active POS provider; local history is preserved.";
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
  return connected && locationSelected && (readiness?.service_count ?? 0) > 0 && (readiness?.staff_count ?? 0) > 0 && !readiness?.booking_write_blocked;
}

function emptyActionForm(preferredDate: string): ActionForm {
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

function firstBookableServiceID(services: POSService[]) {
  return services.find(serviceIsBookable)?.id ?? "";
}

function actionAvailabilitySegments(
  mode: ActionMode,
  form: ActionForm,
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
  if (mode !== "edit" || !appointment) {
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
  const segment = orderedSegments(appointment).find((item) => item.service_id);
  return segment?.service_id ?? appointment.service_id ?? "";
}

function isBookingAttempt(value: AppointmentRecord | BookingAttempt): value is BookingAttempt {
  return "requested_start_time" in value;
}

function orderedSegments(record: AppointmentRecord | BookingAttempt | AvailabilitySlot): DisplaySegment[] {
  const segments = (record.segments ?? []) as DisplaySegment[];
  return [...segments].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0));
}

function bookingSummaryLabel(
  record: AppointmentRecord | BookingAttempt | AvailabilitySlot,
  serviceNames?: Map<string, string>,
  staffNames?: Map<string, string>
) {
  return `${serviceNamesLabel(record, serviceNames)} · Preference: ${technicianPreferenceLabel(record)} · Assigned: ${assignedTechniciansLabel(record, staffNames)}`;
}

function serviceNamesLabel(record: AppointmentRecord | BookingAttempt | AvailabilitySlot, serviceNames?: Map<string, string>) {
  const segments = orderedSegments(record);
  if (segments.length > 0) {
    return segments
      .map((segment) => segment.service_name || (segment.service_id ? serviceNames?.get(segment.service_id) : "") || "Unknown service")
      .join(" + ");
  }
  const serviceID = "service_id" in record ? record.service_id : "";
  return (serviceID ? serviceNames?.get(serviceID) : "") || "Unknown service";
}

function technicianPreferenceLabel(record: AppointmentRecord | BookingAttempt | AvailabilitySlot) {
  if (record.staff_selection_mode === "anyone") {
    return "Anyone available";
  }
  const hasAnyoneSegment = orderedSegments(record).some((segment) => segment.staff_selection_mode === "anyone");
  return hasAnyoneSegment ? "Anyone available" : "Specific technician";
}

function assignedTechniciansLabel(record: AppointmentRecord | BookingAttempt | AvailabilitySlot, staffNames?: Map<string, string>) {
  const names = orderedSegments(record)
    .map((segment) => segment.staff_name || (segment.staff_id ? staffNames?.get(segment.staff_id) : "") || "")
    .filter(Boolean);
  if (names.length > 0) {
    return [...new Set(names)].join(", ");
  }
  if ("staff_name" in record && record.staff_name) {
    return record.staff_name;
  }
  if ("staff_id" in record && record.staff_id) {
    return staffNames?.get(record.staff_id) || "Assigned technician";
  }
  return record.staff_selection_mode === "anyone" ? "Anyone available" : "Unassigned technician";
}

function slotKey(slot: AvailabilitySlot) {
  return `${slot.start_time}-${slot.end_time}-${slot.staff_id || assignedTechniciansLabel(slot)}`;
}

function rangeForView(view: CalendarView, anchorDate: string) {
  if (view === "day") {
    return { start: anchorDate, end: addDaysInput(anchorDate, 1) };
  }
  if (view === "week") {
    const start = startOfWeekInput(anchorDate);
    return { start, end: addDaysInput(start, 7) };
  }
  if (view === "month") {
    const [year, month] = anchorDate.split("-").map(Number);
    const start = formatDateInput(new Date(year, month - 1, 1));
    const end = formatDateInput(new Date(year, month, 1));
    return { start, end };
  }
  return { start: anchorDate, end: addDaysInput(anchorDate, 14) };
}

function rangeLabel(view: CalendarView, range: { start: string; end: string }, timezone?: string) {
  if (view === "day") {
    return formatInputDateLabel(range.start);
  }
  const endInclusive = addDaysInput(range.end, -1);
  return `${formatInputDateLabel(range.start)} - ${formatInputDateLabel(endInclusive)}`;
}

function startOfWeekInput(value: string) {
  const date = inputDateToLocalDate(value);
  const offset = date.getDay();
  date.setDate(date.getDate() - offset);
  return formatDateInput(date);
}

function monthGridDays(anchorDate: string) {
  const [year, month] = anchorDate.split("-").map(Number);
  const first = new Date(year, month - 1, 1);
  const gridStart = new Date(first);
  gridStart.setDate(first.getDate() - first.getDay());
  return Array.from({ length: 42 }, (_, index) => {
    const day = new Date(gridStart);
    day.setDate(gridStart.getDate() + index);
    return formatDateInput(day);
  });
}

function addDaysInput(value: string, days: number) {
  const date = inputDateToLocalDate(value);
  date.setDate(date.getDate() + days);
  return formatDateInput(date);
}

function inputDateToLocalDate(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

function formatDateInput(date: Date, timezone?: string) {
  if (timezone) {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit"
    }).formatToParts(date);
    const year = parts.find((part) => part.type === "year")?.value;
    const month = parts.find((part) => part.type === "month")?.value;
    const day = parts.find((part) => part.type === "day")?.value;
    if (year && month && day) return `${year}-${month}-${day}`;
  }
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function dateKey(value: string, timezone?: string) {
  return formatDateInput(new Date(value), timezone);
}

function formatInputDateLabel(value: string) {
  const date = inputDateToLocalDate(value);
  return date.toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric"
  });
}

function formatFullInputDateLabel(value: string) {
  const date = inputDateToLocalDate(value);
  return date.toLocaleDateString(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
    year: "numeric"
  });
}

function formatMonthDayLabel(value: string) {
  const date = inputDateToLocalDate(value);
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric"
  });
}

function isTodayInput(value: string, timezone?: string) {
  return value === formatDateInput(new Date(), timezone);
}

function warningCount(items: CalendarItem[]) {
  return items.filter((item) => item.warning).length;
}

function schedulerHours(items: CalendarItem[], timezone?: string) {
  const startHours = items.map((item) => Math.floor(localMinutes(item.start, timezone) / 60));
  const endHours = items.map((item) => Math.max(Math.ceil(localMinutes(item.end, timezone) / 60) - 1, Math.floor(localMinutes(item.start, timezone) / 60)));
  const first = Math.max(0, Math.min(defaultSchedulerStartHour, ...startHours));
  const last = Math.min(23, Math.max(defaultSchedulerEndHour, ...endHours));
  return Array.from({ length: last - first + 1 }, (_, index) => first + index);
}

function positionSchedulerItems(items: CalendarItem[], hours: number[], timezone?: string): PositionedCalendarItem[] {
  if (items.length === 0 || hours.length === 0) return [];
  const startBoundary = hours[0] * 60;
  const endBoundary = (hours[hours.length - 1] + 1) * 60;
  const gridHeight = hours.length * schedulerRowHeight;
  const entries = items
    .map((item) => {
      const rawStart = localMinutes(item.start, timezone);
      const rawEnd = localMinutes(item.end, timezone);
      const start = clamp(rawStart, startBoundary, endBoundary - 15);
      const end = clamp(rawEnd > rawStart ? rawEnd : rawStart + 30, start + 15, endBoundary);
      return { item, start, end };
    })
    .sort((a, b) => a.start - b.start || a.end - b.end);

  const positioned: PositionedCalendarItem[] = [];
  let group: typeof entries = [];
  let groupEnd = -1;

  function flushGroup() {
    if (group.length === 0) return;
    const laneEnds: number[] = [];
    const assignments = group.map((entry) => {
      const existingLane = laneEnds.findIndex((end) => end <= entry.start);
      const lane = existingLane >= 0 ? existingLane : laneEnds.length;
      laneEnds[lane] = entry.end;
      return { ...entry, lane };
    });
    const laneCount = Math.max(1, laneEnds.length);
    assignments.forEach((entry) => {
      const top = ((entry.start - startBoundary) / 60) * schedulerRowHeight;
      const rawHeight = ((entry.end - entry.start) / 60) * schedulerRowHeight - 4;
      positioned.push({
        item: entry.item,
        top,
        height: Math.min(Math.max(38, rawHeight), Math.max(38, gridHeight - top - 2)),
        lane: entry.lane,
        laneCount
      });
    });
    group = [];
    groupEnd = -1;
  }

  entries.forEach((entry) => {
    if (group.length === 0 || entry.start < groupEnd) {
      group.push(entry);
      groupEnd = Math.max(groupEnd, entry.end);
      return;
    }
    flushGroup();
    group.push(entry);
    groupEnd = entry.end;
  });
  flushGroup();
  return positioned;
}

function localMinutes(value: string, timezone?: string) {
  if (!value) return 0;
  const date = new Date(value);
  if (!timezone) return date.getHours() * 60 + date.getMinutes();
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: timezone,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23"
  }).formatToParts(date);
  const hour = Number(parts.find((part) => part.type === "hour")?.value ?? "0");
  const minute = Number(parts.find((part) => part.type === "minute")?.value ?? "0");
  return hour * 60 + minute;
}

function hourLabel(hour: number) {
  const suffix = hour >= 12 ? "PM" : "AM";
  const display = hour % 12 === 0 ? 12 : hour % 12;
  return `${display} ${suffix}`;
}

function compactServiceLabel(item: CalendarItem) {
  const parts = item.subtitle.split(" · ");
  const services = parts[0] || "Unknown service";
  const assigned = parts.find((part) => part.startsWith("Assigned: "))?.replace("Assigned: ", "");
  return assigned ? `${services} · ${assigned}` : services;
}

function weekdayLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, { weekday: "short" });
}

function dayNumberLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, { day: "numeric" });
}

function monthCellDayLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, { day: "numeric" });
}

function monthTitle(value: string) {
  const date = inputDateToLocalDate(value);
  return date.toLocaleDateString(undefined, { month: "long", year: "numeric" });
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

function formatDate(value: string, timezone?: string) {
  if (!value) return "Not scheduled";
  return new Date(value).toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: timezone
  });
}

function formatTime(value: string, timezone?: string) {
  if (!value) return "";
  return new Date(value).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timezone
  });
}

function formatTimeRange(start: string, end: string, timezone?: string) {
  return `${formatTime(start, timezone)} - ${formatTime(end, timezone)}`;
}

function formatOptionalDate(value?: string, timezone?: string) {
  if (!value) return "not synced";
  return new Date(value).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: timezone
  });
}

function calendarLatestSync(calendar: CalendarRangeResponse | null) {
  if (!calendar) return "";
  const values = calendar.appointments
    .map((item) => item.last_pos_synced_at)
    .filter((value): value is string => Boolean(value));
  values.sort((a, b) => new Date(b).getTime() - new Date(a).getTime());
  return values[0] ?? "";
}

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
