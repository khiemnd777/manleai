import type { AppointmentRecord, BookingAttempt, CalendarView, SchedulingAuthority } from "../../types/api";

export type CalendarTechnician = {
  id: string;
  name: string;
};

export type CalendarItem = {
  id: string;
  sourceItemID?: string;
  kind: "appointment" | "pending";
  start: string;
  end: string;
  status: string;
  authority: SchedulingAuthority | "unknown";
  customerName: string;
  serviceLabel: string;
  technicians: CalendarTechnician[];
  technicianLabel: string;
  dayLaneKey?: string;
  subtitle: string;
  detail: string;
  warning: string;
  appointment?: AppointmentRecord;
  request?: BookingAttempt;
};

export type CalendarDayGroup = {
  date: string;
  items: CalendarItem[];
  appointmentCount: number;
  pendingCount: number;
  warningCount: number;
};

export type MobileMonthDay = CalendarDayGroup & {
  inCurrentMonth: boolean;
};

export type MobileCalendarActionPolicy = {
  showAdd: boolean;
  addEnabled: boolean;
  showSync: boolean;
};

export type SchedulingAuthorityPresentation = {
  tone: "success" | "warning";
  compactLabel: string;
  title: string;
  message: string;
};

export function defaultCalendarView(viewport: "mobile" | "desktop"): CalendarView {
  return viewport === "mobile" ? "agenda" : "week";
}

export function mobileCalendarActionPolicy(
  authority: SchedulingAuthority | undefined,
  readyForNewExternalBooking: boolean
): MobileCalendarActionPolicy {
  const externalSelected = authority === "external_provider";
  return {
    showAdd: externalSelected,
    addEnabled: externalSelected && readyForNewExternalBooking,
    showSync: externalSelected
  };
}

export function schedulingAuthorityPresentation(
  authority: SchedulingAuthority | undefined,
  version: number | undefined,
  providerLabel: string,
  readyForNewExternalBooking: boolean
): SchedulingAuthorityPresentation {
  if (!authority || !version) {
    return {
      tone: "warning",
      compactLabel: "Scheduling authority unavailable",
      title: "Scheduling authority unavailable",
      message: "The current authority and version are required before this calendar can expose new scheduling actions. Existing rows remain visible, but any row without persisted origin evidence stays read-only."
    };
  }
  if (authority === "owner_manual") {
    return {
      tone: "success",
      compactLabel: "Owner requests · Request only",
      title: `Owner request authority · v${version}`,
      message: "New scheduling work creates requests for owner review and never confirms automatically. Review and resolve those requests in the main Appointments workspace."
    };
  }
  if (authority === "manleai_calendar") {
    return {
      tone: "success",
      compactLabel: "ManleAI Calendar · Read only here",
      title: `ManleAI Calendar authority · v${version}`,
      message: "New work uses the internal calendar. This standalone view shows mixed-origin history; use the main Appointments workspace for safe internal create, reschedule, and cancel actions."
    };
  }
  return {
    tone: readyForNewExternalBooking ? "success" : "warning",
    compactLabel: `${providerLabel} · ${readyForNewExternalBooking ? "Ready" : "Blocked"}`,
    title: `${providerLabel} authority · v${version}`,
    message: readyForNewExternalBooking
      ? "New bookings use Square Appointments. Historical lifecycle actions continue to follow each appointment's persisted origin."
      : "New Square Appointments bookings are blocked until connection, location, services, and staff are ready. Historical rows remain visible and actions are resolved from persisted origin."
  };
}

export function groupCalendarItemsByDay(items: CalendarItem[], timezone?: string): CalendarDayGroup[] {
  const grouped = new Map<string, CalendarItem[]>();
  for (const item of [...items].sort(compareCalendarItems)) {
    const day = calendarDateKey(item.start, timezone);
    const current = grouped.get(day) ?? [];
    current.push(item);
    grouped.set(day, current);
  }
  return [...grouped.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([date, dayItems]) => calendarDayGroup(date, dayItems));
}

export function mobileWeekDays(anchorDate: string, items: CalendarItem[], timezone?: string): CalendarDayGroup[] {
  const start = startOfWeekInput(anchorDate);
  return Array.from({ length: 7 }, (_, index) => {
    const date = addDaysInput(start, index);
    return calendarDayGroup(
      date,
      items.filter((item) => calendarDateKey(item.start, timezone) === date).sort(compareCalendarItems)
    );
  });
}

export function mobileMonthDays(anchorDate: string, items: CalendarItem[], timezone?: string): MobileMonthDay[] {
  const month = anchorDate.slice(0, 7);
  return monthGridDays(anchorDate).map((date) => ({
    ...calendarDayGroup(
      date,
      items.filter((item) => calendarDateKey(item.start, timezone) === date).sort(compareCalendarItems)
    ),
    inCurrentMonth: date.slice(0, 7) === month
  }));
}

export function calendarDayGroup(date: string, items: CalendarItem[]): CalendarDayGroup {
  return {
    date,
    items,
    appointmentCount: items.filter((item) => item.kind === "appointment").length,
    pendingCount: items.filter((item) => item.kind === "pending").length,
    warningCount: items.filter((item) => Boolean(item.warning)).length
  };
}

export function addDaysInput(value: string, days: number) {
  const date = inputDateToLocalDate(value);
  date.setDate(date.getDate() + days);
  return formatDateInput(date);
}

export function startOfWeekInput(value: string) {
  const date = inputDateToLocalDate(value);
  date.setDate(date.getDate() - date.getDay());
  return formatDateInput(date);
}

export function monthGridDays(anchorDate: string) {
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

export function inputDateToLocalDate(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return new Date(year, month - 1, day);
}

export function formatDateInput(date: Date, timezone?: string) {
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

export function calendarDateKey(value: string, timezone?: string) {
  return formatDateInput(new Date(value), timezone);
}

export function formatInputDateLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric"
  });
}

export function formatFullInputDateLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
    year: "numeric"
  });
}

export function formatMonthDayLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric"
  });
}

export function weekdayLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, { weekday: "short" });
}

export function dayNumberLabel(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, { day: "numeric" });
}

export function monthTitle(value: string) {
  return inputDateToLocalDate(value).toLocaleDateString(undefined, { month: "long", year: "numeric" });
}

export function isTodayInput(value: string, timezone?: string) {
  return value === formatDateInput(new Date(), timezone);
}

function compareCalendarItems(left: CalendarItem, right: CalendarItem) {
  return new Date(left.start).getTime() - new Date(right.start).getTime() || left.id.localeCompare(right.id);
}
