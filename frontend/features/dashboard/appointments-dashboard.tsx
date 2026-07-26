"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import {
  AlertTriangle,
  CalendarClock,
  CalendarPlus,
  CalendarSearch,
  ChevronLeft,
  ChevronRight,
  ClipboardList,
  Pencil,
  RefreshCcw
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
  assignedTechniciansLabel,
  bookingSummaryLabel,
  orderedSegments,
  serviceNamesLabel,
  technicianPreferenceLabel,
  technicianPreferenceValue
} from "@/features/dashboard/booking-display";
import { OwnerReviewRequests } from "@/features/dashboard/owner-review-requests";
import { OwnerNotificationDeliveries } from "@/features/dashboard/owner-notification-deliveries";
import { CustomerNotificationStatus } from "@/features/dashboard/customer-notification-status";
import { SchedulingReadinessCard } from "@/features/dashboard/scheduling-readiness-card";
import { InternalAppointmentCreate } from "@/features/dashboard/internal-appointment-create";
import { InternalAppointmentLifecycle } from "@/features/dashboard/internal-appointment-lifecycle";
import { apiRequest } from "@/lib/api/client";
import { getManleAICalendar } from "@/lib/api/internal-calendar";
import { hasCompleteInternalLifecyclePlan } from "@/lib/api/scheduling-actions";
import {
  hasExternalAppointmentConfirmation,
  hasExternalAttemptConfirmation
} from "@/lib/api/scheduling-evidence";
import type {
  AvailabilityResult,
  AvailabilitySlot,
  AppointmentRecord,
  BookingAttempt,
  BookingReconciliationCandidate,
  BookingReconciliationTask,
  BookingSegmentRequest,
  ManleAICalendarAggregate,
  POSConnection,
  POSService,
  POSStaffMember,
  Salon,
  SchedulingAuthority,
  StaffSelectionMode,
  SquareReadiness,
  SyncLog
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection | null;
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
  limit?: number;
  offset?: number;
  has_more?: boolean;
  status?: string;
};

type ReconciliationTasksResponse = {
  tasks: BookingReconciliationTask[];
  limit?: number;
  offset?: number;
  has_more?: boolean;
};

type ReconciliationCandidatesResponse = {
  candidates: BookingReconciliationCandidate[];
};

type ReconciliationQueueStatus = "open" | "escalated";

type ReconciliationPageState = Record<
  ReconciliationQueueStatus,
  {
    offset: number;
    hasMore: boolean;
  }
>;

type ServicesResponse = {
  services: POSService[];
};

type StaffResponse = {
  staff: POSStaffMember[];
};

type AppointmentActionMode = "create" | "reschedule" | "cancel";

type InternalLifecycleState = {
  mode: "reschedule" | "cancel";
  appointment: AppointmentRecord;
};

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
  preservedSegments: BookingSegmentRequest[];
  retryOfAttemptID: string;
  retryRequestedStartTime: string;
  retryRequestedEndTime: string;
};

type ActionNotice = {
  tone: "success" | "warning";
  title: string;
  message: string;
};

const defaultAppointmentPageSize = 10;
const appointmentPageSizeOptions = [10, 25, 50] as const;
const appointmentOverviewLimit = 200;
const defaultFallbackPageSize = 10;
const fallbackOverviewLimit = 200;
const reconciliationPageSize = 25;
const reconciliationQueueStatuses: ReconciliationQueueStatus[] = ["open", "escalated"];

export function AppointmentsDashboard() {
  const actionOperationKeyRef = useRef<{ key: string; fingerprint: string } | null>(null);
  const reconciliationActionRef = useRef<{ key: string; fingerprint: string } | null>(null);
  const reconciliationCandidateRequestIDRef = useRef(0);
  const reconciliationListRequestIDRef = useRef(0);
  const reconciliationSalonIDRef = useRef("");
  const salonDateContextRef = useRef("");
  const availabilityRequestIDRef = useRef(0);
  const actionAvailabilityRequestIDRef = useRef(0);
  const availabilityExpiryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const actionAvailabilityExpiryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [squareStatusError, setSquareStatusError] = useState("");
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [calendarLoading, setCalendarLoading] = useState(true);
  const [calendarError, setCalendarError] = useState("");
  const [appointments, setAppointments] = useState<AppointmentRecord[]>([]);
  const [appointmentRows, setAppointmentRows] = useState<AppointmentRecord[]>([]);
  const [appointmentLimit, setAppointmentLimit] = useState(defaultAppointmentPageSize);
  const [appointmentOffset, setAppointmentOffset] = useState(0);
  const [appointmentHasMore, setAppointmentHasMore] = useState(false);
  const [appointmentOverviewHasMore, setAppointmentOverviewHasMore] = useState(false);
  const [fallbackRequests, setFallbackRequests] = useState<BookingAttempt[]>([]);
  const [fallbackRows, setFallbackRows] = useState<BookingAttempt[]>([]);
  const [fallbackLimit, setFallbackLimit] = useState(defaultFallbackPageSize);
  const [fallbackOffset, setFallbackOffset] = useState(0);
  const [fallbackHasMore, setFallbackHasMore] = useState(false);
  const [fallbackOverviewHasMore, setFallbackOverviewHasMore] = useState(false);
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
  const [internalCreateOpen, setInternalCreateOpen] = useState(false);
  const [internalCreateBusy, setInternalCreateBusy] = useState(false);
  const [internalLifecycle, setInternalLifecycle] = useState<InternalLifecycleState | null>(null);
  const [internalLifecycleBusy, setInternalLifecycleBusy] = useState(false);
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
  const [fallbackListLoading, setFallbackListLoading] = useState(false);
  const [fallbackReviewRequest, setFallbackReviewRequest] = useState<BookingAttempt | null>(null);
  const [appointmentReviewAppointment, setAppointmentReviewAppointment] = useState<AppointmentRecord | null>(null);
  const [reconciliationTasks, setReconciliationTasks] = useState<BookingReconciliationTask[]>([]);
  const [reconciliationLoading, setReconciliationLoading] = useState(false);
  const [reconciliationListError, setReconciliationListError] = useState("");
  const [reconciliationPageState, setReconciliationPageState] = useState<ReconciliationPageState>(() =>
    emptyReconciliationPageState()
  );
  const [reconciliationTask, setReconciliationTask] = useState<BookingReconciliationTask | null>(null);
  const [reconciliationCandidates, setReconciliationCandidates] = useState<BookingReconciliationCandidate[]>([]);
  const [reconciliationCandidatesLoading, setReconciliationCandidatesLoading] = useState(false);
  const [reconciliationCandidateID, setReconciliationCandidateID] = useState("");
  const [reconciliationNote, setReconciliationNote] = useState("");
  const [reconciliationNotCreatedConfirmed, setReconciliationNotCreatedConfirmed] = useState(false);
  const [reconciliationSaving, setReconciliationSaving] = useState(false);
  const [reconciliationError, setReconciliationError] = useState("");

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

  function fallbackListPath(salonID: string, limit: number, offset: number) {
    const params = new URLSearchParams({
      status: "fallback_pending",
      limit: String(limit),
      offset: String(offset)
    });
    return `/api/salons/${salonID}/booking-attempts?${params.toString()}`;
  }

  async function fetchFallbackPage(salonID: string, limit: number, offset: number) {
    return apiRequest<AttemptsResponse>(fallbackListPath(salonID, limit, offset));
  }

  function reconciliationListPath(
    salonID: string,
    status: ReconciliationQueueStatus,
    limit: number,
    offset: number
  ) {
    const params = new URLSearchParams({
      status,
      limit: String(limit),
      offset: String(offset)
    });
    return `/api/salons/${salonID}/booking-reconciliations?${params.toString()}`;
  }

  async function fetchReconciliationPage(
    salonID: string,
    status: ReconciliationQueueStatus,
    limit: number,
    offset: number
  ) {
    return apiRequest<ReconciliationTasksResponse>(reconciliationListPath(salonID, status, limit, offset));
  }

  async function fetchFallbackPageWithFallback(salonID: string, limit: number, offset: number) {
    let response = await fetchFallbackPage(salonID, limit, offset);
    if (response.booking_attempts.length === 0 && offset > 0) {
      const previousOffset = Math.max(0, offset - limit);
      response = await fetchFallbackPage(salonID, limit, previousOffset);
    }
    return response;
  }

  function applyFallbackPage(response: AttemptsResponse, requestedLimit: number, requestedOffset: number) {
    setFallbackRows(response.booking_attempts);
    setFallbackLimit(response.limit ?? requestedLimit);
    setFallbackOffset(response.offset ?? requestedOffset);
    setFallbackHasMore(Boolean(response.has_more));
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

  async function reloadFallbackRows(salonID: string, offset = fallbackOffset, limit = fallbackLimit) {
    setFallbackListLoading(true);
    try {
      const response = await fetchFallbackPageWithFallback(salonID, limit, offset);
      applyFallbackPage(response, limit, offset);
    } finally {
      setFallbackListLoading(false);
    }
  }

  async function reloadReconciliationTasks(salonID: string) {
    const requestID = ++reconciliationListRequestIDRef.current;
    if (reconciliationSalonIDRef.current !== salonID) {
      reconciliationSalonIDRef.current = salonID;
      setReconciliationTasks([]);
      setReconciliationPageState(emptyReconciliationPageState());
    }
    setReconciliationLoading(true);
    setReconciliationListError("");
    try {
      const pages = await Promise.all(
        reconciliationQueueStatuses.map(async (status) => ({
          status,
          response: await fetchReconciliationPage(salonID, status, reconciliationPageSize, 0)
        }))
      );
      if (requestID !== reconciliationListRequestIDRef.current) return;

      const nextPageState = emptyReconciliationPageState();
      for (const page of pages) {
        nextPageState[page.status] = nextReconciliationPageState(page.response, 0, page.status);
      }
      setReconciliationTasks(mergeReconciliationTasks(...pages.map((page) => page.response.tasks)));
      setReconciliationPageState(nextPageState);
    } catch (err) {
      if (requestID !== reconciliationListRequestIDRef.current) return;
      setReconciliationListError(
        err instanceof Error ? err.message : "Could not load provider reconciliation tasks."
      );
    } finally {
      if (requestID === reconciliationListRequestIDRef.current) {
        setReconciliationLoading(false);
      }
    }
  }

  async function loadMoreReconciliationTasks() {
    if (!salon || reconciliationLoading) return;
    const statuses = reconciliationQueueStatuses.filter((status) => reconciliationPageState[status].hasMore);
    if (statuses.length === 0) return;

    const requestID = ++reconciliationListRequestIDRef.current;
    setReconciliationLoading(true);
    setReconciliationListError("");
    try {
      const pages = await Promise.all(
        statuses.map(async (status) => {
          const requestedOffset = reconciliationPageState[status].offset;
          return {
            status,
            requestedOffset,
            response: await fetchReconciliationPage(salon.id, status, reconciliationPageSize, requestedOffset)
          };
        })
      );
      if (requestID !== reconciliationListRequestIDRef.current) return;

      const pageUpdates = pages.map((page) => ({
        status: page.status,
        state: nextReconciliationPageState(page.response, page.requestedOffset, page.status)
      }));
      setReconciliationTasks((current) =>
        mergeReconciliationTasks(current, ...pages.map((page) => page.response.tasks))
      );
      setReconciliationPageState((current) => {
        const next = cloneReconciliationPageState(current);
        for (const update of pageUpdates) {
          next[update.status] = update.state;
        }
        return next;
      });
    } catch (err) {
      if (requestID !== reconciliationListRequestIDRef.current) return;
      setReconciliationListError(
        err instanceof Error ? err.message : "Could not load more provider reconciliation tasks."
      );
    } finally {
      if (requestID === reconciliationListRequestIDRef.current) {
        setReconciliationLoading(false);
      }
    }
  }

  async function load({
    silent = false,
    offset = appointmentOffset,
    limit = appointmentLimit,
    fallbackPageOffset = fallbackOffset,
    fallbackPageLimit = fallbackLimit
  }: { silent?: boolean; offset?: number; limit?: number; fallbackPageOffset?: number; fallbackPageLimit?: number } = {}) {
    setError("");
    if (!silent) {
      setLoading(true);
    } else {
      setAppointmentListLoading(true);
      setFallbackListLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        reconciliationListRequestIDRef.current += 1;
        reconciliationSalonIDRef.current = "";
        salonDateContextRef.current = "";
        setStatus(null);
        setSquareStatusError("");
        setCalendar(null);
        setCalendarError("");
        setCalendarLoading(false);
        setAppointments([]);
        setAppointmentRows([]);
        setAppointmentHasMore(false);
        setAppointmentOverviewHasMore(false);
        setFallbackRequests([]);
        setFallbackRows([]);
        setFallbackHasMore(false);
        setFallbackOverviewHasMore(false);
        setReconciliationTasks([]);
        setReconciliationPageState(emptyReconciliationPageState());
        setReconciliationListError("");
        setReconciliationLoading(false);
        setServices([]);
        setStaff([]);
        return;
      }

      const dateContext = `${firstSalon.id}:${firstSalon.timezone}`;
      if (salonDateContextRef.current !== dateContext) {
        salonDateContextRef.current = dateContext;
        setSelectedDate(formatDateInput(new Date(), firstSalon.timezone));
      }

      // Keep queue failures inside the reconciliation card instead of rejecting the core dashboard load.
      void reloadReconciliationTasks(firstSalon.id);
      setCalendarLoading(true);
      const [statusResult, appointmentResponse, fallbackResponse, serviceResponse, staffResponse, calendarResult] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`)
          .then((value) => ({ value, error: "" }))
          .catch((statusError: unknown) => ({ value: null, error: errorMessage(statusError, "Could not load Square Appointments status.") })),
        fetchAppointmentPageWithFallback(firstSalon.id, limit, offset),
        fetchFallbackPageWithFallback(firstSalon.id, fallbackPageLimit, fallbackPageOffset),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`),
        getManleAICalendar(firstSalon.id)
          .then((response) => ({ value: response.manleai_calendar, error: "" }))
          .catch((calendarFailure: unknown) => ({ value: null, error: errorMessage(calendarFailure, "Could not load scheduling readiness.") }))
      ]);
      const [overviewResponse, fallbackOverviewResponse] = await Promise.all([
        fetchAppointmentPage(firstSalon.id, appointmentOverviewLimit, 0),
        fetchFallbackPage(firstSalon.id, fallbackOverviewLimit, 0)
      ]);
      setStatus(statusResult.value);
      setSquareStatusError(statusResult.error);
      setCalendar(calendarResult.value);
      setCalendarError(calendarResult.error);
      setCalendarLoading(false);
      applyAppointmentPage(appointmentResponse, limit, offset);
      applyFallbackPage(fallbackResponse, fallbackPageLimit, fallbackPageOffset);
      setAppointments(overviewResponse.appointments);
      setAppointmentOverviewHasMore(Boolean(overviewResponse.has_more));
      setFallbackRequests(fallbackOverviewResponse.booking_attempts);
      setFallbackOverviewHasMore(Boolean(fallbackOverviewResponse.has_more));
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);
      setAvailabilityServiceID((current) => current || firstBookableServiceID(serviceResponse.services, firstSalon.active_pos_provider));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load appointment data.");
    } finally {
      setCalendarLoading(false);
      if (!silent) {
        setLoading(false);
      } else {
        setAppointmentListLoading(false);
        setFallbackListLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
    return () => {
      reconciliationListRequestIDRef.current += 1;
      reconciliationCandidateRequestIDRef.current += 1;
      clearAvailabilityExpiryTimer(availabilityExpiryTimerRef);
      clearAvailabilityExpiryTimer(actionAvailabilityExpiryTimerRef);
    };
  }, []);

  async function reloadCalendar() {
    if (!salon?.id) return;
    setCalendarLoading(true);
    setCalendarError("");
    try {
      const response = await getManleAICalendar(salon.id);
      setCalendar(response.manleai_calendar);
    } catch (calendarFailure) {
      setCalendarError(errorMessage(calendarFailure, "Could not load scheduling readiness."));
    } finally {
      setCalendarLoading(false);
    }
  }

  const serviceNames = useMemo(
    () => new Map(services.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [services]
  );
  const staffNames = useMemo(
    () => new Map(staff.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [staff]
  );
  const bookableServices = useMemo(
    () => services.filter((service) => serviceIsBookable(service, salon?.active_pos_provider)),
    [salon?.active_pos_provider, services]
  );
  const bookableStaff = useMemo(
    () => staff.filter((member) => staffIsBookable(member, salon?.active_pos_provider)),
    [salon?.active_pos_provider, staff]
  );
  const pendingRequests = useMemo(
    () => fallbackRequests,
    [fallbackRequests]
  );
  const reconciliationHasMore = reconciliationQueueStatuses.some(
    (status) => reconciliationPageState[status].hasMore
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
      appointments.filter((item) => isConfirmedAppointmentStatus(item.status) && new Date(item.start_time).getTime() >= now)
        .length,
      appointmentOverviewHasMore
    );
  }, [appointments, appointmentOverviewHasMore]);
  const confirmedCount = appointmentCountLabel(
    appointments.filter((item) => isConfirmedAppointmentStatus(item.status)).length,
    appointmentOverviewHasMore
  );
  const internalAppointmentCount = appointmentCountLabel(
    appointments.filter((item) => item.scheduling_authority === "manleai_calendar").length,
    appointmentOverviewHasMore
  );
  const selectedAuthority = salon?.scheduling_authority ?? calendar?.scheduling_authority;
  const selectedAuthorityVersion = salon?.scheduling_authority_version ?? calendar?.authority_version;
  const authorityEvidenceConsistent = Boolean(
    selectedAuthority
    && selectedAuthorityVersion
    && calendar
    && calendar.scheduling_authority === selectedAuthority
    && calendar.authority_version === selectedAuthorityVersion
  );
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
  const bookingDataReady = bookingPathReady(status);
  const bookingWriteReady = bookingDataReady && !status?.readiness?.booking_write_blocked;
  const externalAuthoritySelected = authorityEvidenceConsistent && selectedAuthority === "external_provider";
  const externalStatusAuthorityConsistent = status?.readiness?.scheduling_authority === "external_provider";
  const externalNewWorkSelected = externalAuthoritySelected && externalStatusAuthorityConsistent;
  const internalNewWorkSelected = authorityEvidenceConsistent && selectedAuthority === "manleai_calendar";
  const internalCreateReady = internalNewWorkSelected && Boolean(
    calendar?.readiness.capabilities?.staff_only_create
    || calendar?.readiness.capabilities?.pooled_capacity
    || calendar?.readiness.capabilities?.party_create
  );
  const readyForAvailability = bookingDataReady && bookableServices.length > 0 && externalNewWorkSelected;
  const readyForExternalLifecycle = bookingDataReady && bookableServices.length > 0 && bookableStaff.length > 0;
  const readyForManualBooking = bookingWriteReady && readyForExternalLifecycle;
  const readyForNewBooking = readyForManualBooking && externalNewWorkSelected;
  const retryCreateRequest = actionForm.retryOfAttemptID
    ? fallbackRequests.find((request) => request.id === actionForm.retryOfAttemptID) ?? null
    : null;
  const readyForExternalCreateAction = retryCreateRequest
    ? readyForManualBooking && isEligibleSafeExternalCreateRetry(retryCreateRequest)
    : readyForNewBooking;
  const readyForNewAppointment = readyForNewBooking || internalCreateReady;
  const selectedActionSlot = useMemo(
    () => (actionAvailabilityResult?.slots ?? []).find((slot) => slotKey(slot) === actionForm.selectedSlotKey) ?? null,
    [actionAvailabilityResult, actionForm.selectedSlotKey]
  );
  const selectedReconciliationAttempt = useMemo(
    () => reconciliationAttemptForTask(reconciliationTask, fallbackRequests),
    [fallbackRequests, reconciliationTask]
  );
  async function checkAvailability() {
    if (!salon || !availabilityServiceID || !selectedDate || !readyForAvailability) return;
    const requestID = ++availabilityRequestIDRef.current;
    clearAvailabilityExpiryTimer(availabilityExpiryTimerRef);
    setAvailabilityError("");
    setAvailabilityChecked(true);
    setCheckingAvailability(true);
    const staffSelectionMode = availabilityStaffID ? "specific" : "anyone";
    const payload = {
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
    };
    try {
      const result = await apiRequest<AvailabilityResult>(`/api/salons/${salon.id}/availability`, {
        method: "POST",
        body: JSON.stringify(payload)
      });
      if (requestID !== availabilityRequestIDRef.current) return;
      setAvailabilityResult(result);
      scheduleAvailabilityExpiry(
        availabilityExpiryTimerRef,
        result.expires_at,
        requestID,
        availabilityRequestIDRef,
        () => {
          setAvailabilityResult(null);
          setAvailabilityChecked(false);
          setAvailabilityError("These availability results expired. Check Square Appointments availability again for current slots.");
        }
      );
    } catch (err) {
      if (requestID !== availabilityRequestIDRef.current) return;
      setAvailabilityResult(null);
      setAvailabilityError(err instanceof Error ? err.message : "Could not check Square Appointments availability.");
    } finally {
      if (requestID === availabilityRequestIDRef.current) {
        setCheckingAvailability(false);
      }
    }
  }

  function setDateFromShortcut(offsetDays: number) {
    const today = formatDateInput(new Date(), salon?.timezone);
    setSelectedDate(addDaysInput(today, offsetDays));
    resetOverviewAvailability();
  }

  function updateActionForm(patch: Partial<AppointmentActionForm>) {
    const invalidatesAvailability =
      Object.prototype.hasOwnProperty.call(patch, "serviceID") ||
      Object.prototype.hasOwnProperty.call(patch, "staffID") ||
      Object.prototype.hasOwnProperty.call(patch, "preferredDate") ||
      Object.prototype.hasOwnProperty.call(patch, "preservedSegments");
    if (invalidatesAvailability) {
      invalidateActionAvailability();
    }
    setActionForm((current) => ({
      ...current,
      ...patch,
      ...(invalidatesAvailability ? { selectedSlotKey: "" } : {})
    }));
  }

  function resetOverviewAvailability() {
    availabilityRequestIDRef.current += 1;
    clearAvailabilityExpiryTimer(availabilityExpiryTimerRef);
    setAvailabilityResult(null);
    setAvailabilityChecked(false);
    setCheckingAvailability(false);
    setAvailabilityError("");
  }

  function invalidateActionAvailability(message = "") {
    actionAvailabilityRequestIDRef.current += 1;
    clearAvailabilityExpiryTimer(actionAvailabilityExpiryTimerRef);
    setActionAvailabilityResult(null);
    setActionAvailabilityChecked(false);
    setCheckingActionAvailability(false);
    setActionAvailabilityError(message);
    setActionForm((current) => ({ ...current, selectedSlotKey: "" }));
  }

  function resetActionAvailability() {
    invalidateActionAvailability();
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

  function updateFallbackPageSize(limit: number) {
    setFallbackLimit(limit);
    setFallbackOffset(0);
    if (salon) {
      void reloadFallbackRows(salon.id, 0, limit);
    }
  }

  function goToPreviousFallbackPage() {
    const previousOffset = Math.max(0, fallbackOffset - fallbackLimit);
    setFallbackOffset(previousOffset);
    if (salon) {
      void reloadFallbackRows(salon.id, previousOffset, fallbackLimit);
    }
  }

  function goToNextFallbackPage() {
    if (!fallbackHasMore) return;
    const nextOffset = fallbackOffset + fallbackLimit;
    setFallbackOffset(nextOffset);
    if (salon) {
      void reloadFallbackRows(salon.id, nextOffset, fallbackLimit);
    }
  }

  function openAppointmentReview(appointment: AppointmentRecord) {
    setAppointmentReviewAppointment(appointment);
    setFallbackReviewRequest(null);
  }

  function closeAppointmentReview() {
    setAppointmentReviewAppointment(null);
  }

  function openCreateAppointment() {
    setActionNotice(null);
    if (internalCreateReady) {
      setInternalCreateOpen(true);
      return;
    }
    if (readyForNewBooking) openCreateBooking();
  }

  async function handleInternalAppointmentConfirmed(appointmentID: string) {
    setActionNotice({
      tone: "success",
      title: "Appointment confirmed",
      message: `ManleAI Calendar returned durable appointment ID ${appointmentID} after the atomic commit.`
    });
    await load({ silent: true });
  }

  function openCreateBooking() {
    actionOperationKeyRef.current = null;
    setActionMode("create");
    setSelectedAppointment(null);
    setActionForm({
      ...emptyActionForm(selectedDate),
      serviceID: firstBookableServiceID(services, salon?.active_pos_provider)
    });
    setActionError("");
    setActionNotice(null);
    resetActionAvailability();
  }

  function openCreateBookingFromFallback(request: BookingAttempt) {
    actionOperationKeyRef.current = null;
    const retryServiceID = bookingAttemptPrimaryServiceID(request);
    const retryStaffID = bookingAttemptRetryStaffID(request);
    const preservedSegments = bookingAttemptRetrySegments(request);
    setActionMode("create");
    setSelectedAppointment(null);
    setFallbackReviewRequest(null);
    setActionForm({
      ...emptyActionForm(formatDateInput(new Date(request.requested_start_time), salon?.timezone)),
      customerName: request.customer_name,
      customerPhone: request.customer_phone,
      customerEmail: request.customer_email ?? "",
      serviceID: bookableServices.some((item) => item.id === retryServiceID) ? retryServiceID : "",
      staffID: bookableStaff.some((item) => item.id === retryStaffID) ? retryStaffID : "",
      notes: request.notes ?? "",
      preservedSegments,
      retryOfAttemptID: request.id,
      retryRequestedStartTime: request.requested_start_time,
      retryRequestedEndTime: request.requested_end_time
    });
    setActionError("");
    setActionNotice(null);
    resetActionAvailability();
  }

  function openReschedule(appointment: AppointmentRecord, preferredDate?: string, retryRequest?: BookingAttempt) {
    if (!canRescheduleAppointment(appointment)) {
      setActionNotice({
        tone: "warning",
        title: "Reschedule unavailable",
        message: "This appointment's persisted origin or backend capability does not allow rescheduling. Review the row evidence before taking another action."
      });
      return;
    }
    if (appointment.scheduling_authority === "manleai_calendar") {
      setInternalLifecycle({ mode: "reschedule", appointment });
      setAppointmentReviewAppointment(null);
      setFallbackReviewRequest(null);
      setActionNotice(null);
      return;
    }
    if (appointment.scheduling_authority !== "external_provider") {
      setActionNotice({
        tone: "warning",
        title: "Appointment action unavailable",
        message: "This row does not have a supported confirming appointment authority. Review its origin before taking a lifecycle action."
      });
      return;
    }
    actionOperationKeyRef.current = null;
    setActionMode("reschedule");
    setSelectedAppointment(appointment);
    setAppointmentReviewAppointment(null);
    setFallbackReviewRequest(null);
    setActionForm({
      ...emptyActionForm(preferredDate ?? formatDateInput(new Date(appointment.start_time), salon?.timezone)),
      customerName: appointment.customer_name,
      customerPhone: appointment.customer_phone,
      customerEmail: appointment.customer_email ?? "",
      serviceID: appointmentPrimaryServiceID(appointment),
      staffID: "",
      notes: retryRequest?.notes ?? appointment.notes ?? "",
      preservedSegments: retryRequest ? bookingAttemptRetrySegments(retryRequest) : [],
      retryOfAttemptID: retryRequest?.id ?? "",
      retryRequestedStartTime: retryRequest?.requested_start_time ?? "",
      retryRequestedEndTime: retryRequest?.requested_end_time ?? ""
    });
    setActionError("");
    setActionNotice(null);
    resetActionAvailability();
  }

  function openCancel(appointment: AppointmentRecord, reason = "", retryRequest?: BookingAttempt) {
    if (!canCancelAppointment(appointment)) {
      setActionNotice({
        tone: "warning",
        title: "Cancellation unavailable",
        message: "This appointment's persisted origin or backend capability does not allow cancellation. Review the row evidence before taking another action."
      });
      return;
    }
    if (appointment.scheduling_authority === "manleai_calendar") {
      setInternalLifecycle({ mode: "cancel", appointment });
      setAppointmentReviewAppointment(null);
      setFallbackReviewRequest(null);
      setActionNotice(null);
      return;
    }
    if (appointment.scheduling_authority !== "external_provider") {
      setActionNotice({
        tone: "warning",
        title: "Appointment action unavailable",
        message: "This row does not have a supported confirming appointment authority. Review its origin before taking a lifecycle action."
      });
      return;
    }
    actionOperationKeyRef.current = null;
    setActionMode("cancel");
    setSelectedAppointment(appointment);
    setAppointmentReviewAppointment(null);
    setFallbackReviewRequest(null);
    setActionForm({
      ...emptyActionForm(formatDateInput(new Date(appointment.start_time), salon?.timezone)),
      customerName: appointment.customer_name,
      customerPhone: appointment.customer_phone,
      customerEmail: appointment.customer_email ?? "",
      serviceID: appointmentPrimaryServiceID(appointment),
      staffID: "",
      notes: appointment.notes ?? "",
      cancelReason: reason,
      preservedSegments: retryRequest ? bookingAttemptRetrySegments(retryRequest) : [],
      retryOfAttemptID: retryRequest?.id ?? "",
      retryRequestedStartTime: retryRequest?.requested_start_time ?? "",
      retryRequestedEndTime: retryRequest?.requested_end_time ?? ""
    });
    setActionError("");
    setActionNotice(null);
	resetActionAvailability();
  }

  function closeInternalLifecycle() {
    setInternalLifecycle(null);
  }

  async function handleInternalLifecycleConfirmed(
    appointmentID: string,
    mode: "reschedule" | "cancel",
    version: number
  ) {
    setActionNotice({
      tone: "success",
      title: mode === "reschedule" ? "Appointment rescheduled" : "Appointment cancelled",
      message: mode === "reschedule"
        ? `ManleAI Calendar advanced durable root ${appointmentID} to version ${version} with the exact selected child plan.`
        : `ManleAI Calendar advanced durable root ${appointmentID} to version ${version} and proved zero active children.`
    });
    await load({ silent: true });
  }

  async function handleInternalLifecycleConflict(message: string) {
    setActionNotice({ tone: "warning", title: "Appointment changed", message });
    await load({ silent: true });
  }

  function openFallbackReview(request: BookingAttempt) {
    setFallbackReviewRequest(request);
    setAppointmentReviewAppointment(null);
  }

  function closeFallbackReview() {
    setFallbackReviewRequest(null);
  }

  async function openReconciliation(task: BookingReconciliationTask) {
    if (!salon) return;
    const requestID = ++reconciliationCandidateRequestIDRef.current;
    reconciliationActionRef.current = null;
    setReconciliationTask(task);
    setReconciliationCandidates([]);
    setReconciliationCandidatesLoading(true);
    setReconciliationCandidateID("");
    setReconciliationNote("");
    setReconciliationNotCreatedConfirmed(false);
    setReconciliationError("");
    setFallbackReviewRequest(null);
    try {
      const response = await apiRequest<ReconciliationCandidatesResponse>(
        `/api/salons/${salon.id}/booking-reconciliations/${task.booking_attempt_id}/candidates`
      );
      if (requestID !== reconciliationCandidateRequestIDRef.current) return;
      setReconciliationCandidates(response.candidates);
    } catch (err) {
      if (requestID !== reconciliationCandidateRequestIDRef.current) return;
      setReconciliationError(
        err instanceof Error ? err.message : "Could not load verified provider booking candidates."
      );
    } finally {
      if (requestID === reconciliationCandidateRequestIDRef.current) {
        setReconciliationCandidatesLoading(false);
      }
    }
  }

  function openReconciliationForRequest(request: BookingAttempt) {
    if (request.scheduling_authority !== "external_provider") {
      setActionNotice({
        tone: "warning",
        title: "Provider reconciliation unavailable",
        message: "Reconciliation applies only to persisted external-provider operations. This request must be reviewed through its own scheduling authority."
      });
      return;
    }
    const task = reconciliationTasks.find((item) => item.booking_attempt_id === request.id);
    if (!task) {
      setActionNotice({
        tone: "warning",
        title: "Reconciliation task is not loaded",
        message: "Load more provider results or reload the reconciliation queue before resolving it. Retry remains blocked."
      });
      return;
    }
    void openReconciliation(task);
  }

  function closeReconciliation() {
    if (reconciliationSaving) return;
    reconciliationCandidateRequestIDRef.current += 1;
    reconciliationActionRef.current = null;
    setReconciliationTask(null);
    setReconciliationCandidates([]);
    setReconciliationCandidatesLoading(false);
    setReconciliationCandidateID("");
    setReconciliationNote("");
    setReconciliationNotCreatedConfirmed(false);
    setReconciliationError("");
  }

  async function resolveReconciliation(action: "provider_attached" | "not_created" | "escalated") {
    if (!salon || !reconciliationTask || !selectedReconciliationAttempt) return;
    if (selectedReconciliationAttempt.scheduling_authority !== "external_provider") {
      setReconciliationError("This operation does not have an external-provider origin, so provider reconciliation is disabled.");
      return;
    }
    const candidate = reconciliationCandidates.find((item) => item.appointment_id === reconciliationCandidateID);
    if (action === "provider_attached" && !candidate) {
      setReconciliationError("Select a provider-synced appointment that matches this request.");
      return;
    }
    if (action === "not_created" && !reconciliationNotCreatedConfirmed) {
      setReconciliationError("Confirm that the persisted external provider was checked and the requested booking action was not applied.");
      return;
    }
    const payload = {
      action,
      provider_appointment_id: candidate?.provider_appointment_id ?? "",
      provider_appointment_version: candidate?.provider_appointment_version ?? 0,
      provider_status: action === "provider_attached" ? candidate?.provider_status ?? "" : "",
      note: reconciliationNote.trim()
    };
    const actionKey = operationKeyForPayload(reconciliationActionRef, payload);
    setReconciliationSaving(true);
    setReconciliationError("");
    try {
      await apiRequest<BookingReconciliationTask>(
        `/api/salons/${salon.id}/booking-reconciliations/${reconciliationTask.booking_attempt_id}/resolve`,
        {
          method: "POST",
          body: JSON.stringify({ ...payload, action_key: actionKey })
        }
      );
      setActionNotice({
        tone: action === "escalated" ? "warning" : "success",
        title: action === "escalated" ? "Reconciliation escalated" : "Reconciliation saved",
        message:
          action === "provider_attached"
            ? "The request was linked only after a matching provider-synced appointment was verified."
            : action === "not_created"
              ? "The request is marked safe to retry because the owner verified that the requested provider action was not applied."
              : "Retry remains blocked until the provider result is resolved."
      });
      setReconciliationTask(null);
      setReconciliationCandidates([]);
      reconciliationActionRef.current = null;
      await load({ silent: true });
    } catch (err) {
      setReconciliationError(err instanceof Error ? err.message : "Could not resolve provider reconciliation.");
    } finally {
      setReconciliationSaving(false);
    }
  }

  function retryFallbackRequest(request: BookingAttempt) {
    if (!canRetryFallbackRequest(request)) {
      setActionNotice({
        tone: "warning",
        title: "Retry unavailable",
        message: fallbackRetryDisabledReason(request)
      });
      return;
    }
    const action = fallbackBookingAction(request);
    if (action === "book") {
      openCreateBookingFromFallback(request);
      return;
    }
    const appointment = request.appointment;
    if (!appointment) {
      setFallbackReviewRequest(request);
      return;
    }
    if (action === "reschedule") {
      openReschedule(
        appointment,
        formatDateInput(new Date(request.requested_start_time), salon?.timezone),
        request
      );
      return;
    }
    openCancel(appointment, request.notes ?? "", request);
  }

  function closeActionPanel() {
    actionOperationKeyRef.current = null;
    setActionMode(null);
    setSelectedAppointment(null);
    setActionError("");
    resetActionAvailability();
  }

  async function checkActionAvailability() {
    const actionPathReady = actionMode === "create" ? readyForExternalCreateAction : readyForExternalLifecycle;
    if (!salon || !actionMode || actionMode === "cancel" || !actionForm.preferredDate || !actionPathReady) {
      return;
    }
    const requestID = ++actionAvailabilityRequestIDRef.current;
    clearAvailabilityExpiryTimer(actionAvailabilityExpiryTimerRef);
    setActionAvailabilityError("");
    setActionAvailabilityChecked(true);
    setCheckingActionAvailability(true);
    setActionAvailabilityResult(null);
    setActionForm((current) => ({ ...current, selectedSlotKey: "" }));
    try {
      const segments = actionAvailabilitySegments(actionMode, actionForm, selectedAppointment);
      if (segments.length === 0 || segments.some((segment) => !segment.service_id)) {
        throw new Error("This appointment is missing service details needed to check availability.");
      }
      const staffSelectionMode = aggregateStaffSelectionMode(segments);
      const retryOfAttemptID = actionMode === "create" ? actionForm.retryOfAttemptID : "";
      const result = await apiRequest<AvailabilityResult>(`/api/salons/${salon.id}/availability`, {
        method: "POST",
        body: JSON.stringify({
          target_appointment_id: actionMode === "reschedule" ? selectedAppointment?.id : undefined,
          retry_of_attempt_id: retryOfAttemptID || undefined,
          service_id: segments[0].service_id,
          staff_id: actionMode === "create" ? actionForm.staffID : "",
          staff_selection_mode: staffSelectionMode,
          segments,
          preferred_date: actionForm.preferredDate,
          limit: 5
        })
      });
      if (requestID !== actionAvailabilityRequestIDRef.current) return;
      const exactRetrySlots = actionForm.retryOfAttemptID
        ? result.slots.filter((slot) => retrySlotMatchesStoredRequest(actionForm, slot))
        : result.slots;
      const nextResult = exactRetrySlots === result.slots ? result : { ...result, slots: exactRetrySlots };
      setActionAvailabilityResult(nextResult);
      if (actionForm.retryOfAttemptID && exactRetrySlots.length === 0) {
        setActionAvailabilityError(
          "The original requested time and technician assignments are no longer available. Close this retry and create a new request instead of changing this retry lineage."
        );
      }
      scheduleAvailabilityExpiry(
        actionAvailabilityExpiryTimerRef,
        result.expires_at,
        requestID,
        actionAvailabilityRequestIDRef,
        () => {
          setActionAvailabilityResult(null);
          setActionAvailabilityChecked(false);
          setActionForm((current) => ({ ...current, selectedSlotKey: "" }));
          setActionAvailabilityError("This availability quote expired. Check Square Appointments availability again before submitting.");
        }
      );
    } catch (err) {
      if (requestID !== actionAvailabilityRequestIDRef.current) return;
      setActionAvailabilityResult(null);
      setActionAvailabilityError(
        err instanceof Error ? err.message : "Could not check Square Appointments availability."
      );
    } finally {
      if (requestID === actionAvailabilityRequestIDRef.current) {
        setCheckingActionAvailability(false);
      }
    }
  }

  async function submitCreateBooking() {
    if (!salon || !selectedActionSlot || !actionAvailabilityResult || !readyForExternalCreateAction) return;
    setSavingAction(true);
    setActionError("");
    setActionNotice(null);
    try {
      assertAvailabilityQuoteUsable(actionAvailabilityResult, selectedActionSlot);
	  const requestedSegments = actionAvailabilitySegments("create", actionForm, null);
	  const segments = slotBookingSegments(selectedActionSlot, requestedSegments);
      if (segments.length === 0 || segments.some((segment) => !segment.staff_id)) {
        throw new Error("Select a returned Square Appointments slot before creating the booking.");
      }
      const staffSelectionMode = aggregateStaffSelectionMode(segments);
      const payload = {
        retry_of_attempt_id: actionForm.retryOfAttemptID || undefined,
        availability_quote_id: actionAvailabilityResult.quote_id,
        slot_fingerprint: selectedActionSlot.fingerprint,
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
      };
      const attempt = await apiRequest<BookingAttempt>(`/api/salons/${salon.id}/booking-attempts`, {
        method: "POST",
        body: JSON.stringify({ ...payload, operation_key: operationKeyForPayload(actionOperationKeyRef, payload) })
      });
      if (!hasExternalAttemptConfirmation(attempt)) {
        setActionNotice({
          tone: "warning",
          title: "Booking needs owner review",
          message: "Square Appointments did not return confirmation evidence. The request remains pending for owner review."
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
    if (!salon || !selectedAppointment || !selectedActionSlot || !canRescheduleAppointment(selectedAppointment)) return;
    setSavingAction(true);
    setActionError("");
    setActionNotice(null);
    try {
      if (!actionAvailabilityResult) {
        throw new Error("Check the appointment's external-provider availability again before rescheduling.");
      }
      assertAvailabilityQuoteUsable(actionAvailabilityResult, selectedActionSlot);
	  const requestedSegments = actionAvailabilitySegments("reschedule", actionForm, selectedAppointment);
	  const segments = slotBookingSegments(selectedActionSlot, requestedSegments);
	  if (segments.length === 0) {
		throw new Error("External-provider availability did not preserve every requested service. Check availability again.");
	  }
      const payload = {
        retry_of_attempt_id: actionForm.retryOfAttemptID || undefined,
        availability_quote_id: actionAvailabilityResult.quote_id,
        slot_fingerprint: selectedActionSlot.fingerprint,
        start_time: selectedActionSlot.start_time,
        staff_id: actionForm.staffID,
        segments,
        notes: actionForm.notes
      };
      const response = await apiRequest<AppointmentRecord | BookingAttempt>(
        `/api/salons/${salon.id}/appointments/${selectedAppointment.id}/reschedule`,
        {
          method: "POST",
          body: JSON.stringify({ ...payload, operation_key: operationKeyForPayload(actionOperationKeyRef, payload) })
        }
      );
      if (isBookingAttempt(response)) {
        setActionNotice({
          tone: "warning",
          title: "Reschedule needs owner review",
          message: "The appointment's external provider did not confirm this reschedule. The original appointment was left unchanged."
        });
      } else {
        setActionNotice({
          tone: "success",
          title: "Appointment rescheduled",
          message: "The appointment's external provider confirmed the new time before the dashboard updated this appointment."
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
    if (!salon || !selectedAppointment || !canCancelAppointment(selectedAppointment)) return;
    setSavingAction(true);
    setActionError("");
    setActionNotice(null);
    try {
      const payload = {
        retry_of_attempt_id: actionForm.retryOfAttemptID || undefined,
        segments:
          actionForm.preservedSegments.length > 0
            ? actionForm.preservedSegments
            : appointmentRequestSegments(selectedAppointment, ""),
        reason: actionForm.cancelReason
      };
      const response = await apiRequest<AppointmentRecord | BookingAttempt>(
        `/api/salons/${salon.id}/appointments/${selectedAppointment.id}/cancel`,
        {
          method: "POST",
          body: JSON.stringify({ ...payload, operation_key: operationKeyForPayload(actionOperationKeyRef, payload) })
        }
      );
      if (isBookingAttempt(response)) {
        setActionNotice({
          tone: "warning",
          title: "Cancellation needs owner review",
          message: "The appointment's external provider did not confirm cancellation. The original appointment was left unchanged."
        });
      } else {
        setActionNotice({
          tone: "success",
          title: "Appointment cancelled",
          message: "The appointment's external provider confirmed the cancellation before the dashboard updated this appointment."
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
            <h1 className="text-2xl font-bold text-ink">Appointments</h1>
          <p className="mt-1 text-sm text-muted">
            Review scheduling work by its originating authority without treating a POS connection as a universal prerequisite.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "AI calls active" : "AI calls paused"} />
          <Button
            type="button"
            onClick={openCreateAppointment}
            disabled={!readyForNewAppointment || savingAction}
          >
            <CalendarPlus className="h-4 w-4" />
            Create appointment
          </Button>
          <Button type="button" variant="secondary" onClick={() => void load({ silent: true })}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Appointments unavailable" message={error} /> : null}
      {actionNotice ? <ActionNoticePanel notice={actionNotice} /> : null}
      {selectedAuthority === "external_provider" && squareStatusError ? (
        <Alert
          title="Square Appointments status unavailable"
          message={`${squareStatusError} Appointment history and owner-review requests remain available.`}
        />
      ) : null}
      {externalAuthoritySelected && status && !externalStatusAuthorityConsistent ? (
        <Alert
          title="Square Appointments authority changed"
          message="The provider-readiness response does not identify Square Appointments as the current scheduling path. New Square Appointments work is disabled until the authority evidence agrees; historical rows remain available by origin."
        />
      ) : null}

      <SchedulingAuthorityBanner
        authority={selectedAuthority}
        version={selectedAuthorityVersion}
        evidenceConsistent={authorityEvidenceConsistent}
        activeProvider={salon.active_pos_provider}
      />

      <SchedulingReadinessCard calendar={calendar} loading={calendarLoading} error={calendarError} onRetry={() => void reloadCalendar()} />
      {calendar?.scheduling_authority === "external_provider" ? (
        <>
          <ReadinessPanel status={status} />
          <BookingBoundaryPanel />
        </>
      ) : null}

      <Dialog
        open={internalCreateOpen}
        title="Create appointment"
        description="Create one atomic ManleAI Calendar appointment from a backend-verified staff, time, guest, and resource plan."
        onClose={() => setInternalCreateOpen(false)}
        closeDisabled={internalCreateBusy}
        className="max-w-4xl"
      >
        {calendar ? (
          <InternalAppointmentCreate
            salonID={salon.id}
            timezone={salon.timezone}
            calendar={calendar}
            capabilityReady={Boolean(
              calendar.readiness.capabilities?.staff_only_create
              || calendar.readiness.capabilities?.pooled_capacity
              || calendar.readiness.capabilities?.party_create
            )}
            capabilityBlockers={calendar.readiness.blockers.map((blocker) => blocker.message)}
            onClose={() => setInternalCreateOpen(false)}
            onConfirmed={handleInternalAppointmentConfirmed}
            onReadinessInvalidated={reloadCalendar}
            onBusyChange={setInternalCreateBusy}
          />
        ) : (
          <Alert title="Internal scheduling unavailable" message="Scheduling readiness could not be loaded." />
        )}
      </Dialog>

      <Dialog
        open={Boolean(internalLifecycle)}
        title={internalLifecycle?.mode === "cancel" ? "Cancel internal appointment" : "Reschedule internal appointment"}
        description="This lifecycle action follows the appointment's persisted ManleAI Calendar origin and version, even when the salon's current authority differs."
        onClose={closeInternalLifecycle}
        closeDisabled={internalLifecycleBusy}
        className={internalLifecycle?.mode === "cancel" ? "max-w-2xl" : "max-w-5xl"}
      >
        {internalLifecycle ? (
          <InternalAppointmentLifecycle
            key={`${internalLifecycle.mode}:${internalLifecycle.appointment.id}:${internalLifecycle.appointment.authority_appointment_version ?? 0}`}
            salonID={salon.id}
            timezone={salon.timezone}
            appointment={internalLifecycle.appointment}
            mode={internalLifecycle.mode}
            cutoffMinutes={internalLifecycle.mode === "reschedule"
              ? calendar?.config?.reschedule_cutoff_minutes
              : calendar?.config?.cancellation_cutoff_minutes}
            onClose={closeInternalLifecycle}
            onConfirmed={handleInternalLifecycleConfirmed}
            onConflict={handleInternalLifecycleConflict}
            onBusyChange={setInternalLifecycleBusy}
          />
        ) : null}
      </Dialog>

      <Dialog
        open={Boolean(actionMode)}
        title={actionMode ? actionDialogTitle(actionMode) : "Appointment action"}
        description={actionMode ? actionDialogDescription(actionMode) : undefined}
        onClose={closeActionPanel}
        closeDisabled={savingAction}
        className={actionMode === "cancel" ? "max-w-2xl" : "max-w-4xl"}
      >
        {actionMode ? (
          <BookingActionPanel
            mode={actionMode}
            form={actionForm}
            selectedAppointment={selectedAppointment}
            bookableServices={bookableServices}
            bookableStaff={bookableStaff}
            readyForManualBooking={actionMode === "create" ? readyForExternalCreateAction : readyForExternalLifecycle}
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
      </Dialog>

      <Dialog
        open={Boolean(appointmentReviewAppointment)}
        title="View appointment"
        description="Review confirmation evidence and authority-specific lifecycle capabilities."
        onClose={closeAppointmentReview}
        closeDisabled={savingAction}
        className="max-w-2xl"
      >
        {appointmentReviewAppointment ? (
          <AppointmentReviewDialog
            salonID={salon.id}
            appointment={appointmentReviewAppointment}
            timezone={salon.timezone}
            disabled={savingAction}
            onReschedule={openReschedule}
            onCancel={openCancel}
          />
        ) : null}
      </Dialog>

      <Dialog
        open={Boolean(fallbackReviewRequest)}
        title="Review pending request"
        description="This external-provider request is not confirmed. Retry actions must still pass through its persisted external-provider origin."
        onClose={closeFallbackReview}
        closeDisabled={savingAction}
        className="max-w-2xl"
      >
        {fallbackReviewRequest ? (
          <FallbackReviewDialog
            request={fallbackReviewRequest}
            timezone={salon.timezone}
            disabled={savingAction || !canRetryFallbackRequest(fallbackReviewRequest)}
            onRetry={retryFallbackRequest}
            onReconcile={openReconciliationForRequest}
          />
        ) : null}
      </Dialog>

      <Dialog
        open={Boolean(reconciliationTask)}
        title="Resolve provider result"
        description="Verify the result against provider-synced data before changing retry or confirmation state."
        onClose={closeReconciliation}
        closeDisabled={reconciliationSaving}
        className="max-w-2xl"
      >
        {reconciliationTask && selectedReconciliationAttempt ? (
          <ReconciliationDialog
            task={reconciliationTask}
            attempt={selectedReconciliationAttempt}
            candidates={reconciliationCandidates}
            candidatesLoading={reconciliationCandidatesLoading}
            selectedCandidateID={reconciliationCandidateID}
            note={reconciliationNote}
            notCreatedConfirmed={reconciliationNotCreatedConfirmed}
            timezone={salon.timezone}
            saving={reconciliationSaving}
            error={reconciliationError}
            onCandidateChange={setReconciliationCandidateID}
            onNoteChange={setReconciliationNote}
            onNotCreatedConfirmedChange={setReconciliationNotCreatedConfirmed}
            onResolve={(action) => void resolveReconciliation(action)}
          />
        ) : null}
      </Dialog>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Confirmed appointments" value={confirmedCount} />
        <Metric label="Upcoming" value={upcomingCount} />
        <Metric label="Pending requests" value={appointmentCountLabel(pendingRequests.length, fallbackOverviewHasMore)} />
        <Metric
          label={selectedAuthority === "manleai_calendar"
            ? "ManleAI-origin appointments"
            : selectedAuthority === "owner_manual"
              ? "Owner requests"
              : "Last Square Appointments sync"}
          value={selectedAuthority === "manleai_calendar"
            ? internalAppointmentCount
            : selectedAuthority === "owner_manual"
              ? appointmentCountLabel(pendingRequests.length, fallbackOverviewHasMore)
              : formatOptionalDate(status?.connection?.last_sync_at)}
        />
      </div>

      <div className={`grid gap-4 ${externalAuthoritySelected ? "xl:grid-cols-[1.25fr_0.75fr]" : ""}`}>
        <Card>
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <CardTitle>Calendar view</CardTitle>
              <CardDescription>
                Confirmed appointments and pending requests for the selected day, preserving each originating authority.
              </CardDescription>
            </div>
            <Badge value={dayAppointments.length + dayPendingRequests.length > 0 ? "active" : "disabled"} />
          </div>

          <DateControls
            selectedDate={selectedDate}
            onShortcut={setDateFromShortcut}
            onChange={(value) => {
              setSelectedDate(value);
              resetOverviewAvailability();
            }}
          />

          <DaySchedule
            selectedDate={selectedDate}
            appointments={dayAppointments}
            pendingRequests={dayPendingRequests}
            serviceNames={serviceNames}
            staffNames={staffNames}
            timezone={salon.timezone}
            onEdit={openAppointmentReview}
            onReschedule={openReschedule}
            onCancel={openCancel}
            onReviewFallback={openFallbackReview}
          />
        </Card>

        {externalAuthoritySelected ? <Card>
          <div className="flex items-start gap-3">
            <CalendarSearch className="mt-1 h-5 w-5 text-brand" />
            <div>
              <CardTitle>Find available slots</CardTitle>
              <CardDescription>
                Check Square Appointments availability before offering times or creating an owner-entered booking.
              </CardDescription>
            </div>
          </div>

          <div className="mt-5 grid gap-4">
            {!readyForAvailability ? (
              <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
                Connect Square Appointments, choose a location, and sync bookable services, staff, and business hours before checking availability.
              </div>
            ) : null}

            <label className="block">
              <span className="text-sm font-medium text-ink">Service</span>
              <select
                className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
                value={availabilityServiceID}
                onChange={(event) => {
                  setAvailabilityServiceID(event.target.value);
                  resetOverviewAvailability();
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
                  resetOverviewAvailability();
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
        </Card> : null}
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Appointment records</CardTitle>
            <CardDescription>
              Confirmation evidence and available actions follow each row's originating scheduling authority.
            </CardDescription>
          </div>
          <Badge value={appointments.length > 0 ? "active" : "disabled"} />
        </div>

        {appointmentListLoading ? (
          <div className="mt-4 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
            Loading appointment records...
          </div>
        ) : null}

        {appointments.length === 0 ? (
          <EmptyState
            icon={<CalendarClock className="h-5 w-5 text-muted" />}
            title="No appointments yet"
            message="Confirmed internal or external appointments will appear here with their originating authority."
          >
            <Button
              type="button"
              className="mt-4"
              onClick={openCreateAppointment}
              disabled={!readyForNewAppointment || savingAction}
            >
              <CalendarPlus className="h-4 w-4" />
              Create appointment
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
              itemLabel="appointment records"
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
                    <th className="px-4 py-3">Origin</th>
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
                        {item.scheduling_authority === "manleai_calendar" ? (
                          <div className="mt-1 text-xs text-muted">Version {item.authority_appointment_version ?? "-"}</div>
                        ) : null}
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={appointmentOriginBadge(item.scheduling_authority)} />
                        <div className="mt-1 text-xs text-muted">{appointmentOriginLabel(item)}</div>
                      </td>
                      <td className="px-4 py-3">
                        <AppointmentActions
                          appointment={item}
                          disabled={savingAction}
                          onReview={openAppointmentReview}
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
                  disabled={savingAction}
                  onReview={openAppointmentReview}
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
              itemLabel="appointment records"
              onPrevious={goToPreviousAppointmentPage}
              onNext={goToNextAppointmentPage}
              onLimitChange={updateAppointmentPageSize}
            />
          </>
        )}
      </Card>

      <OwnerReviewRequests key={salon.id} salonID={salon.id} timezone={salon.timezone} />
      <OwnerNotificationDeliveries salonID={salon.id} />

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Provider reconciliation</CardTitle>
            <CardDescription>
              Unknown or pending provider outcomes stay blocked until a provider-synced appointment is matched or the owner verifies that the requested booking action was not applied.
            </CardDescription>
          </div>
          <Badge
            value={reconciliationListError ? "error" : reconciliationTasks.length > 0 ? "needs_review" : "disabled"}
          />
        </div>

        {reconciliationLoading ? (
          <div className="mt-4 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
            {reconciliationTasks.length > 0
              ? "Loading more provider reconciliation tasks..."
              : "Loading provider reconciliation tasks..."}
          </div>
        ) : null}

        {reconciliationListError ? (
          <div className="mt-4 space-y-3" role="alert">
            <Alert title="Provider reconciliation unavailable" message={reconciliationListError} />
            <Button
              type="button"
              variant="secondary"
              disabled={reconciliationLoading}
              onClick={() => void reloadReconciliationTasks(salon.id)}
            >
              <RefreshCcw className="h-4 w-4" />
              Reload reconciliation queue
            </Button>
          </div>
        ) : null}

        {!reconciliationLoading && !reconciliationListError && reconciliationTasks.length === 0 ? (
          <EmptyState
            icon={<ClipboardList className="h-5 w-5 text-muted" />}
            title="No provider results need reconciliation"
            message="Uncertain POS writes and provider-pending bookings will appear here without being marked confirmed."
          />
        ) : null}

        {reconciliationTasks.length > 0 ? (
          <>
            <div className="mt-5 grid gap-3 lg:grid-cols-2">
              {reconciliationTasks.map((task) => {
                const attempt = reconciliationAttemptForTask(task, fallbackRequests);
                return attempt ? (
                  <ReconciliationTaskCard
                    key={task.id}
                    task={task}
                    attempt={attempt}
                    timezone={salon.timezone}
                    disabled={reconciliationSaving}
                    onReview={(task) => void openReconciliation(task)}
                  />
                ) : null;
              })}
            </div>
            <div className="mt-4 flex flex-col justify-between gap-3 border-t border-line pt-4 text-sm text-muted sm:flex-row sm:items-center">
              <span>
                Showing {reconciliationTasks.length} open or escalated provider result
                {reconciliationTasks.length === 1 ? "" : "s"}.
              </span>
              {reconciliationHasMore ? (
                <Button
                  type="button"
                  variant="secondary"
                  disabled={reconciliationLoading}
                  onClick={() => void loadMoreReconciliationTasks()}
                >
                  {reconciliationLoading ? "Loading more..." : "Load more provider results"}
                </Button>
              ) : (
                <span>All currently open and escalated results are loaded.</span>
              )}
            </div>
          </>
        ) : null}
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Fallback requests</CardTitle>
            <CardDescription>
              These external-provider requests are not confirmed appointments. Review them when the external-provider path fails.
            </CardDescription>
          </div>
          <Badge value={pendingRequests.length > 0 ? "fallback_pending" : "disabled"} />
        </div>

        {fallbackListLoading ? (
          <div className="mt-4 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
            Loading fallback requests...
          </div>
        ) : null}

        {fallbackRows.length === 0 ? (
          <EmptyState
            icon={<ClipboardList className="h-5 w-5 text-muted" />}
            title="No pending requests"
            message="External-provider failures will create pending requests here for owner review."
          />
        ) : (
          <>
            <AppointmentPaginationControls
              className="mt-5"
              count={fallbackRows.length}
              limit={fallbackLimit}
              offset={fallbackOffset}
              hasMore={fallbackHasMore}
              busy={fallbackListLoading || savingAction}
              itemLabel="fallback requests"
              onPrevious={goToPreviousFallbackPage}
              onNext={goToNextFallbackPage}
              onLimitChange={updateFallbackPageSize}
            />
            <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[1220px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Requested time</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Request type</th>
                    <th className="px-4 py-3">Services</th>
                    <th className="px-4 py-3">Technician preference</th>
                    <th className="px-4 py-3">Assigned technicians</th>
                    <th className="px-4 py-3">Failure reason</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {fallbackRows.map((item) => (
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
                      <td className="px-4 py-3">
                        <Badge value={fallbackBookingAction(item)} />
                        <div className="mt-1 text-xs text-muted">{fallbackActionLabel(item)}</div>
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
                      <td className="px-4 py-3">
                        <FallbackActions
                          request={item}
                          disabled={savingAction}
                          onReview={openFallbackReview}
                          onRetry={retryFallbackRequest}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {fallbackRows.map((item) => (
                <FallbackCard
                  key={item.id}
                  item={item}
                  serviceName={serviceNamesLabel(item, serviceNames)}
                  staffName={assignedTechniciansLabel(item, staffNames)}
                  technicianPreference={technicianPreferenceLabel(item)}
                  timezone={salon.timezone}
                  disabled={savingAction}
                  onReview={openFallbackReview}
                  onRetry={retryFallbackRequest}
                />
              ))}
            </div>
            <AppointmentPaginationControls
              className="mt-4"
              count={fallbackRows.length}
              limit={fallbackLimit}
              offset={fallbackOffset}
              hasMore={fallbackHasMore}
              busy={fallbackListLoading || savingAction}
              itemLabel="fallback requests"
              onPrevious={goToPreviousFallbackPage}
              onNext={goToNextFallbackPage}
              onLimitChange={updateFallbackPageSize}
            />
          </>
        )}
      </Card>
    </div>
  );
}

function emptyReconciliationPageState(): ReconciliationPageState {
  return {
    open: { offset: 0, hasMore: false },
    escalated: { offset: 0, hasMore: false }
  };
}

function cloneReconciliationPageState(state: ReconciliationPageState): ReconciliationPageState {
  return {
    open: { ...state.open },
    escalated: { ...state.escalated }
  };
}

function nextReconciliationPageState(
  response: ReconciliationTasksResponse,
  requestedOffset: number,
  status: ReconciliationQueueStatus
) {
  if (response.has_more && response.tasks.length === 0) {
    throw new Error(`Could not advance the ${status} reconciliation queue.`);
  }
  return {
    offset: (response.offset ?? requestedOffset) + response.tasks.length,
    hasMore: Boolean(response.has_more)
  };
}

function mergeReconciliationTasks(...groups: BookingReconciliationTask[][]) {
  const tasksByID = new Map<string, BookingReconciliationTask>();
  for (const group of groups) {
    for (const task of group) {
      tasksByID.set(task.id, task);
    }
  }
  return [...tasksByID.values()].sort((left, right) => {
    const createdAtDifference = new Date(left.created_at).getTime() - new Date(right.created_at).getTime();
    return createdAtDifference || left.id.localeCompare(right.id);
  });
}

function SchedulingAuthorityBanner({
  authority,
  version,
  evidenceConsistent,
  activeProvider
}: {
  authority?: SchedulingAuthority;
  version?: number;
  evidenceConsistent: boolean;
  activeProvider?: string;
}) {
  if (!authority || !version) {
    return (
      <Alert
        title="Scheduling authority unavailable"
        message="The salon response did not include a scheduling authority and version. New appointment actions are disabled until that backend contract is available."
      />
    );
  }
  if (!evidenceConsistent) {
    return (
      <Alert
        title="Scheduling authority changed"
        message="The salon and scheduling-readiness responses do not describe the same authority version. Refresh before starting new work. Historical rows remain visible by their persisted origin."
      />
    );
  }
  return (
    <Card className="border-blue-200 bg-blue-50 shadow-none">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle>Current scheduling authority</CardTitle>
            <Badge value={appointmentOriginBadge(authority)} />
          </div>
          <CardDescription className="text-blue-900">
            {authorityWorkflowDescription(authority, activeProvider)}
          </CardDescription>
        </div>
        <div className="text-sm font-semibold text-blue-900">Version {version}</div>
      </div>
    </Card>
  );
}

function ReadinessPanel({ status }: { status: StatusResponse | null }) {
  const connection = status?.connection;
  const readiness = status?.readiness;
  const connected = Boolean(connection?.id) && connection?.status === "active" && Boolean(connection?.last_sync_at);
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
            <CardTitle>Square Appointments booking writes are blocked</CardTitle>
              <Badge value="fallback_pending" />
            </div>
            <CardDescription className="text-red-900">
              Availability may remain readable, but new bookings cannot be confirmed until Square Appointments accepts booking writes.
            </CardDescription>
            <div className="mt-3 rounded-md border border-red-200 bg-white p-3 text-sm leading-6 text-red-900">
              <div className="font-semibold">{readiness?.booking_write_blocked_code || "Square Appointments booking write blocked"}</div>
              <div className="mt-1">{readiness?.booking_write_blocked_reason || "Square Appointments rejected booking writes."}</div>
              <div className="mt-1 text-xs text-muted">
                Last seen: {formatOptionalDate(readiness?.booking_write_blocked_at)}
              </div>
            </div>
            <a
              className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
              href="/dashboard/integrations"
            >
              Open Square Appointments integration
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
              <CardTitle>Square Appointments changes need owner review</CardTitle>
              <Badge value="fallback_pending" />
            </div>
            <CardDescription className="text-amber-900">
              New bookings can still be provider-confirmed, but Square Appointments rejected automatic reschedule or cancellation.
            </CardDescription>
            <div className="mt-3 rounded-md border border-amber-200 bg-white p-3 text-sm leading-6 text-amber-900">
              <div className="font-semibold">{readiness?.appointment_change_write_blocked_code || "Square Appointments write blocked"}</div>
              <div className="mt-1">{readiness?.appointment_change_write_blocked_reason || "Square Appointments rejected appointment-change writes."}</div>
              <div className="mt-1 text-xs text-muted">
                Last seen: {formatOptionalDate(readiness?.appointment_change_write_blocked_at)}
              </div>
            </div>
            <a
              className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
              href="/dashboard/integrations"
            >
              Open Square Appointments integration
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
            <CardTitle>Square Appointments booking path is configured</CardTitle>
            <CardDescription className="text-emerald-800">
              Provider-confirmed appointments require a successful Square Appointments booking ID. Owner-entered actions remain independent of AI-call enablement.
            </CardDescription>
          </div>
          <Badge value={readiness?.ai_enabled ? "AI calls active" : "AI calls paused"} />
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
            Connect Square Appointments, choose a location, and sync services, staff, and business hours before external booking can operate.
          </CardDescription>
          <a
            className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/integrations"
          >
            Open Square Appointments integration
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
            The dashboard separates provider-confirmed appointments from requests that still need owner review.
          </CardDescription>
        </div>
        <Badge value="pos_pending" />
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <BoundaryItem
          label="Provider-confirmed appointment"
          value="The selected provider returned a booking ID before the appointment was recorded as confirmed."
        />
        <BoundaryItem
          label="Pending request"
          value="The provider path did not confirm. Owner review is required and no appointment is marked confirmed."
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
  const retrying = Boolean(form.retryOfAttemptID);
  const retryServiceLabel = segmentServiceNames(form.preservedSegments, bookableServices);
  const retryStaffLabel = segmentStaffNames(form.preservedSegments, bookableStaff);
  const canCheckAvailability =
    readyForManualBooking &&
    !availabilityLoading &&
    !saving &&
    Boolean(form.preferredDate) &&
    (mode === "create" ? Boolean(form.serviceID || form.preservedSegments.length > 0) : Boolean(selectedAppointment));
  const canSubmitCreate =
    mode === "create" &&
    readyForManualBooking &&
    Boolean(selectedSlot) &&
    Boolean(selectedSlot?.fingerprint) &&
    availabilityQuoteIsUsable(availabilityResult) &&
    Boolean(form.customerName.trim()) &&
    Boolean(form.customerPhone.trim()) &&
    !saving;
  const canSubmitReschedule =
    mode === "reschedule" &&
    readyForManualBooking &&
    Boolean(selectedAppointment) &&
    Boolean(selectedSlot) &&
    Boolean(selectedSlot?.fingerprint) &&
    availabilityQuoteIsUsable(availabilityResult) &&
    !saving;

  return (
    <div>
      {!readyForManualBooking && mode !== "cancel" ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
          {retrying && mode === "create"
            ? "This stored request cannot obtain a fresh retry quote until its external-provider lineage, provider setup, and safe-retry evidence are all ready. The current scheduling authority does not reinterpret that persisted origin."
            : "Connect Square Appointments, choose a location, and keep at least one bookable service and staff member before creating or rescheduling external bookings."}
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
          retrying={retrying}
          saving={saving}
          onReasonChange={(value) => onChange({ cancelReason: value })}
          onSubmit={onCancelAppointment}
        />
      ) : (
        <div className="mt-5 grid gap-5">
          {retrying ? (
            <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
              <div className="font-semibold">Retrying a stored external-provider request</div>
              <div className="mt-1 leading-6">
                The original customer, time, notes, ordered service segments, technician assignments, and retry lineage are locked. Availability must return that exact request before it can be retried.
              </div>
            </div>
          ) : null}
          {mode === "create" ? (
            <CustomerFields form={form} disabled={saving || retrying} onChange={onChange} />
          ) : (
            <AppointmentActionSummary appointment={selectedAppointment} timezone={timezone} />
          )}

          <div className="grid gap-4 lg:grid-cols-3">
            {mode === "create" && !retrying ? (
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
            ) : mode === "create" ? (
              <ReadOnlyField label="Services" value={retryServiceLabel} />
            ) : (
              <ReadOnlyField label="Services" value={selectedAppointment ? serviceNamesLabel(selectedAppointment) : "-"} />
            )}

            {retrying ? (
              <ReadOnlyField label="Technician assignments" value={retryStaffLabel} />
            ) : (
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
            )}

            <label className="block">
              <span className="text-sm font-medium text-ink">Date</span>
              <input
                className={inputClassName}
                type="date"
                value={form.preferredDate}
                onChange={(event) => onChange({ preferredDate: event.target.value, selectedSlotKey: "" })}
                disabled={!readyForManualBooking || saving || availabilityLoading || retrying}
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
              disabled={saving || retrying}
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
                : "Select a returned Square Appointments slot before submitting."}
            </div>
          </div>

          {availabilityError ? <Alert title="Availability check failed" message={availabilityError} /> : null}

          <ActionAvailabilitySlots
            checked={availabilityChecked}
            loading={availabilityLoading}
            result={availabilityResult}
            retrying={retrying}
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
    </div>
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
        items={appointmentDetailItems(appointment)}
      />
    </div>
  );
}

function CancelAppointmentForm({
  appointment,
  reason,
  retrying,
  saving,
  onReasonChange,
  onSubmit
}: {
  appointment: AppointmentRecord | null;
  reason: string;
  retrying: boolean;
  saving: boolean;
  onReasonChange: (value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <div className="mt-5 grid gap-5">
      <AppointmentActionSummary appointment={appointment} />
      {retrying ? (
        <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
          This retry keeps the original cancellation reason and request lineage. Close it and start a new cancellation if the reason must change.
        </div>
      ) : null}
      <label className="block">
        <span className="text-sm font-medium text-ink">Cancellation reason</span>
        <textarea
          className={textareaClassName}
          value={reason}
          onChange={(event) => onReasonChange(event.target.value)}
          placeholder="Customer requested cancellation"
          disabled={saving || retrying}
        />
      </label>
      <div className="rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
        This does not delete appointment history. The dashboard marks this external-origin appointment cancelled only after its provider confirms the cancellation.
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
  retrying,
  selectedSlotKey,
  timezone,
  onSelect
}: {
  checked: boolean;
  loading: boolean;
  result: AvailabilityResult | null;
  retrying: boolean;
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
        message={
          retrying
            ? "The original requested time and technician assignments are unavailable. Close this retry and create a new request if the booking details must change."
            : "Try another day, service, or technician before submitting this Square Appointments action."
        }
      />
    );
  }
  return (
    <div className="space-y-3">
      <div className="rounded-md border border-line bg-slate-50 px-3 py-2 text-xs leading-5 text-muted">
        Availability quote valid until {formatQuoteExpiry(result.expires_at, timezone)}. Changing service, technician, or date requires a new check.
      </div>
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
  onReview,
  onReschedule,
  onCancel
}: {
  appointment: AppointmentRecord;
  disabled: boolean;
  onReview: (appointment: AppointmentRecord) => void;
  onReschedule: (appointment: AppointmentRecord) => void;
  onCancel: (appointment: AppointmentRecord) => void;
}) {
  const rescheduleDisabled = disabled || !canRescheduleAppointment(appointment);
  const cancelDisabled = disabled || !canCancelAppointment(appointment);
  return (
    <div className="flex flex-col gap-2 sm:flex-row">
      <Button type="button" variant="secondary" onClick={() => onReview(appointment)} disabled={disabled}>
        View
      </Button>
      <Button type="button" variant="secondary" onClick={() => onReschedule(appointment)} disabled={rescheduleDisabled}>
        Reschedule
      </Button>
      <Button type="button" variant="danger" onClick={() => onCancel(appointment)} disabled={cancelDisabled}>
        Cancel
      </Button>
    </div>
  );
}

function CalendarAppointmentActions({
  appointment,
  disabled,
  onEdit,
  onReschedule,
  onCancel
}: {
  appointment: AppointmentRecord;
  disabled: boolean;
  onEdit: (appointment: AppointmentRecord) => void;
  onReschedule: (appointment: AppointmentRecord) => void;
  onCancel: (appointment: AppointmentRecord) => void;
}) {
  const rescheduleDisabled = disabled || !canRescheduleAppointment(appointment);
  const cancelDisabled = disabled || !canCancelAppointment(appointment);
  return (
    <div className="flex flex-wrap gap-2 sm:justify-end">
      <Button type="button" variant="secondary" className="h-9 px-3" onClick={() => onEdit(appointment)} disabled={disabled}>
        <Pencil className="h-4 w-4" />
        View
      </Button>
      <Button type="button" variant="secondary" className="h-9 px-3" onClick={() => onReschedule(appointment)} disabled={rescheduleDisabled}>
        Reschedule
      </Button>
      <Button type="button" variant="danger" className="h-9 px-3" onClick={() => onCancel(appointment)} disabled={cancelDisabled}>
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
  itemLabel = "appointment records",
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
  itemLabel?: string;
  onPrevious: () => void;
  onNext: () => void;
  onLimitChange: (limit: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;

  return (
    <div
      className={`flex flex-col gap-3 rounded-md border border-line bg-slate-50 px-3 py-3 sm:flex-row sm:items-center sm:justify-between ${className}`}
    >
      <div className="text-sm leading-6 text-muted">{appointmentRangeLabel(count, offset, hasMore, itemLabel)}</div>
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

function appointmentRangeLabel(count: number, offset: number, hasMore: boolean, itemLabel = "appointment records") {
  if (count === 0) {
    return `No ${itemLabel}`;
  }
  const start = offset + 1;
  const end = offset + count;
  const total = hasMore ? `at least ${end + 1}` : String(end);
  return `Showing ${start}-${end} of ${total} ${itemLabel}`;
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
  timezone,
  onEdit,
  onReschedule,
  onCancel,
  onReviewFallback
}: {
  selectedDate: string;
  appointments: AppointmentRecord[];
  pendingRequests: BookingAttempt[];
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
  timezone?: string;
  onEdit: (appointment: AppointmentRecord) => void;
  onReschedule: (appointment: AppointmentRecord) => void;
  onCancel: (appointment: AppointmentRecord) => void;
  onReviewFallback: (request: BookingAttempt) => void;
}) {
  const items = [
    ...appointments.map((item) => ({
      id: `appointment-${item.id}`,
      appointment: item,
      request: null,
      start: item.start_time,
      end: item.end_time,
      title: item.customer_name,
      subtitle: bookingSummaryLabel(item, serviceNames, staffNames),
      status: item.status,
      authority: item.scheduling_authority,
      detail: appointmentOperationalDetail(item)
    })),
    ...pendingRequests.map((item) => ({
      id: `pending-${item.id}`,
      appointment: null,
      request: item,
      start: item.requested_start_time,
      end: item.requested_end_time,
      title: item.customer_name,
      subtitle: bookingSummaryLabel(item, serviceNames, staffNames),
      status: item.status,
      authority: item.scheduling_authority,
      detail: item.error_code || "Pending owner review"
    }))
  ].sort((a, b) => new Date(a.start).getTime() - new Date(b.start).getTime());

  if (items.length === 0) {
    return (
      <EmptyState
        icon={<CalendarClock className="h-5 w-5 text-muted" />}
        title="No calendar items for this day"
        message="Confirmed appointments and pending requests for the selected date will appear here with their originating authority."
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
          <div className="flex flex-col gap-2 sm:items-end">
            <Badge value={item.status} />
            <Badge value={appointmentOriginBadge(item.authority)} />
            {item.appointment ? (
              <CalendarAppointmentActions
                appointment={item.appointment}
                disabled={false}
                onEdit={onEdit}
                onReschedule={onReschedule}
                onCancel={onCancel}
              />
            ) : item.request ? (
              <Button type="button" variant="secondary" className="h-9 px-3" onClick={() => onReviewFallback(item.request)}>
                Review
              </Button>
            ) : null}
          </div>
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
        <div className="text-sm font-semibold text-ink">Available Square Appointments slots</div>
        <div className="mt-1 text-xs leading-5 text-muted">
          These slots can be offered or owner-selected, but booking still requires provider confirmation
          {result.timezone ? ` (${result.timezone})` : ""}.
        </div>
        <div className="mt-1 text-xs leading-5 text-muted">
          Quote valid until {formatQuoteExpiry(result.expires_at, timezone)}.
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
  onReview,
  onReschedule,
  onCancel
}: {
  item: AppointmentRecord;
  serviceName: string;
  staffName: string;
  technicianPreference: string;
  timezone?: string;
  disabled: boolean;
  onReview: (appointment: AppointmentRecord) => void;
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
          ["Origin", appointmentOriginLabel(item)],
          ...(item.scheduling_authority === "manleai_calendar"
            ? [["Lifecycle version", String(item.authority_appointment_version ?? "-")] as [string, string]]
            : [])
        ]}
      />
      <SegmentAssignmentList record={item} />
      <div className="mt-4">
        <AppointmentActions
          appointment={item}
          disabled={disabled}
          onReview={onReview}
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
  timezone,
  disabled,
  onReview,
  onRetry
}: {
  item: BookingAttempt;
  serviceName: string;
  staffName: string;
  technicianPreference: string;
  timezone?: string;
  disabled: boolean;
  onReview: (request: BookingAttempt) => void;
  onRetry: (request: BookingAttempt) => void;
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
          ["Request type", fallbackActionLabel(item)],
          ["Services", serviceName],
          ["Technician preference", technicianPreference],
          ["Assigned technicians", staffName],
          ["Origin", bookingAttemptOriginLabel(item)],
          ["Failure", item.error_code || "POS error"]
        ]}
      />
      <SegmentAssignmentList record={item} />
      <div className="mt-3 text-sm leading-6 text-muted">
        {item.error_message || "Review POS logs for details."}
      </div>
      <div className="mt-4">
        <FallbackActions request={item} disabled={disabled} onReview={onReview} onRetry={onRetry} />
      </div>
    </div>
  );
}

function AppointmentReviewDialog({
  salonID,
  appointment,
  timezone,
  disabled,
  onReschedule,
  onCancel
}: {
  salonID: string;
  appointment: AppointmentRecord;
  timezone?: string;
  disabled: boolean;
  onReschedule: (appointment: AppointmentRecord) => void;
  onCancel: (appointment: AppointmentRecord) => void;
}) {
  const internalOrigin = appointment.scheduling_authority === "manleai_calendar";
  const rescheduleDisabled = disabled || !canRescheduleAppointment(appointment);
  const cancelDisabled = disabled || !canCancelAppointment(appointment);
  return (
    <div>
      <AppointmentActionSummary appointment={appointment} timezone={timezone} />
      <div className="mt-5 rounded-md border border-line bg-slate-50 p-4 text-sm leading-6 text-muted">
        {internalOrigin
          ? "Reschedule and cancellation follow this appointment's persisted ManleAI Calendar origin and version. Reschedule replaces the whole active party plan atomically; cancellation releases every active child together."
          : "Customer details and service history stay tied to the provider-confirmed appointment. Time changes and cancellations are applied only after its persisted provider confirms the update."}
      </div>
      <CustomerNotificationStatus salonID={salonID} appointmentID={appointment.id} customerPhone={appointment.customer_phone} />
      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:justify-end">
        <Button type="button" variant="secondary" onClick={() => onReschedule(appointment)} disabled={rescheduleDisabled}>
          Reschedule appointment
        </Button>
        <Button type="button" variant="danger" onClick={() => onCancel(appointment)} disabled={cancelDisabled}>
          Cancel appointment
        </Button>
      </div>
      {internalOrigin ? <div className="mt-3 text-xs leading-5 text-muted">The current salon authority does not reroute this historical lifecycle target.</div> : null}
    </div>
  );
}

function FallbackReviewDialog({
  request,
  timezone,
  disabled,
  onRetry,
  onReconcile
}: {
  request: BookingAttempt;
  timezone?: string;
  disabled: boolean;
  onRetry: (request: BookingAttempt) => void;
  onReconcile: (request: BookingAttempt) => void;
}) {
  const action = fallbackBookingAction(request);
  const retryDisabledReason = fallbackRetryDisabledReason(request);

  return (
    <div>
      <div className="rounded-md border border-line bg-slate-50 p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">{request.customer_name || "Unknown customer"}</div>
            <div className="mt-1 text-sm leading-6 text-muted">{request.customer_phone}</div>
          </div>
          <div className="flex flex-wrap gap-2">
            <Badge value={action} />
            <Badge value={request.status} />
          </div>
        </div>
        <InfoGrid
          items={[
            ["Requested", `${formatDate(request.requested_start_time, timezone)} ${formatTimeRange(request.requested_start_time, request.requested_end_time, timezone)}`],
            ["Request type", fallbackActionLabel(request)],
            ["Services", serviceNamesLabel(request)],
            ["Assigned technicians", assignedTechniciansLabel(request)],
            ["Technician preference", technicianPreferenceLabel(request)],
            ["POS error", request.error_code || "POS error"],
            ["Retry policy", retryPolicyLabel(request)],
            ["Reconciliation", reconciliationLabel(request)]
          ]}
        />
        <SegmentAssignmentList record={request} />
      </div>

      <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
        {request.error_message || "The external provider did not confirm this request. Owner review is required."}
      </div>

      {request.appointment && action !== "book" ? (
        <div className="mt-5">
          <div className="text-sm font-semibold text-ink">Current provider-confirmed appointment</div>
          <div className="mt-3">
            <AppointmentActionSummary appointment={request.appointment} timezone={timezone} />
          </div>
        </div>
      ) : null}

      {retryDisabledReason ? (
        <div className="mt-4 rounded-md border border-line bg-slate-50 p-4 text-sm leading-6 text-muted">
          {retryDisabledReason}
        </div>
      ) : null}

      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:justify-end">
        {request.reconciliation_status === "required" ? (
          <Button type="button" variant="secondary" onClick={() => onReconcile(request)}>
            Resolve provider result
          </Button>
        ) : null}
        <Button type="button" onClick={() => onRetry(request)} disabled={disabled}>
          <RefreshCcw className="h-4 w-4" />
          {fallbackRetryLabel(request)}
        </Button>
      </div>
    </div>
  );
}

type ReconciliationAction = "provider_attached" | "not_created" | "escalated";

function ReconciliationTaskCard({
  task,
  attempt,
  timezone,
  disabled,
  onReview
}: {
  task: BookingReconciliationTask;
  attempt: BookingAttempt;
  timezone?: string;
  disabled: boolean;
  onReview: (task: BookingReconciliationTask) => void;
}) {
  return (
    <div className="rounded-md border border-amber-200 bg-amber-50 p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">{attempt.customer_name || "Unknown customer"}</div>
          <div className="mt-1 text-xs text-muted">{attempt.customer_phone || "No customer phone"}</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge value={attempt.operation_type || fallbackBookingAction(attempt)} />
          <Badge value={task.status === "escalated" ? "escalated" : "needs_review"} />
        </div>
      </div>
      <div className="mt-3 text-sm font-medium text-ink">
        {formatDate(attempt.requested_start_time, timezone)} {formatTimeRange(attempt.requested_start_time, attempt.requested_end_time, timezone)}
      </div>
      <div className="mt-2 text-sm leading-6 text-amber-900">
        {attempt.error_message || "The provider result is not safe to retry or confirm without reconciliation."}
      </div>
      <Button type="button" className="mt-4" variant="secondary" onClick={() => onReview(task)} disabled={disabled}>
        Review provider result
      </Button>
    </div>
  );
}

function ReconciliationDialog({
  task,
  attempt,
  candidates,
  candidatesLoading,
  selectedCandidateID,
  note,
  notCreatedConfirmed,
  timezone,
  saving,
  error,
  onCandidateChange,
  onNoteChange,
  onNotCreatedConfirmedChange,
  onResolve
}: {
  task: BookingReconciliationTask;
  attempt: BookingAttempt;
  candidates: BookingReconciliationCandidate[];
  candidatesLoading: boolean;
  selectedCandidateID: string;
  note: string;
  notCreatedConfirmed: boolean;
  timezone?: string;
  saving: boolean;
  error: string;
  onCandidateChange: (value: string) => void;
  onNoteChange: (value: string) => void;
  onNotCreatedConfirmedChange: (value: boolean) => void;
  onResolve: (action: ReconciliationAction) => void;
}) {
  const operation = attempt.operation_type || fallbackBookingAction(attempt);
  const providerMutationVisible = candidates.length > 0;
  const notCreatedBlocked =
    attempt.status === "provider_pending" ||
    (operation === "book" && Boolean(attempt.pos_booking_id)) ||
    providerMutationVisible;
  const notCreatedBlockedReason =
    attempt.status === "provider_pending"
      ? " The provider result is still pending and must be reconciled first."
      : operation === "book" && attempt.pos_booking_id
        ? " A provider booking ID already exists, so the booking must be attached or escalated."
        : providerMutationVisible
          ? " The latest provider-synced version shows that this booking or appointment action exists."
          : "";
  return (
    <div>
      <div className="rounded-md border border-line bg-slate-50 p-4">
        <div className="flex flex-wrap gap-2">
          <Badge value={attempt.operation_type || fallbackBookingAction(attempt)} />
          <Badge value={attempt.provider_outcome} />
          <Badge value={task.status} />
        </div>
        <InfoGrid
          items={[
            ["Customer", attempt.customer_name || "Unknown customer"],
            ["Phone", attempt.customer_phone || "Unavailable"],
            ["Requested", `${formatDate(attempt.requested_start_time, timezone)} ${formatTimeRange(attempt.requested_start_time, attempt.requested_end_time, timezone)}`],
            ["Provider", attempt.pos_provider || attempt.authority_provider || "External provider"],
            ["Provider booking ID", attempt.pos_booking_id || "Not returned"],
            ["Retry policy", retryPolicyLabel(attempt)]
          ]}
        />
      </div>

      <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900">
        Check the persisted provider first. A request is confirmed only by linking the exact appointment already imported by provider calendar sync.
      </div>

      <label className="mt-4 block text-sm font-medium text-ink">
        Matching provider-synced appointment
        <select
          className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
          value={selectedCandidateID}
          onChange={(event) => onCandidateChange(event.target.value)}
          disabled={saving || candidatesLoading || candidates.length === 0}
        >
          <option value="">Select a verified provider appointment</option>
          {candidates.map((candidate) => (
            <option key={candidate.appointment_id} value={candidate.appointment_id}>
              {formatDate(candidate.start_time, timezone)} {formatTimeRange(candidate.start_time, candidate.end_time, timezone)} · {candidate.customer_name} · {candidate.provider_appointment_id}
            </option>
          ))}
        </select>
      </label>
      {candidatesLoading ? (
        <div className="mt-2 text-xs leading-5 text-muted">Loading exact provider-synced matches...</div>
      ) : candidates.length === 0 ? (
        <div className="mt-2 text-xs leading-5 text-muted">
          No exact provider-synced match is loaded. Sync the persisted external-provider calendar and refresh before attaching a booking.
        </div>
      ) : null}

      <label className="mt-4 block text-sm font-medium text-ink">
        Resolution note
        <textarea
          className="mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink"
          value={note}
          maxLength={2000}
          onChange={(event) => onNoteChange(event.target.value)}
          placeholder="What was verified with the provider?"
          disabled={saving}
        />
      </label>

      <label className="mt-4 flex items-start gap-3 rounded-md border border-line bg-slate-50 p-3 text-sm leading-6 text-ink">
        <input
          className="mt-1 h-4 w-4"
          type="checkbox"
          checked={notCreatedConfirmed}
          onChange={(event) => onNotCreatedConfirmedChange(event.target.checked)}
          disabled={saving || candidatesLoading || notCreatedBlocked}
        />
        <span>
          I checked the persisted provider and verified that this action did not create or change a booking.
          {notCreatedBlockedReason}
        </span>
      </label>

      {error ? <div className="mt-4"><Alert title="Reconciliation failed" message={error} /></div> : null}

      <div className="mt-5 grid gap-3 sm:grid-cols-3">
        <Button
          type="button"
          onClick={() => onResolve("provider_attached")}
          disabled={saving || !selectedCandidateID}
        >
          {saving ? "Saving..." : "Attach verified booking"}
        </Button>
        <Button
          type="button"
          variant="secondary"
          onClick={() => onResolve("not_created")}
          disabled={saving || candidatesLoading || notCreatedBlocked || !notCreatedConfirmed}
        >
          {operation === "book" ? "Mark not created" : "Mark action not applied"}
        </Button>
        <Button type="button" variant="secondary" onClick={() => onResolve("escalated")} disabled={saving}>
          Escalate review
        </Button>
      </div>
    </div>
  );
}

function FallbackActions({
  request,
  disabled,
  onReview,
  onRetry
}: {
  request: BookingAttempt;
  disabled: boolean;
  onReview: (request: BookingAttempt) => void;
  onRetry: (request: BookingAttempt) => void;
}) {
  const retryDisabled = disabled || !canRetryFallbackRequest(request);

  return (
    <div className="flex flex-col gap-2 sm:flex-row">
      <Button type="button" variant="secondary" onClick={() => onReview(request)} disabled={disabled}>
        Review
      </Button>
      <Button type="button" onClick={() => onRetry(request)} disabled={retryDisabled}>
        <RefreshCcw className="h-4 w-4" />
        {fallbackRetryLabel(request)}
      </Button>
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

function formatQuoteExpiry(value: string | undefined, timezone?: string) {
  if (!value) return "an unknown time";
  return new Date(value).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
    second: "2-digit",
    timeZone: timezone
  });
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

function addDaysInput(value: string, days: number) {
  const [year, month, day] = value.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day + days));
  return `${date.getUTCFullYear()}-${String(date.getUTCMonth() + 1).padStart(2, "0")}-${String(
    date.getUTCDate()
  ).padStart(2, "0")}`;
}

function clearAvailabilityExpiryTimer(ref: { current: ReturnType<typeof setTimeout> | null }) {
  if (ref.current) {
    clearTimeout(ref.current);
    ref.current = null;
  }
}

function scheduleAvailabilityExpiry(
  timerRef: { current: ReturnType<typeof setTimeout> | null },
  expiresAt: string | undefined,
  requestID: number,
  requestIDRef: { current: number },
  onExpire: () => void
) {
  clearAvailabilityExpiryTimer(timerRef);
  if (!expiresAt) return;
  const expiresAtMs = new Date(expiresAt).getTime();
  const delay = expiresAtMs - Date.now();
  if (!Number.isFinite(expiresAtMs) || delay <= 0) {
    if (requestID === requestIDRef.current) onExpire();
    return;
  }
  timerRef.current = setTimeout(() => {
    if (requestID !== requestIDRef.current) return;
    requestIDRef.current += 1;
    timerRef.current = null;
    onExpire();
  }, Math.min(delay, 2_147_483_647));
}

function assertAvailabilityQuoteUsable(result: AvailabilityResult, slot: AvailabilitySlot) {
  if (!result.quote_id || !result.request_fingerprint || !result.expires_at || !slot.fingerprint) {
    throw new Error("Square Appointments availability did not return a verifiable quote. Check availability again.");
  }
  const expiresAt = new Date(result.expires_at).getTime();
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
    throw new Error("This availability quote expired. Check Square Appointments availability again before submitting.");
  }
}

function availabilityQuoteIsUsable(result: AvailabilityResult | null) {
  if (!result?.quote_id || !result.request_fingerprint || !result.expires_at) return false;
  const expiresAt = new Date(result.expires_at).getTime();
  return Number.isFinite(expiresAt) && expiresAt > Date.now();
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

function firstBookableServiceID(services: POSService[], activeProvider?: string) {
  return services.find((service) => serviceIsBookable(service, activeProvider))?.id ?? "";
}

function serviceIsBookable(service: POSService, activeProvider?: string) {
  return (
    Boolean(service.id) &&
    service.active &&
    !service.archived_at &&
    service.ai_bookable &&
    Boolean(activeProvider) &&
    service.pos_provider === activeProvider &&
    service.sync_status === "synced" &&
    service.pos_linked &&
    service.duration_minutes > 0
  );
}

function staffIsBookable(member: POSStaffMember, activeProvider?: string) {
  return (
    Boolean(member.id) &&
    member.active &&
    !member.archived_at &&
    member.ai_bookable &&
    Boolean(activeProvider) &&
    member.pos_provider === activeProvider &&
    member.sync_status === "synced" &&
    member.pos_linked
  );
}

function bookingPathReady(status: StatusResponse | null) {
  const connection = status?.connection;
  const readiness = status?.readiness;
  const connected = Boolean(connection?.id) && connection?.status === "active" && Boolean(connection?.last_sync_at);
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
    cancelReason: "",
    preservedSegments: [],
    retryOfAttemptID: "",
    retryRequestedStartTime: "",
    retryRequestedEndTime: ""
  };
}

function actionAvailabilitySegments(
  mode: AppointmentActionMode,
  form: AppointmentActionForm,
  appointment: AppointmentRecord | null
): BookingSegmentRequest[] {
  if (mode === "create") {
    if (form.preservedSegments.length > 0) {
      return form.preservedSegments;
    }
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
  if (form.preservedSegments.length > 0) {
    return form.preservedSegments;
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
	requestedSegments: BookingSegmentRequest[]
): BookingSegmentRequest[] {
  const segments = slot.segments ?? [];
  if (segments.length > 0) {
	return segments.map((segment, index) => {
	  const requested = requestedSegments[index];
	  const staffID = segment.staff_id ?? "";
	  return {
		service_id: segment.service_id,
		staff_id: staffID,
		staff_selection_mode: staffMode(requested?.staff_selection_mode ?? segment.staff_selection_mode, staffID)
	  };
	});
  }
	if (requestedSegments.length !== 1) return [];
	const requested = requestedSegments[0];
	const staffID = slot.staff_id ?? "";
  return [
    {
	  service_id: requested.service_id,
	  staff_id: staffID,
	  staff_selection_mode: staffMode(requested.staff_selection_mode ?? slot.staff_selection_mode, staffID)
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

function aggregateStaffSelectionMode(segments: BookingSegmentRequest[]): StaffSelectionMode {
  return segments.some((segment) => segment.staff_selection_mode === "anyone") ? "anyone" : "specific";
}

function segmentServiceNames(segments: BookingSegmentRequest[], services: POSService[]) {
  const names = new Map(services.flatMap((service) => (service.id ? [[service.id, service.name] as const] : [])));
  const values = segments.map((segment) => names.get(segment.service_id) || "Unavailable service");
  return values.length > 0 ? values.join(" + ") : "Service details unavailable";
}

function segmentStaffNames(segments: BookingSegmentRequest[], staff: POSStaffMember[]) {
  const names = new Map(staff.flatMap((member) => (member.id ? [[member.id, member.name] as const] : [])));
  const values = segments.map((segment) =>
    segment.staff_selection_mode === "anyone"
      ? "Anyone available"
      : names.get(segment.staff_id ?? "") || "Assigned technician unavailable"
  );
  return values.length > 0 ? values.join(" + ") : "Technician details unavailable";
}

function bookingAttemptRetrySegments(request: BookingAttempt): BookingSegmentRequest[] {
  const segments = orderedSegments(request);
  if (segments.length > 0) {
    return segments.map((segment) => ({
      service_id: segment.service_id ?? request.service_id ?? "",
      staff_id: segment.staff_id ?? "",
      staff_selection_mode: staffMode(segment.staff_selection_mode, segment.staff_id ?? "")
    }));
  }
  return [
    {
      service_id: request.service_id ?? "",
      staff_id: request.staff_id ?? "",
      staff_selection_mode: staffMode(request.staff_selection_mode, request.staff_id ?? "")
    }
  ];
}

function canRescheduleAppointment(appointment: AppointmentRecord) {
  if (appointment.scheduling_authority === "manleai_calendar") {
    return hasCompleteInternalLifecyclePlan(appointment);
  }
  if (appointment.scheduling_authority !== "external_provider") return false;
  if (appointment.can_edit === false) return false;
  return hasExternalAppointmentConfirmation(appointment);
}

function canCancelAppointment(appointment: AppointmentRecord) {
  if (appointment.scheduling_authority === "manleai_calendar") {
    return hasCompleteInternalLifecyclePlan(appointment);
  }
  if (appointment.scheduling_authority !== "external_provider") return false;
  if (appointment.can_cancel === false || appointment.can_delete === false) return false;
  return hasExternalAppointmentConfirmation(appointment);
}

function isConfirmedAppointmentStatus(status: string) {
  return status === "confirmed" || status === "rescheduled";
}

function appointmentOperationalDetail(appointment: AppointmentRecord) {
  if (appointment.scheduling_authority === "manleai_calendar" && isConfirmedAppointmentStatus(appointment.status)) {
    return "Internally confirmed: the atomic calendar commit returned a durable appointment ID.";
  }
  if (appointment.scheduling_authority === "manleai_calendar" && appointment.status === "cancelled") {
    return "Internally cancelled: the durable root advanced version and has no active child plan.";
  }
  if (hasExternalAppointmentConfirmation(appointment)) {
    return "Provider-confirmed: the provider returned a booking ID.";
  }
  if (appointment.scheduling_authority === "owner_manual") {
    return "Owner request origin is request-only and must not be represented as a confirmed appointment.";
  }
  if (!appointment.scheduling_authority) {
    return "Scheduling origin is missing; lifecycle actions remain disabled.";
  }
  if (appointment.status === "provider_pending") {
    return "Provider pending: this is not a confirmed appointment yet.";
  }
  if (appointment.status === "declined") return "Provider declined this appointment.";
  if (appointment.status === "no_show") return "Provider marked this appointment as no-show.";
  if (appointment.status === "cancelled") return "Provider marked this appointment as cancelled.";
  return "Provider status is unknown; verify this appointment before acting.";
}

function appointmentOriginLabel(appointment: AppointmentRecord) {
  if (appointment.scheduling_authority === "manleai_calendar") return "ManleAI Calendar";
  if (appointment.scheduling_authority === "owner_manual") return "Owner request";
  if (appointment.scheduling_authority !== "external_provider") return "Origin missing";
  if (appointment.authority_provider === "square" || appointment.pos_provider === "square") return "Square Appointments";
  return appointment.authority_provider || appointment.pos_provider || "External provider";
}

function bookingAttemptOriginLabel(attempt: BookingAttempt) {
  if (attempt.scheduling_authority === "owner_manual") return "Owner request";
  if (attempt.scheduling_authority === "manleai_calendar") return "ManleAI Calendar";
  if (attempt.scheduling_authority !== "external_provider") return "Origin missing";
  if (attempt.authority_provider === "square" || attempt.pos_provider === "square") return "Square Appointments";
  return attempt.authority_provider || attempt.pos_provider || "External provider";
}

function appointmentOriginBadge(authority: SchedulingAuthority | undefined) {
  if (authority === "owner_manual") return "Owner request";
  if (authority === "manleai_calendar") return "ManleAI Calendar";
  if (authority === "external_provider") return "External provider";
  return "Origin missing";
}

function authorityWorkflowDescription(authority: SchedulingAuthority, activeProvider?: string) {
  if (authority === "owner_manual") {
    return "New scheduling work becomes a pending owner-review request. It is never confirmed automatically.";
  }
  if (authority === "manleai_calendar") {
    return "New scheduling work uses ManleAI Calendar and confirms only after an atomic internal commit returns a durable appointment ID.";
  }
  const provider = activeProvider === "square" ? "Square Appointments" : "the selected external-provider adapter";
  return `New scheduling work uses ${provider} and confirms only after the provider returns the required booking evidence.`;
}

function appointmentDetailItems(appointment: AppointmentRecord): [string, string][] {
  const items: [string, string][] = [
    ["Origin", appointmentOriginLabel(appointment)],
    ["Services", serviceNamesLabel(appointment)],
    ["Assigned technicians", assignedTechniciansLabel(appointment)],
    ["Technician preference", technicianPreferenceLabel(appointment)]
  ];
  if (appointment.scheduling_authority === "manleai_calendar") {
    items.push(["Internal appointment ID", appointment.authority_appointment_id || appointment.id]);
    items.push(["Version", String(appointment.authority_appointment_version ?? 1)]);
    return items;
  }
  if (appointment.scheduling_authority === "external_provider") {
    items.push(["Provider booking", appointment.authority_appointment_id || appointment.pos_appointment_id || "Not returned"]);
  }
  return items;
}

function isBookingAttempt(response: AppointmentRecord | BookingAttempt): response is BookingAttempt {
  return "requested_start_time" in response;
}

function actionDialogTitle(mode: AppointmentActionMode) {
  if (mode === "create") return "New booking";
  if (mode === "reschedule") return "Reschedule appointment";
  return "Cancel appointment";
}

function actionDialogDescription(mode: AppointmentActionMode) {
  if (mode === "cancel") {
    return "Cancellation is applied only after the appointment's persisted provider confirms it.";
  }
  return mode === "create"
    ? "Check Square Appointments availability, choose a returned slot, then submit the booking."
    : "Check availability through this appointment's persisted external-provider origin, then submit the change.";
}

function fallbackBookingAction(request: BookingAttempt): "book" | "reschedule" | "cancel" {
  if (request.booking_action === "reschedule" || request.notification_type === "reschedule_fallback_pending") {
    return "reschedule";
  }
  if (request.booking_action === "cancel" || request.notification_type === "cancel_fallback_pending") {
    return "cancel";
  }
  return "book";
}

function fallbackActionLabel(request: BookingAttempt) {
  const action = fallbackBookingAction(request);
  if (action === "reschedule") return "Reschedule";
  if (action === "cancel") return "Cancellation";
  return "New booking";
}

function fallbackRetryLabel(request: BookingAttempt) {
  const action = fallbackBookingAction(request);
  if (action === "reschedule") return "Retry reschedule";
  if (action === "cancel") return "Retry cancellation";
  return "Retry booking";
}

function canRetryFallbackRequest(request: BookingAttempt) {
  if (request.scheduling_authority !== "external_provider" || request.status !== "fallback_pending" ||
      request.retry_policy !== "safe" || !request.can_retry || request.superseded_at || request.superseded_by_attempt_id) {
    return false;
  }
  const action = fallbackBookingAction(request);
  if (action === "book") {
	const storedSegments = orderedSegments(request);
	const hasEveryService = storedSegments.length > 0
	  ? storedSegments.every((segment) => Boolean(segment.service_id))
	  : Boolean(request.service_id);
	return hasEveryService && Boolean(request.customer_name.trim() && request.customer_phone.trim());
  }
  if (!request.appointment) return false;
  return action === "reschedule"
    ? canRescheduleAppointment(request.appointment)
    : canCancelAppointment(request.appointment);
}

function isEligibleSafeExternalCreateRetry(request: BookingAttempt) {
  return fallbackBookingAction(request) === "book" && canRetryFallbackRequest(request);
}

function fallbackRetryDisabledReason(request: BookingAttempt) {
  if (request.scheduling_authority !== "external_provider") {
    return "Provider retry is disabled because this request does not have an external-provider origin.";
  }
  if (request.status !== "fallback_pending" || request.retry_policy !== "safe" || !request.can_retry) {
    return request.retry_blocked_reason || "This request does not have backend safe-retry evidence.";
  }
  if (request.superseded_at || request.superseded_by_attempt_id) {
    return "This request was superseded by a later attempt and cannot own another retry.";
  }
  if (request.retry_blocked_reason) {
    return request.retry_blocked_reason;
  }
  if (request.reconciliation_status === "required") {
    return "The provider may have completed this action. Reconcile it with provider-synced evidence before retrying.";
  }
  if (canRetryFallbackRequest(request)) {
    return "";
  }
  const action = fallbackBookingAction(request);
  if (action === "book") {
    return "This pending booking is missing customer or service details needed to retry through its external-provider origin.";
  }
  return "This pending appointment action is missing the target provider-confirmed appointment, so retry is gated until the appointment context is available.";
}

function retryPolicyLabel(request: BookingAttempt) {
  if (request.retry_policy === "safe") return "Retry allowed";
  if (request.retry_policy === "blocked") return "Retry blocked";
  return "No retry needed";
}

function reconciliationLabel(request: BookingAttempt) {
  if (request.reconciliation_status === "required") return "Provider review required";
  if (request.reconciliation_status === "resolved") return "Resolved";
  return "Not required";
}

function reconciliationAttemptForTask(
  task: BookingReconciliationTask | null,
  fallbackRequests: BookingAttempt[]
) {
  if (!task) return null;
  const attempt = fallbackRequests.find((item) => item.id === task.booking_attempt_id) ?? task.booking_attempt ?? null;
  return attempt?.scheduling_authority === "external_provider" ? attempt : null;
}

function retrySlotMatchesStoredRequest(form: AppointmentActionForm, slot: AvailabilitySlot) {
  if (!form.retryRequestedStartTime || !form.retryRequestedEndTime || form.preservedSegments.length === 0) {
    return false;
  }
  if (
    new Date(slot.start_time).getTime() !== new Date(form.retryRequestedStartTime).getTime() ||
    new Date(slot.end_time).getTime() !== new Date(form.retryRequestedEndTime).getTime()
  ) {
    return false;
  }
  const slotSegments = slotBookingSegments(slot, form.preservedSegments);
  return slotSegments.length === form.preservedSegments.length && slotSegments.every((segment, index) => {
    const stored = form.preservedSegments[index];
    return segment.service_id === stored.service_id &&
      (segment.staff_id ?? "") === (stored.staff_id ?? "") &&
      segment.staff_selection_mode === stored.staff_selection_mode;
  });
}

function operationKeyForPayload(
  ref: { current: { key: string; fingerprint: string } | null },
  payload: Record<string, unknown>
) {
  const fingerprint = JSON.stringify(payload);
  if (!ref.current || ref.current.fingerprint !== fingerprint) {
    ref.current = { key: crypto.randomUUID(), fingerprint };
  }
  return ref.current.key;
}

function bookingAttemptPrimaryServiceID(request: BookingAttempt) {
  return orderedSegments(request).find((segment) => segment.service_id)?.service_id ?? request.service_id ?? "";
}

function bookingAttemptRetryStaffID(request: BookingAttempt) {
  if (request.staff_selection_mode === "anyone") {
    return "";
  }
  return orderedSegments(request).find((segment) => segment.staff_id)?.staff_id ?? request.staff_id ?? "";
}

function slotKey(slot: AvailabilitySlot) {
  return slot.fingerprint || `${slot.start_time}-${slot.end_time}-${slot.staff_id || assignedTechniciansLabel(slot)}`;
}

const inputClassName =
  "mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";

const selectClassName =
  "mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";

const textareaClassName =
  "mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}
