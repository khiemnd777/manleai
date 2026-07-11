"use client";

import { useEffect, useMemo, useRef, useState } from "react";
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
  Trash2,
  X
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest, apiStream, getAccessToken, logoutSession } from "@/lib/api/client";
import { cn } from "@/lib/utils/cn";
import type {
  AvailabilityResult,
  AvailabilitySlot,
  AppointmentRecord,
  BookingAttempt,
  BookingSegmentRequest,
  CalendarEvent,
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

type CalendarToast = {
  id: string;
  type: "success" | "warning";
  title: string;
  message: string;
  event: CalendarEvent;
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
  const [calendarToasts, setCalendarToasts] = useState<CalendarToast[]>([]);
  const [actionMode, setActionMode] = useState<ActionMode | null>(null);
  const [selectedAppointment, setSelectedAppointment] = useState<AppointmentRecord | null>(null);
  const [selectedCalendarItemID, setSelectedCalendarItemID] = useState("");
  const [pendingFocusItemID, setPendingFocusItemID] = useState("");
  const [detailOpen, setDetailOpen] = useState(false);
  const [selectedDayKey, setSelectedDayKey] = useState("");
  const [dayDrawerOpen, setDayDrawerOpen] = useState(false);
  const [actionForm, setActionForm] = useState<ActionForm>(() => emptyActionForm(formatDateInput(new Date())));
  const [availabilityResult, setAvailabilityResult] = useState<AvailabilityResult | null>(null);
  const [availabilityChecked, setAvailabilityChecked] = useState(false);
  const [availabilityError, setAvailabilityError] = useState("");
  const [checkingAvailability, setCheckingAvailability] = useState(false);
  const [savingAction, setSavingAction] = useState(false);
  const [actionError, setActionError] = useState("");
  const seenCalendarEventIDs = useRef<Set<string>>(new Set());
  const toastTimers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());
  const actionOperationKeyRef = useRef("");

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
  const visibleCalendarItems = useMemo(
    () => visibleItemsForView(items, view, anchorDate, salon?.timezone),
    [anchorDate, items, salon?.timezone, view]
  );
  const selectedActionSlot = useMemo(
    () => (availabilityResult?.slots ?? []).find((slot) => slotKey(slot) === actionForm.selectedSlotKey) ?? null,
    [actionForm.selectedSlotKey, availabilityResult]
  );
  const selectedCalendarItem = useMemo(
    () => items.find((item) => item.id === selectedCalendarItemID) ?? defaultSelectedCalendarItem(items),
    [items, selectedCalendarItemID]
  );
  const selectedDayItems = useMemo(
    () =>
      selectedDayKey
        ? items
            .filter((item) => dateKey(item.start, salon?.timezone) === selectedDayKey)
            .sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime())
        : [],
    [items, salon?.timezone, selectedDayKey]
  );

  useEffect(() => {
    if (items.length === 0) {
      if (selectedCalendarItemID) setSelectedCalendarItemID("");
      if (detailOpen) setDetailOpen(false);
      return;
    }
    if (!items.some((item) => item.id === selectedCalendarItemID)) {
      setSelectedCalendarItemID(defaultSelectedCalendarItem(items)?.id ?? items[0].id);
    }
  }, [detailOpen, items, selectedCalendarItemID]);

  useEffect(() => {
    if (!pendingFocusItemID) return;
    if (!items.some((item) => item.id === pendingFocusItemID)) return;
    selectCalendarItem(pendingFocusItemID);
    setPendingFocusItemID("");
  }, [items, pendingFocusItemID]);

  useEffect(() => {
    setDetailOpen(false);
    setDayDrawerOpen(false);
    setSelectedDayKey("");
  }, [anchorDate, view]);

  useEffect(() => {
    if (!salon) return;
    let active = true;
    let reloadTimer: ReturnType<typeof setTimeout> | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    const controller = new AbortController();
    const cursorKey = calendarEventCursorKey(salon.id);

    const scheduleRangeReload = () => {
      if (reloadTimer) clearTimeout(reloadTimer);
      reloadTimer = setTimeout(() => {
        if (!active) return;
        void loadCalendarRange(salon.id, view, anchorDate);
      }, 400);
    };

    const connect = async () => {
      if (!active) return;
      const cursor = window.sessionStorage.getItem(cursorKey);
      const params = new URLSearchParams();
      if (cursor) params.set("cursor", cursor);
      try {
        const response = await apiStream(
          `/api/salons/${salon.id}/calendar/events/stream${params.toString() ? `?${params.toString()}` : ""}`,
          { signal: controller.signal }
        );
        await readCalendarEventStream(response, (event) => {
          if (!active || seenCalendarEventIDs.current.has(event.id)) return;
          seenCalendarEventIDs.current.add(event.id);
          if (event.cursor) window.sessionStorage.setItem(cursorKey, event.cursor);
          pushCalendarToast(event);
          if (calendarEventIntersectsRange(event, rangeForView(view, anchorDate))) {
            scheduleRangeReload();
          }
        });
        if (active) reconnectTimer = setTimeout(connect, 1000);
      } catch {
        if (active && !controller.signal.aborted) reconnectTimer = setTimeout(connect, 3000);
      }
    };

    void connect();

    return () => {
      active = false;
      controller.abort();
      if (reloadTimer) clearTimeout(reloadTimer);
      if (reconnectTimer) clearTimeout(reconnectTimer);
    };
  }, [anchorDate, salon, view]);

  useEffect(() => {
    return () => {
      for (const timer of toastTimers.current.values()) {
        clearTimeout(timer);
      }
      toastTimers.current.clear();
    };
  }, []);

  function selectCalendarItem(itemID: string) {
    setSelectedCalendarItemID(itemID);
    setDayDrawerOpen(false);
    setDetailOpen(true);
  }

  function openDayAppointments(day: string) {
    setSelectedDayKey(day);
    setDetailOpen(false);
    setDayDrawerOpen(true);
  }

  function pushCalendarToast(event: CalendarEvent) {
    const toast = calendarToastFromEvent(event);
    setCalendarToasts((current) => [toast, ...current.filter((item) => item.id !== toast.id)].slice(0, 3));
    const existingTimer = toastTimers.current.get(toast.id);
    if (existingTimer) clearTimeout(existingTimer);
    const timer = setTimeout(() => dismissCalendarToast(toast.id), 8000);
    toastTimers.current.set(toast.id, timer);
  }

  function dismissCalendarToast(id: string) {
    const timer = toastTimers.current.get(id);
    if (timer) clearTimeout(timer);
    toastTimers.current.delete(id);
    setCalendarToasts((current) => current.filter((item) => item.id !== id));
  }

  function focusCalendarEvent(event: CalendarEvent) {
    const itemID = calendarItemIDForEvent(event);
    if (!itemID) return;
    const nextAnchorDate = formatDateInput(new Date(event.start_time), salon?.timezone);
    const currentRange = rangeForView(view, anchorDate);
    if (calendarEventIntersectsRange(event, currentRange) && items.some((item) => item.id === itemID)) {
      selectCalendarItem(itemID);
      return;
    }
    setPendingFocusItemID(itemID);
    setAnchorDate(nextAnchorDate);
    void loadCalendarRange(event.salon_id, view, nextAnchorDate);
  }

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
    actionOperationKeyRef.current = crypto.randomUUID();
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
    actionOperationKeyRef.current = crypto.randomUUID();
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
    actionOperationKeyRef.current = crypto.randomUUID();
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
    actionOperationKeyRef.current = "";
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
          operation_key: currentActionOperationKey(),
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
      if (attempt.status !== "confirmed" || !attempt.pos_booking_id) {
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
            operation_key: currentActionOperationKey(),
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
          body: JSON.stringify({ operation_key: currentActionOperationKey(), reason: actionForm.cancelReason })
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

  function currentActionOperationKey() {
    if (!actionOperationKeyRef.current) {
      actionOperationKeyRef.current = crypto.randomUUID();
    }
    return actionOperationKeyRef.current;
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
      <main className="h-screen overflow-hidden bg-shell p-3 sm:p-4">
        <div className="flex h-full min-h-0 flex-col gap-3">
          <Skeleton className="h-24" />
          <div className="flex min-h-0 flex-1">
            <Skeleton className="min-h-0 flex-1" />
          </div>
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
    <main className="h-screen overflow-hidden bg-shell p-2 text-ink sm:p-3">
      <div className="flex h-full min-h-0 flex-col gap-2">
        <header className="shrink-0 rounded-lg border border-line bg-panel px-3 py-2 shadow-soft">
          <div className="grid gap-2 lg:grid-cols-[auto_1fr_auto] lg:items-center">
            <div className="flex min-w-0 items-center gap-2">
              <CalendarDays className="h-4 w-4 flex-none text-brand" />
              <h1 className="whitespace-nowrap text-base font-bold text-ink">POS Calendar</h1>
            </div>

            <div className="flex min-w-0 flex-wrap items-center justify-center gap-1.5 lg:flex-nowrap">
              <div className="grid grid-cols-4 gap-1 rounded-md border border-line bg-slate-50 p-1">
                {views.map((item) => (
                  <Button
                    key={item}
                    type="button"
                    variant={view === item ? "primary" : "ghost"}
                    className="h-8 px-2 text-xs"
                    onClick={() => setView(item)}
                  >
                    {capitalize(item)}
                  </Button>
                ))}
              </div>

              <div className="flex items-center gap-1 rounded-md border border-line bg-white px-1">
                <Button type="button" variant="ghost" className="h-8 px-2" onClick={() => moveRange(-1)} aria-label="Previous range">
                  <ChevronLeft className="h-3.5 w-3.5" />
                </Button>
                <div className="min-w-40 whitespace-nowrap px-1 text-center text-xs font-bold text-ink">
                  {rangeLabel(view, range, salon.timezone)}
                </div>
                <Button type="button" variant="ghost" className="h-8 px-2" onClick={() => moveRange(1)} aria-label="Next range">
                  <ChevronRight className="h-3.5 w-3.5" />
                </Button>
              </div>

              {view === "day" || view === "agenda" ? (
                <div className="flex items-center gap-1.5">
                  <Button type="button" variant="secondary" className="h-8 px-2 text-xs" onClick={() => setShortcut(0)}>
                    Today
                  </Button>
                  <Button type="button" variant="secondary" className="h-8 px-2 text-xs" onClick={() => setShortcut(1)}>
                    Tomorrow
                  </Button>
                </div>
              ) : null}
            </div>

            <div className="flex flex-wrap items-center justify-start gap-1.5 lg:justify-end">
              <Button type="button" variant="secondary" className="h-8 px-2 text-xs" onClick={syncCalendar} disabled={busy === "sync"}>
                <RefreshCcw className={cn("h-3.5 w-3.5", busy === "sync" ? "animate-spin" : "")} />
                {busy === "sync" ? "Syncing..." : "Sync Square"}
              </Button>
              <Button type="button" className="h-8 px-2 text-xs" onClick={openCreate} disabled={!readyForBooking}>
                <Plus className="h-3.5 w-3.5" />
                Add Appointment
              </Button>
              <Button type="button" variant="ghost" className="h-8 px-2 text-xs" onClick={signOut} disabled={busy === "logout"}>
                <LogOut className="h-3.5 w-3.5" />
                Sign out
              </Button>
              </div>
            </div>
          </header>

          <CalendarToastStack
            toasts={calendarToasts}
            timezone={salon.timezone}
            onView={focusCalendarEvent}
            onDismiss={dismissCalendarToast}
          />

          {error ? <Alert title="Calendar error" message={error} /> : null}
          {notice ? <Alert type={notice.type} title={notice.title} message={notice.message} /> : null}

        <section className="min-h-0 flex-1">
          <Card className="flex h-full min-w-0 flex-col overflow-hidden p-0">
            <div className="flex h-full min-h-0 flex-col p-2 sm:p-3">
              {loadingCalendar ? (
                <div className="flex h-full min-h-0 flex-col gap-3">
                  <Skeleton className="h-12" />
                  <Skeleton className="min-h-0 flex-1" />
                </div>
              ) : (
                  <div className="flex h-full min-h-0 flex-col gap-2">
                    <CalendarViewSummary
                      label={`${capitalize(view)} view`}
                      title={calendarSummaryTitle(view, anchorDate, salon.timezone)}
                      items={visibleCalendarItems}
                      bookableStaffCount={bookableStaff.length}
                      bookableServiceCount={bookableServices.length}
                    />
                    {view === "agenda" && visibleCalendarItems.length === 0 ? (
                      <EmptyState
                        title="No calendar items"
                        message="POS-confirmed appointments and pending booking requests for this range will appear here."
                      />
                  ) : view === "day" ? (
                    <DayScheduler
                      items={items}
                      anchorDate={anchorDate}
                      timezone={salon.timezone}
                      selectedItemID={selectedCalendarItem?.id ?? ""}
                      onSelect={selectCalendarItem}
                    />
                  ) : view === "agenda" ? (
                    <AgendaList
                      items={visibleCalendarItems}
                      timezone={salon.timezone}
                      selectedItemID={selectedCalendarItem?.id ?? ""}
                      onSelect={selectCalendarItem}
                      onEdit={openEdit}
                      onDelete={openDelete}
                    />
                  ) : view === "week" ? (
                    <WeekScheduler
                      items={items}
                      anchorDate={anchorDate}
                      timezone={salon.timezone}
                      selectedItemID={selectedCalendarItem?.id ?? ""}
                      onSelect={selectCalendarItem}
                    />
                  ) : (
                    <MonthGrid
                      items={items}
                      anchorDate={anchorDate}
                      timezone={salon.timezone}
                      selectedItemID={selectedCalendarItem?.id ?? ""}
                      onSelect={selectCalendarItem}
                      onDayOpen={openDayAppointments}
                    />
                  )}
                </div>
              )}
            </div>
          </Card>
        </section>

        <AppointmentDetailDrawer
          open={detailOpen}
          selectedItem={selectedCalendarItem}
          readyForBooking={readyForBooking}
          timezone={salon.timezone}
          onClose={() => setDetailOpen(false)}
          onEdit={openEdit}
          onDelete={openDelete}
        />

        <DayAppointmentsDrawer
          open={dayDrawerOpen}
          day={selectedDayKey}
          items={selectedDayItems}
          timezone={salon.timezone}
          onClose={() => setDayDrawerOpen(false)}
          onSelect={selectCalendarItem}
        />

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

function CalendarToastStack({
  toasts,
  timezone,
  onView,
  onDismiss
}: {
  toasts: CalendarToast[];
  timezone?: string;
  onView: (event: CalendarEvent) => void;
  onDismiss: (id: string) => void;
}) {
  if (toasts.length === 0) return null;
  return (
    <div className="fixed right-4 top-4 z-[70] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={cn(
            "rounded-lg border bg-panel p-3 shadow-2xl ring-1 ring-black/5",
            toast.type === "warning" ? "border-amber-200" : "border-emerald-200"
          )}
        >
          <div className="flex items-start gap-3">
            <div
              className={cn(
                "mt-0.5 h-2.5 w-2.5 flex-none rounded-full",
                toast.type === "warning" ? "bg-amber-400" : "bg-emerald-500"
              )}
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-bold text-ink">{toast.title}</div>
              <div className="mt-1 text-xs font-semibold text-ink">
                {toast.event.customer_name || "New customer"} · {formatShortDateTime(toast.event.start_time, timezone)}
              </div>
              <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted">{toast.message}</div>
              <div className="mt-3 flex items-center gap-2">
                <Button type="button" variant="secondary" className="h-7 px-2 text-xs" onClick={() => onView(toast.event)}>
                  View
                </Button>
                <Button type="button" variant="ghost" className="h-7 px-2 text-xs" onClick={() => onDismiss(toast.id)}>
                  Dismiss
                </Button>
              </div>
            </div>
            <Button type="button" variant="ghost" className="h-7 px-2" onClick={() => onDismiss(toast.id)} aria-label="Dismiss notification">
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}

function AgendaList({
  items,
  timezone,
  selectedItemID,
  onSelect,
  onEdit,
  onDelete
}: {
  items: CalendarItem[];
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  return (
    <div className="min-h-0 flex-1 space-y-3 overflow-y-auto pr-1">
      {items.map((item) => (
        <CalendarItemRow
          key={item.id}
          item={item}
          timezone={timezone}
          selected={item.id === selectedItemID}
          onSelect={onSelect}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </div>
  );
}

function DayScheduler({
  items,
  anchorDate,
  timezone,
  selectedItemID,
  onSelect
}: {
  items: CalendarItem[];
  anchorDate: string;
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
}) {
  const dayItems = items
    .filter((item) => dateKey(item.start, timezone) === anchorDate)
    .sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());
  const hours = schedulerHours(dayItems, timezone);
  const positionedItems = positionSchedulerItems(dayItems, hours, timezone);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-line bg-white shadow-sm">
      <div className="grid shrink-0 grid-cols-[3.5rem_minmax(0,1fr)] border-b border-line bg-slate-50">
        <div className="border-r border-line px-2 py-3 text-xs font-semibold uppercase text-muted">Day</div>
        <div
          className={cn(
            "px-3 py-3 text-center",
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
      <div className="grid shrink-0 grid-cols-[3.5rem_minmax(0,1fr)] border-b border-line">
        <div className="border-r border-line bg-slate-50 px-2 py-2 text-xs font-semibold text-muted">All-day</div>
        <div className="min-h-9 bg-white px-2 py-2" />
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-[3.5rem_minmax(0,1fr)] overflow-y-auto">
        <div>
          {hours.map((hour) => (
            <div
              key={hour}
              className="border-r border-line bg-slate-50 px-2 pt-2 text-right text-[11px] font-semibold text-muted"
              style={{ height: schedulerRowHeight }}
            >
              {hourLabel(hour)}
            </div>
          ))}
        </div>
        <div className="relative min-w-0 bg-white" style={{ height: hours.length * schedulerRowHeight }}>
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
          <div className="absolute inset-x-2 top-0 bottom-0">
            {positionedItems.map((positioned) => (
              <SchedulerEventBlock
                key={positioned.item.id}
                positioned={positioned}
                timezone={timezone}
                selected={positioned.item.id === selectedItemID}
                onSelect={onSelect}
              />
            ))}
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
  selectedItemID,
  onSelect
}: {
  items: CalendarItem[];
  anchorDate: string;
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
}) {
  const weekStart = startOfWeekInput(anchorDate);
  const days = Array.from({ length: 7 }, (_, index) => addDaysInput(weekStart, index));
  const weekItems = items.filter((item) => days.includes(dateKey(item.start, timezone)));
  const hours = schedulerHours(weekItems, timezone);
  const gridHeight = hours.length * schedulerRowHeight;

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-line bg-white shadow-sm">
      <div className="grid shrink-0 grid-cols-[3.5rem_repeat(7,minmax(0,1fr))] border-b border-line bg-slate-50">
        <div className="border-r border-line px-2 py-3 text-xs font-semibold uppercase text-muted">Week</div>
        {days.map((day) => (
          <div
            key={day}
            className={cn(
              "min-w-0 border-r border-line px-2 py-2 text-center last:border-r-0",
              isTodayInput(day, timezone) ? "bg-teal-50 text-brand" : "text-ink"
            )}
          >
            <div className="truncate text-[11px] font-semibold uppercase text-muted">{weekdayLabel(day)}</div>
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
      <div className="grid shrink-0 grid-cols-[3.5rem_repeat(7,minmax(0,1fr))] border-b border-line">
        <div className="border-r border-line bg-slate-50 px-2 py-2 text-[11px] font-semibold text-muted">All-day</div>
        {days.map((day) => (
          <div key={day} className="min-h-9 min-w-0 border-r border-line bg-white px-1 py-2 last:border-r-0" />
        ))}
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-[3.5rem_repeat(7,minmax(0,1fr))] overflow-y-auto">
        <div>
          {hours.map((hour) => (
            <div
              key={hour}
              className="border-r border-line bg-slate-50 px-2 pt-2 text-right text-[11px] font-semibold text-muted"
              style={{ height: schedulerRowHeight }}
            >
              {hourLabel(hour)}
            </div>
          ))}
        </div>
        {days.map((day) => {
          const dayItems = weekItems.filter((item) => dateKey(item.start, timezone) === day);
          const positionedItems = positionSchedulerItems(dayItems, hours, timezone);
          return (
            <div
              key={day}
              className={cn(
                "relative min-w-0 border-r border-line bg-white last:border-r-0",
                isTodayInput(day, timezone) ? "bg-teal-50/25" : ""
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
              <div className="absolute inset-x-1 top-0 bottom-0">
                {positionedItems.map((positioned) => (
                  <SchedulerEventBlock
                    key={positioned.item.id}
                    positioned={positioned}
                    timezone={timezone}
                    compact
                    selected={positioned.item.id === selectedItemID}
                    onSelect={onSelect}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function MonthGrid({
  items,
  anchorDate,
  timezone,
  selectedItemID,
  onSelect,
  onDayOpen
}: {
  items: CalendarItem[];
  anchorDate: string;
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
  onDayOpen: (day: string) => void;
}) {
  const days = monthGridDays(anchorDate);
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-line bg-white shadow-sm">
      <div className="grid shrink-0 grid-cols-7 border-b border-line bg-slate-50 text-[11px] font-semibold uppercase text-muted">
        {weekdayLabels.map((label) => (
          <div key={label} className="min-w-0 border-r border-line px-2 py-2 last:border-r-0">
            {label}
          </div>
        ))}
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-7 grid-rows-6">
        {days.map((day) => {
          const dayItems = items
            .filter((item) => dateKey(item.start, timezone) === day)
            .sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());
          const muted = day.slice(0, 7) !== anchorDate.slice(0, 7);
          const warnings = warningCount(dayItems);
          const pending = dayItems.filter((item) => item.kind === "pending").length;
          const visibleItems = dayItems.slice(0, 3);
          return (
            <div
              key={day}
              onClick={() => onDayOpen(day)}
              className={cn(
                "flex min-h-0 min-w-0 cursor-pointer flex-col overflow-hidden border-r border-b border-line bg-white p-2 transition hover:bg-slate-50 last:border-r-0",
                muted ? "bg-slate-50 text-muted" : "",
                isTodayInput(day, timezone) ? "relative z-10 ring-2 ring-brand ring-inset" : ""
              )}
            >
              <div className="mb-2 flex items-start justify-between gap-1">
                <button
                  type="button"
                  className={cn(
                    "flex h-7 min-w-7 items-center justify-center rounded-full px-2 text-sm font-bold outline-none focus:ring-2 focus:ring-brand",
                    isTodayInput(day, timezone) ? "bg-brand text-white" : muted ? "text-slate-400" : "text-ink"
                  )}
                  onClick={(event) => {
                    event.stopPropagation();
                    onDayOpen(day);
                  }}
                  aria-label={`Open appointments for ${formatFullInputDateLabel(day)}`}
                >
                  {monthCellDayLabel(day)}
                </button>
                {warnings > 0 ? <AlertTriangle className="h-4 w-4 flex-none text-amber-600" /> : null}
              </div>
              {dayItems.length > 1 ? (
                <button
                  type="button"
                  className="mb-1 flex min-w-0 flex-wrap items-center gap-1 text-left text-[10px] font-semibold text-muted outline-none focus:ring-2 focus:ring-brand"
                  onClick={(event) => {
                    event.stopPropagation();
                    onDayOpen(day);
                  }}
                >
                  <span>{dayItems.length} appts</span>
                  {pending > 0 ? <span>{pending} pending</span> : null}
                  {warnings > 0 ? <span>{warnings} warn</span> : null}
                </button>
              ) : null}
              <div className="min-h-0 space-y-1 overflow-hidden">
                {visibleItems.map((item, index) => (
                  <MonthEventChip
                    key={item.id}
                    item={item}
                    timezone={timezone}
                    selected={item.id === selectedItemID}
                    className={index > 1 ? "hidden xl:flex" : ""}
                    onSelect={onSelect}
                  />
                ))}
                {dayItems.length > 2 ? (
                  <button
                    type="button"
                    className="block w-full truncate rounded-md bg-slate-100 px-2 py-1 text-left text-[10px] font-semibold text-muted outline-none hover:bg-slate-200 focus:ring-2 focus:ring-brand xl:hidden"
                    onClick={(event) => {
                      event.stopPropagation();
                      onDayOpen(day);
                    }}
                  >
                    +{dayItems.length - 2} more
                  </button>
                ) : null}
                {dayItems.length > 3 ? (
                  <button
                    type="button"
                    className="hidden w-full truncate rounded-md bg-slate-100 px-2 py-1 text-left text-[10px] font-semibold text-muted outline-none hover:bg-slate-200 focus:ring-2 focus:ring-brand xl:block"
                    onClick={(event) => {
                      event.stopPropagation();
                      onDayOpen(day);
                    }}
                  >
                    +{dayItems.length - 3} more
                  </button>
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function CalendarViewSummary({
  label,
  title,
  items,
  bookableStaffCount,
  bookableServiceCount
}: {
  label: string;
  title: string;
  items: CalendarItem[];
  bookableStaffCount: number;
  bookableServiceCount: number;
}) {
  const appointments = items.filter((item) => item.kind === "appointment").length;
  const pending = items.filter((item) => item.kind === "pending").length;
  const warnings = warningCount(items);
  return (
    <div className="shrink-0 rounded-md border border-line bg-slate-50 px-3 py-2">
      <div className="text-[10px] font-bold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 grid gap-1.5 lg:grid-cols-[auto_1fr_auto] lg:items-center">
        <div className="min-w-0 flex-none whitespace-nowrap text-sm font-bold text-ink xl:text-base">{title}</div>
        <div className="flex min-w-0 flex-wrap justify-center gap-1 lg:flex-nowrap lg:justify-self-center">
          <SummaryStatCard label="Appointments" value={String(appointments)} />
          <SummaryStatCard label="Pending" value={String(pending)} tone={pending > 0 ? "warning" : "normal"} />
          <SummaryStatCard label="Warnings" value={String(warnings)} tone={warnings > 0 ? "warning" : "normal"} />
          <SummaryStatCard label="Staff" value={String(bookableStaffCount)} />
          <SummaryStatCard label="Services" value={String(bookableServiceCount)} />
        </div>
        <div className="flex-none lg:justify-self-end">
          <CalendarLegend />
        </div>
      </div>
    </div>
  );
}

function SummaryStatCard({
  label,
  value,
  tone = "normal"
}: {
  label: string;
  value: string;
  tone?: "normal" | "warning";
}) {
  return (
    <div className={cn("w-20 flex-none rounded-md border px-1.5 py-1 lg:w-24 xl:w-28 xl:px-2 xl:py-1.5", tone === "warning" ? "border-amber-200 bg-amber-50" : "border-line bg-white")}>
      <div className="truncate text-[9px] font-semibold uppercase tracking-wide text-muted xl:text-[10px]">{label}</div>
      <div className="text-xs font-bold text-ink xl:mt-0.5 xl:text-sm">{value}</div>
    </div>
  );
}

function CalendarLegend() {
  return (
    <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold text-muted xl:gap-3 xl:text-xs">
      <span className="inline-flex items-center gap-1.5">
        <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 xl:h-2 xl:w-2" />
        confirmed
      </span>
      <span className="inline-flex items-center gap-1.5">
        <span className="h-1.5 w-1.5 rounded-full bg-amber-500 xl:h-2 xl:w-2" />
        pending
      </span>
      <span className="inline-flex items-center gap-1.5">
        <span className="h-1.5 w-1.5 rounded-full bg-red-500 xl:h-2 xl:w-2" />
        warning
      </span>
    </div>
  );
}

function SchedulerEventBlock({
  positioned,
  timezone,
  compact = false,
  selected,
  onSelect
}: {
  positioned: PositionedCalendarItem;
  timezone?: string;
  compact?: boolean;
  selected: boolean;
  onSelect: (itemID: string) => void;
}) {
  const item = positioned.item;
  const customerLabel = item.customerName || "Unknown";
  const serviceLabel = compactServiceLabel(item);
  const primaryLine = `${formatTime(item.start, timezone)} · ${customerLabel} - ${serviceLabel}`;
  const showStatusBadge = compact || positioned.height >= 54;

  return (
    <div
      role="button"
      tabIndex={0}
      className={cn(
        "absolute cursor-pointer overflow-hidden rounded-md border p-2 text-left shadow-sm outline-none transition hover:shadow-md focus:ring-2 focus:ring-brand",
        item.warning ? "border-amber-300 bg-amber-50" : item.kind === "pending" ? "border-amber-200 bg-amber-50" : "border-emerald-200 bg-emerald-50",
        selected ? "ring-2 ring-brand ring-offset-1" : ""
      )}
      style={{
        top: positioned.top,
        height: positioned.height,
        left: `calc(${(positioned.lane / positioned.laneCount) * 100}% + 2px)`,
        width: `calc(${100 / positioned.laneCount}% - 4px)`
      }}
      onClick={() => onSelect(item.id)}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect(item.id);
        }
      }}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          {compact ? (
            <>
              <div className="truncate text-xs font-bold text-ink">
                {formatTime(item.start, timezone)} · {customerLabel}
              </div>
              <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted">{serviceLabel}</div>
            </>
          ) : (
            <div className="truncate text-xs font-bold text-ink">{primaryLine}</div>
          )}
        </div>
        {item.warning ? <AlertTriangle className="h-4 w-4 flex-none text-amber-600" /> : null}
      </div>
      {showStatusBadge ? (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          <Badge value={item.status} className="px-2 py-0.5" />
        </div>
      ) : null}
    </div>
  );
}

function MonthEventChip({
  item,
  timezone,
  selected,
  className,
  onSelect
}: {
  item: CalendarItem;
  timezone?: string;
  selected: boolean;
  className?: string;
  onSelect: (itemID: string) => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "group flex w-full items-start gap-1.5 rounded-md border px-1.5 py-1 text-left text-[10px] outline-none transition hover:shadow-sm focus:ring-2 focus:ring-brand",
        item.warning ? "border-amber-300 bg-amber-50" : item.kind === "pending" ? "border-amber-200 bg-amber-50" : "border-emerald-200 bg-emerald-50",
        selected ? "ring-2 ring-brand ring-offset-1" : "",
        className
      )}
      onClick={(event) => {
        event.stopPropagation();
        onSelect(item.id);
      }}
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate font-bold text-ink">
          {formatTime(item.start, timezone)} · {item.customerName || "Unknown"}
        </span>
        <span className="block truncate leading-4 text-muted">{compactServiceLabel(item)}</span>
      </span>
      <span className="flex flex-none items-center gap-1">
        {item.kind === "pending" ? <span className="rounded bg-amber-100 px-1 text-[9px] font-semibold text-amber-700">pending</span> : null}
        {item.warning ? <AlertTriangle className="mt-0.5 h-3 w-3 flex-none text-amber-600" /> : null}
      </span>
    </button>
  );
}

function DayAppointmentsDrawer({
  open,
  day,
  items,
  timezone,
  onClose,
  onSelect
}: {
  open: boolean;
  day: string;
  items: CalendarItem[];
  timezone?: string;
  onClose: () => void;
  onSelect: (itemID: string) => void;
}) {
  const pending = items.filter((item) => item.kind === "pending").length;
  const warnings = warningCount(items);
  const visible = open && Boolean(day);

  return (
    <div className={cn("fixed inset-0 z-50 transition", visible ? "pointer-events-auto" : "pointer-events-none")}>
      <button
        type="button"
        aria-label="Close day appointments"
        className={cn("absolute inset-0 bg-slate-950/20 transition-opacity", visible ? "opacity-100" : "opacity-0")}
        onClick={onClose}
      />
      <aside
        className={cn(
          "absolute right-0 top-0 flex h-full w-full max-w-md flex-col border-l border-line bg-panel shadow-2xl transition-transform duration-200",
          visible ? "translate-x-0" : "translate-x-full"
        )}
      >
        <div className="border-b border-line px-4 py-3">
          <div className="flex items-start justify-between gap-3">
            <div>
              <CardTitle>Day appointments</CardTitle>
              <CardDescription>{day ? formatFullInputDateLabel(day) : "Selected day"}</CardDescription>
            </div>
            <Button type="button" variant="ghost" className="h-9 px-2" onClick={onClose} aria-label="Close day appointments">
              <X className="h-4 w-4" />
            </Button>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs font-semibold text-muted">
            <span>{items.length} appointments</span>
            {pending > 0 ? <span>{pending} pending</span> : null}
            {warnings > 0 ? <span>{warnings} warnings</span> : null}
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
          {items.length === 0 ? (
            <div className="rounded-md border border-dashed border-line bg-slate-50 p-5 text-center text-sm text-muted">
              No appointments scheduled for this day.
            </div>
          ) : (
            <div className="space-y-2">
              {items.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className={cn(
                    "w-full rounded-md border p-3 text-left outline-none transition hover:shadow-sm focus:ring-2 focus:ring-brand",
                    item.warning
                      ? "border-amber-300 bg-amber-50"
                      : item.kind === "pending"
                        ? "border-amber-200 bg-amber-50"
                        : "border-line bg-white"
                  )}
                  onClick={() => onSelect(item.id)}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate text-sm font-bold text-ink">
                        {formatTimeRange(item.start, item.end, timezone)} · {item.customerName || "Unknown"}
                      </div>
                      <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted">{compactServiceLabel(item)}</div>
                    </div>
                    {item.warning ? <AlertTriangle className="h-4 w-4 flex-none text-amber-600" /> : null}
                  </div>
                  <div className="mt-2 flex flex-wrap gap-1.5">
                    <Badge value={item.status} />
                    {item.warning ? <Badge value="warning" /> : null}
                  </div>
                </button>
              ))}
            </div>
          )}
        </div>
      </aside>
    </div>
  );
}

function AppointmentDetailDrawer({
  open,
  selectedItem,
  readyForBooking,
  timezone,
  onClose,
  onEdit,
  onDelete
}: {
  open: boolean;
  selectedItem: CalendarItem | null;
  readyForBooking: boolean;
  timezone?: string;
  onClose: () => void;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const appointment = selectedItem?.appointment;

  return (
    <div className={cn("fixed inset-0 z-50 transition", open && selectedItem ? "pointer-events-auto" : "pointer-events-none")}>
      <button
        type="button"
        aria-label="Close appointment detail"
        className={cn("absolute inset-0 bg-slate-950/20 transition-opacity", open && selectedItem ? "opacity-100" : "opacity-0")}
        onClick={onClose}
      />
      <aside
        className={cn(
          "absolute right-0 top-0 flex h-full w-full max-w-md flex-col border-l border-line bg-panel shadow-2xl transition-transform duration-200",
          open && selectedItem ? "translate-x-0" : "translate-x-full"
        )}
      >
        <div className="border-b border-line px-4 py-3">
          <div className="flex items-start justify-between gap-3">
            <div>
              <CardTitle>Appointment Detail</CardTitle>
              <CardDescription>Selected booking and sync health.</CardDescription>
            </div>
            <Button type="button" variant="ghost" className="h-9 px-2" onClick={onClose} aria-label="Close appointment detail">
              <X className="h-4 w-4" />
            </Button>
          </div>
          {!readyForBooking ? (
            <div className="mt-3 rounded-md border border-line bg-slate-50 p-3 text-xs leading-5 text-muted">
              Booking disabled: connect Square Appointments and sync bookable staff/services.
            </div>
          ) : null}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-3">
          {selectedItem ? (
            <div className="rounded-md border border-line bg-white p-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge value={selectedItem.status} />
                {selectedItem.warning ? <Badge value="warning" /> : null}
              </div>
              <div className="mt-3 text-lg font-bold text-ink">{selectedItem.customerName || "Unknown customer"}</div>
              <div className="mt-1 text-sm font-semibold text-ink">{formatTimeRange(selectedItem.start, selectedItem.end, timezone)}</div>
              <div className="mt-2 text-sm leading-6 text-muted">{compactServiceLabel(selectedItem)}</div>
              <div className="mt-1 text-xs leading-5 text-muted">{selectedItem.detail}</div>
              {selectedItem.warning ? (
                <div className="mt-3 flex gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-900">
                  <AlertTriangle className="mt-0.5 h-4 w-4 flex-none" />
                  <span>{selectedItem.warning}</span>
                </div>
              ) : null}
              {!appointment ? (
                <div className="mt-3 rounded-md border border-line bg-slate-50 p-3 text-xs leading-5 text-muted">
                  Needs owner review. This request is not a confirmed POS appointment, so edit and delete actions are disabled.
                </div>
              ) : null}
              <div className="mt-4 grid grid-cols-2 gap-2">
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => {
                    if (!appointment) return;
                    onEdit(appointment);
                    onClose();
                  }}
                  disabled={!appointment || !canEditAppointment(appointment)}
                >
                  <Pencil className="h-4 w-4" />
                  Edit
                </Button>
                <Button
                  type="button"
                  variant="danger"
                  onClick={() => {
                    if (!appointment) return;
                    onDelete(appointment);
                    onClose();
                  }}
                  disabled={!appointment || !canDeleteAppointment(appointment)}
                >
                  <Trash2 className="h-4 w-4" />
                  Delete
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      </aside>
    </div>
  );
}

function CalendarItemRow({
  item,
  timezone,
  selected,
  onSelect,
  onEdit,
  onDelete
}: {
  item: CalendarItem;
  timezone?: string;
  selected: boolean;
  onSelect: (itemID: string) => void;
  onEdit: (appointment: AppointmentRecord) => void;
  onDelete: (appointment: AppointmentRecord) => void;
}) {
  const appointment = item.appointment;
  return (
    <div
      role="button"
      tabIndex={0}
      className={cn(
        "grid cursor-pointer gap-3 rounded-md border bg-white p-4 outline-none transition hover:border-brand/60 md:grid-cols-[11rem_1fr_auto] md:items-start",
        selected ? "border-brand ring-2 ring-teal-100" : "border-line"
      )}
      onClick={() => onSelect(item.id)}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect(item.id);
        }
      }}
    >
      <div>
        <div className="text-sm font-semibold text-ink">{formatTimeRange(item.start, item.end, timezone)}</div>
        <div className="mt-1 text-xs text-muted">{formatDate(item.start, timezone)}</div>
      </div>
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <div className="text-sm font-semibold text-ink">{item.customerName || "Unknown customer"}</div>
            <Badge value={item.status} />
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
            <Button
              type="button"
              variant="secondary"
              className="h-9 px-3"
              onClick={(event) => {
                event.stopPropagation();
                onEdit(appointment);
              }}
              disabled={!canEditAppointment(appointment)}
            >
              <Pencil className="h-4 w-4" />
              Edit
            </Button>
            <Button
              type="button"
              variant="danger"
              className="h-9 px-3"
              onClick={(event) => {
                event.stopPropagation();
                onDelete(appointment);
              }}
              disabled={!canDeleteAppointment(appointment)}
            >
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
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center rounded-md border border-dashed border-line bg-slate-50 p-6 text-center">
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

function defaultSelectedCalendarItem(items: CalendarItem[]) {
  return prioritizedCalendarItems(items)[0] ?? null;
}

function prioritizedCalendarItems(items: CalendarItem[]) {
  return [...items].sort((a, b) => {
    const aReview = a.warning || a.kind === "pending" ? 0 : 1;
    const bReview = b.warning || b.kind === "pending" ? 0 : 1;
    if (aReview !== bReview) return aReview - bReview;
    return new Date(a.start).getTime() - new Date(b.start).getTime();
  });
}

function calendarEventCursorKey(salonID: string) {
  return `pos-calendar-event-cursor:${salonID}`;
}

async function readCalendarEventStream(response: Response, onEvent: (event: CalendarEvent) => void) {
  if (!response.body) return;
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const frames = buffer.split("\n\n");
    buffer = frames.pop() ?? "";
    frames.forEach((frame) => {
      const event = parseCalendarEventFrame(frame);
      if (event) onEvent(event);
    });
  }
}

function parseCalendarEventFrame(frame: string): CalendarEvent | null {
  const data = frame
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trimStart())
    .join("\n");
  if (!data) return null;
  try {
    const event = JSON.parse(data) as CalendarEvent;
    return event.id && event.type ? event : null;
  } catch {
    return null;
  }
}

function calendarToastFromEvent(event: CalendarEvent): CalendarToast {
  const confirmed = event.type === "booking_confirmed" || event.booking_status === "confirmed";
  return {
    id: event.id,
    type: confirmed ? "success" : "warning",
    title: confirmed ? "New POS-confirmed appointment" : "New pending booking request",
    message: confirmed
      ? "The active POS returned a booking ID before the calendar updated."
      : "Square Appointments has not confirmed this booking yet.",
    event
  };
}

function calendarItemIDForEvent(event: CalendarEvent) {
  if (event.appointment_id) return `appointment-${event.appointment_id}`;
  if (event.booking_attempt_id) return `pending-${event.booking_attempt_id}`;
  return "";
}

function calendarEventIntersectsRange(event: CalendarEvent, range: { start: string; end: string }) {
  if (!event.start_time || !event.end_time) return false;
  const start = new Date(event.start_time).getTime();
  const end = new Date(event.end_time).getTime();
  const rangeStart = inputDateToLocalDate(range.start).getTime();
  const rangeEnd = inputDateToLocalDate(range.end).getTime();
  return start < rangeEnd && end > rangeStart;
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

function visibleItemsForView(items: CalendarItem[], view: CalendarView, anchorDate: string, timezone?: string) {
  if (view === "day") {
    return items.filter((item) => dateKey(item.start, timezone) === anchorDate);
  }
  if (view === "week") {
    const start = startOfWeekInput(anchorDate);
    const days = new Set(Array.from({ length: 7 }, (_, index) => addDaysInput(start, index)));
    return items.filter((item) => days.has(dateKey(item.start, timezone)));
  }
  if (view === "month") {
    return items.filter((item) => dateKey(item.start, timezone).slice(0, 7) === anchorDate.slice(0, 7));
  }
  return items;
}

function calendarSummaryTitle(view: CalendarView, anchorDate: string, timezone?: string) {
  if (view === "day") {
    return formatFullInputDateLabel(anchorDate);
  }
  if (view === "month") {
    return monthTitle(anchorDate);
  }
  return rangeLabel(view, rangeForView(view, anchorDate), timezone);
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

function formatShortDateTime(value: string, timezone?: string) {
  if (!value) return "Not scheduled";
  return new Date(value).toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
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

function capitalize(value: string) {
  return value.charAt(0).toUpperCase() + value.slice(1);
}
