"use client";

import { useCallback, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  AlertTriangle,
  CalendarCheck2,
  CalendarClock,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  Ellipsis,
  LogOut,
  Plus,
  RefreshCcw,
  ShieldCheck,
  X
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils/cn";
import type { CalendarView, SchedulingAuthority } from "@/types/api";
import {
  addDaysInput,
  calendarDateKey,
  dayNumberLabel,
  formatFullInputDateLabel,
  groupCalendarItemsByDay,
  isTodayInput,
  mobileCalendarActionPolicy,
  mobileMonthDays,
  mobileWeekDays,
  monthTitle,
  weekdayLabel
} from "./calendar-view-model";
import type { CalendarItem, SchedulingAuthorityPresentation } from "./calendar-view-model";

type Notice = {
  type: "success" | "warning" | "error";
  title: string;
  message: string;
};

type MobilePOSCalendarProps = {
  view: CalendarView;
  anchorDate: string;
  focusedDate: string;
  rangeLabel: string;
  timezone?: string;
  authority?: SchedulingAuthority;
  authorityVersion?: number;
  authorityPresentation: SchedulingAuthorityPresentation;
  readyForNewExternalBooking: boolean;
  bookableStaffCount: number;
  bookableServiceCount: number;
  items: CalendarItem[];
  selectedItemID: string;
  loadingCalendar: boolean;
  busy: string;
  calendarError: string;
  statusError: string;
  notice: Notice | null;
  dayView: ReactNode;
  onViewChange: (view: CalendarView) => void;
  onMoveRange: (direction: -1 | 1) => void;
  onShortcut: (offsetDays: number) => void;
  onFocusedDateChange: (date: string) => void;
  onSelect: (itemID: string) => void;
  onOpenCreate: () => void;
  onSync: () => void;
  onSignOut: () => void;
  onRetry: () => void;
  onDismissNotice: () => void;
};

type SheetMode = "actions" | "status" | null;

export function MobilePOSCalendar({
  view,
  anchorDate,
  focusedDate,
  rangeLabel,
  timezone,
  authority,
  authorityVersion,
  authorityPresentation,
  readyForNewExternalBooking,
  bookableStaffCount,
  bookableServiceCount,
  items,
  selectedItemID,
  loadingCalendar,
  busy,
  calendarError,
  statusError,
  notice,
  dayView,
  onViewChange,
  onMoveRange,
  onShortcut,
  onFocusedDateChange,
  onSelect,
  onOpenCreate,
  onSync,
  onSignOut,
  onRetry,
  onDismissNotice
}: MobilePOSCalendarProps) {
  const [sheetMode, setSheetMode] = useState<SheetMode>(null);
  const actionPolicy = mobileCalendarActionPolicy(authority, readyForNewExternalBooking);
  const appointments = items.filter((item) => item.kind === "appointment").length;
  const pending = items.filter((item) => item.kind === "pending").length;
  const warnings = items.filter((item) => Boolean(item.warning)).length;
  const agendaGroups = useMemo(() => groupCalendarItemsByDay(items, timezone), [items, timezone]);
  const weekDays = useMemo(() => mobileWeekDays(anchorDate, items, timezone), [anchorDate, items, timezone]);
  const monthDays = useMemo(() => mobileMonthDays(anchorDate, items, timezone), [anchorDate, items, timezone]);
  const focusedItems = useMemo(
    () => items.filter((item) => calendarDateKey(item.start, timezone) === focusedDate),
    [focusedDate, items, timezone]
  );
  const closeSheet = useCallback(() => setSheetMode(null), []);

  function changeView(nextView: CalendarView) {
    if (nextView === "day") onFocusedDateChange(focusedDate || anchorDate);
    onViewChange(nextView);
  }

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border border-line bg-panel shadow-soft">
      <header className="flex min-h-14 shrink-0 items-center justify-between gap-3 border-b border-line px-3 py-2">
        <button type="button" className="min-w-0 text-left" onClick={() => setSheetMode("status")}>
          <span className="flex items-center gap-2 text-sm font-bold text-ink">
            <CalendarDays className="h-4 w-4 flex-none text-brand" />
            <span className="truncate">Scheduling Calendar</span>
          </span>
          <span className="mt-1 flex items-center gap-1.5 truncate text-[11px] font-semibold text-muted">
            <span
              className={cn(
                "h-1.5 w-1.5 flex-none rounded-full",
                authorityPresentation.tone === "success" ? "bg-emerald-500" : "bg-amber-500"
              )}
            />
            {authorityPresentation.compactLabel}
          </span>
        </button>
        <div className="flex flex-none items-center gap-1.5">
          {actionPolicy.showAdd ? (
            <Button
              type="button"
              className="h-11 w-11 px-0"
              onClick={onOpenCreate}
              disabled={!actionPolicy.addEnabled}
              aria-label="Add via Square Appointments"
            >
              <Plus className="h-4 w-4" />
            </Button>
          ) : null}
          <Button
            type="button"
            variant="secondary"
            className="h-11 w-11 px-0"
            onClick={() => setSheetMode("actions")}
            aria-label="Open calendar actions"
            aria-expanded={sheetMode === "actions"}
          >
            <Ellipsis className="h-4 w-4" />
          </Button>
        </div>
      </header>

      <div className="grid shrink-0 grid-cols-[2.75rem_minmax(0,1fr)_2.75rem_auto] items-center gap-1 border-b border-line px-2 py-1.5">
        <Button type="button" variant="ghost" className="h-11 px-0" onClick={() => onMoveRange(-1)} aria-label="Previous range">
          <ChevronLeft className="h-4 w-4" />
        </Button>
        <div className="truncate px-1 text-center text-xs font-bold text-ink">{rangeLabel}</div>
        <Button type="button" variant="ghost" className="h-11 px-0" onClick={() => onMoveRange(1)} aria-label="Next range">
          <ChevronRight className="h-4 w-4" />
        </Button>
        <label className="sr-only" htmlFor="mobile-calendar-view">
          Calendar view
        </label>
        <select
          id="mobile-calendar-view"
          value={view}
          onChange={(event) => changeView(event.target.value as CalendarView)}
          className="h-11 rounded-md border border-line bg-slate-50 px-2 text-xs font-semibold text-ink outline-none focus:border-brand"
        >
          <option value="day">Day</option>
          <option value="week">Week</option>
          <option value="month">Month</option>
          <option value="agenda">Agenda</option>
        </select>
      </div>

      <div className="flex min-h-8 shrink-0 items-center gap-3 overflow-hidden border-b border-line bg-slate-50 px-3 py-1.5 text-[11px] font-semibold text-muted">
        <span className="whitespace-nowrap"><strong className="mr-1 text-ink">{appointments}</strong>appointments</span>
        <span className="whitespace-nowrap"><strong className="mr-1 text-ink">{pending}</strong>pending</span>
        <span className={cn("whitespace-nowrap", warnings > 0 ? "text-amber-700" : "")}>
          <strong className={cn("mr-1", warnings > 0 ? "text-amber-700" : "text-ink")}>{warnings}</strong>warning{warnings === 1 ? "" : "s"}
        </span>
      </div>

      {calendarError ? (
        <div className="flex shrink-0 items-center gap-2 border-b border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800">
          <AlertTriangle className="h-4 w-4 flex-none" />
          <span className="min-w-0 flex-1 truncate">{calendarError}</span>
          <Button type="button" variant="ghost" className="h-11 px-2 text-xs text-red-800" onClick={onRetry}>
            Retry
          </Button>
        </div>
      ) : statusError ? (
        <button
          type="button"
          className="flex min-h-10 shrink-0 items-center gap-2 border-b border-amber-200 bg-amber-50 px-3 py-2 text-left text-xs text-amber-900"
          onClick={() => setSheetMode("status")}
        >
          <AlertTriangle className="h-4 w-4 flex-none" />
          <span className="min-w-0 flex-1 truncate">Square Appointments status unavailable</span>
          <span className="font-semibold">Details</span>
        </button>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {loadingCalendar ? (
          <div className="flex min-h-0 flex-1 flex-col gap-2 p-2">
            <Skeleton className="h-12" />
            <Skeleton className="h-16" />
            <Skeleton className="h-16" />
            <Skeleton className="min-h-20 flex-1" />
          </div>
        ) : view === "day" ? (
          <div className="flex min-h-0 flex-1 p-2">{dayView}</div>
        ) : view === "week" ? (
          <MobileWeekView
            days={weekDays}
            focusedDate={focusedDate}
            timezone={timezone}
            selectedItemID={selectedItemID}
            onFocusedDateChange={onFocusedDateChange}
            onSelect={onSelect}
          />
        ) : view === "month" ? (
          <MobileMonthView
            days={monthDays}
            anchorDate={anchorDate}
            focusedDate={focusedDate}
            focusedItems={focusedItems}
            timezone={timezone}
            selectedItemID={selectedItemID}
            onFocusedDateChange={onFocusedDateChange}
            onSelect={onSelect}
          />
        ) : (
          <MobileAgendaView
            groups={agendaGroups}
            timezone={timezone}
            selectedItemID={selectedItemID}
            onSelect={onSelect}
          />
        )}
      </div>

      {notice ? (
        <div
          className={cn(
            "absolute inset-x-2 top-2 z-50 flex items-start gap-2 rounded-md border p-3 text-xs shadow-xl",
            notice.type === "success"
              ? "border-emerald-200 bg-emerald-50 text-emerald-900"
              : notice.type === "warning"
                ? "border-amber-200 bg-amber-50 text-amber-900"
                : "border-red-200 bg-red-50 text-red-900"
          )}
        >
          <div className="min-w-0 flex-1">
            <div className="font-bold">{notice.title}</div>
            <div className="mt-1 line-clamp-2 leading-5">{notice.message}</div>
          </div>
          <Button type="button" variant="ghost" className="h-11 w-11 flex-none px-0" onClick={onDismissNotice} aria-label="Dismiss notice">
            <X className="h-4 w-4" />
          </Button>
        </div>
      ) : null}

      <Sheet
        open={sheetMode === "actions"}
        title="Calendar actions"
        description="Secondary controls stay available without reducing calendar space."
        onClose={closeSheet}
        closeDisabled={busy === "logout"}
      >
        <div className="divide-y divide-line">
          {actionPolicy.showSync ? (
            <SheetAction
              icon={<RefreshCcw className={cn("h-4 w-4", busy === "sync" ? "animate-spin" : "")} />}
              title={busy === "sync" ? "Syncing Square Appointments..." : "Sync Square Appointments"}
              description="Refresh the currently visible calendar range."
              disabled={busy === "sync" || Boolean(statusError)}
              onClick={() => {
                setSheetMode(null);
                onSync();
              }}
            />
          ) : null}
          <SheetAction
            icon={<CalendarCheck2 className="h-4 w-4" />}
            title="Go to Today"
            description="Jump to the current salon date."
            onClick={() => {
              setSheetMode(null);
              onShortcut(0);
            }}
          />
          <SheetAction
            icon={<CalendarClock className="h-4 w-4" />}
            title="Go to Tomorrow"
            description="Open the next salon date."
            onClick={() => {
              setSheetMode(null);
              onShortcut(1);
            }}
          />
          <SheetAction
            icon={<ShieldCheck className="h-4 w-4" />}
            title="Scheduling authority details"
            description={authorityPresentation.compactLabel}
            onClick={() => setSheetMode("status")}
          />
          <SheetAction
            icon={<LogOut className="h-4 w-4" />}
            title={busy === "logout" ? "Signing out..." : "Sign out"}
            description="End this calendar session."
            destructive
            disabled={busy === "logout"}
            onClick={onSignOut}
          />
        </div>
      </Sheet>

      <Sheet
        open={sheetMode === "status"}
        title={authorityPresentation.title}
        description={authorityPresentation.message}
        onClose={closeSheet}
      >
        <dl className="grid grid-cols-2 gap-2 text-sm">
          <StatusField label="Authority" value={authority ? authority.replaceAll("_", " ") : "Unavailable"} />
          <StatusField label="Version" value={authorityVersion ? String(authorityVersion) : "Unavailable"} />
          <StatusField label="Bookable staff" value={String(bookableStaffCount)} />
          <StatusField label="Bookable services" value={String(bookableServiceCount)} />
        </dl>
        {statusError ? (
          <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-xs leading-5 text-amber-900">
            {statusError}
          </div>
        ) : null}
      </Sheet>
    </div>
  );
}

function MobileWeekView({
  days,
  focusedDate,
  timezone,
  selectedItemID,
  onFocusedDateChange,
  onSelect
}: {
  days: ReturnType<typeof mobileWeekDays>;
  focusedDate: string;
  timezone?: string;
  selectedItemID: string;
  onFocusedDateChange: (date: string) => void;
  onSelect: (itemID: string) => void;
}) {
  const selected = days.find((day) => day.date === focusedDate) ?? days[0];
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <div className="grid shrink-0 grid-cols-7 gap-1 border-b border-line bg-white p-2">
        {days.map((day) => (
          <button
            key={day.date}
            type="button"
            className={cn(
              "flex min-h-14 min-w-0 flex-col items-center justify-center rounded-md px-1 text-xs outline-none focus:ring-2 focus:ring-brand",
              day.date === selected.date ? "bg-brand text-white" : "bg-slate-50 text-ink"
            )}
            onClick={() => onFocusedDateChange(day.date)}
            aria-label={`${formatFullInputDateLabel(day.date)}, ${day.items.length} calendar items`}
          >
            <span className={cn("text-[10px] font-semibold uppercase", day.date === selected.date ? "text-white/80" : "text-muted")}>
              {weekdayLabel(day.date)}
            </span>
            <strong className="mt-1 text-sm">{dayNumberLabel(day.date)}</strong>
            <span className={cn("mt-1 text-[10px]", day.date === selected.date ? "text-white/80" : "text-muted")}>{day.items.length}</span>
          </button>
        ))}
      </div>
      <MobileDayList
        date={selected.date}
        items={selected.items}
        timezone={timezone}
        selectedItemID={selectedItemID}
        onSelect={onSelect}
      />
    </div>
  );
}

function MobileMonthView({
  days,
  anchorDate,
  focusedDate,
  focusedItems,
  timezone,
  selectedItemID,
  onFocusedDateChange,
  onSelect
}: {
  days: ReturnType<typeof mobileMonthDays>;
  anchorDate: string;
  focusedDate: string;
  focusedItems: CalendarItem[];
  timezone?: string;
  selectedItemID: string;
  onFocusedDateChange: (date: string) => void;
  onSelect: (itemID: string) => void;
}) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="bg-white">
        <div className="grid grid-cols-7 border-b border-line px-2 py-2 text-center text-[10px] font-semibold uppercase text-muted">
          {["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"].map((label) => <span key={label}>{label.slice(0, 1)}</span>)}
        </div>
        <div className="grid grid-cols-7 gap-1 border-b border-line p-2">
          {days.map((day) => (
            <button
              key={day.date}
              type="button"
              className={cn(
                "relative flex min-h-12 min-w-0 flex-col items-center justify-center rounded-md text-xs outline-none focus:ring-2 focus:ring-brand",
                day.date === focusedDate ? "bg-brand text-white" : day.inCurrentMonth ? "text-ink hover:bg-slate-50" : "text-slate-400"
              )}
              onClick={() => onFocusedDateChange(day.date)}
              aria-label={`${formatFullInputDateLabel(day.date)}, ${day.items.length} calendar items`}
            >
              <strong>{dayNumberLabel(day.date)}</strong>
              {day.items.length > 0 ? (
                <span className={cn("mt-1 flex items-center gap-1 text-[9px]", day.date === focusedDate ? "text-white/85" : "text-muted")}>
                  {day.items.length}
                  {day.warningCount > 0 ? <AlertTriangle className="h-2.5 w-2.5 text-amber-500" /> : null}
                  {day.pendingCount > 0 ? <span className="h-1.5 w-1.5 rounded-full bg-amber-500" /> : null}
                </span>
              ) : null}
            </button>
          ))}
        </div>
      </div>
      <div className="border-b border-line bg-slate-50 px-3 py-2 text-xs font-semibold text-muted">
        {monthTitle(anchorDate)} · selected {formatFullInputDateLabel(focusedDate)}
      </div>
      <MobileDayList
        date={focusedDate}
        items={focusedItems}
        timezone={timezone}
        selectedItemID={selectedItemID}
        onSelect={onSelect}
        showHeading={false}
      />
    </div>
  );
}

function MobileAgendaView({
  groups,
  timezone,
  selectedItemID,
  onSelect
}: {
  groups: ReturnType<typeof groupCalendarItemsByDay>;
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
}) {
  if (groups.length === 0) {
    return <MobileEmptyState title="No calendar items" message="Appointments and pending requests for this range will appear here." />;
  }
  return (
    <div className="min-h-0 flex-1 overflow-y-auto bg-shell">
      {groups.map((group) => (
        <div key={group.date}>
          <div className="sticky top-0 z-10 flex items-center justify-between gap-2 border-y border-line bg-slate-50/95 px-3 py-2 text-[11px] font-bold uppercase text-ink backdrop-blur">
            <span>{dayHeading(group.date, timezone)}</span>
            <span className="font-semibold normal-case text-muted">{group.items.length} items</span>
          </div>
          <MobileAgendaRows items={group.items} timezone={timezone} selectedItemID={selectedItemID} onSelect={onSelect} />
        </div>
      ))}
    </div>
  );
}

function MobileDayList({
  date,
  items,
  timezone,
  selectedItemID,
  onSelect,
  showHeading = true
}: {
  date: string;
  items: CalendarItem[];
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
  showHeading?: boolean;
}) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto bg-shell">
      {showHeading ? (
        <div className="sticky top-0 z-10 flex items-center justify-between gap-2 border-b border-line bg-slate-50/95 px-3 py-2 text-[11px] font-bold uppercase text-ink backdrop-blur">
          <span>{dayHeading(date, timezone)}</span>
          <span className="font-semibold normal-case text-muted">{items.length} items</span>
        </div>
      ) : null}
      {items.length === 0 ? (
        <MobileEmptyState title="No appointments" message="No appointments or pending requests are scheduled for this day." />
      ) : (
        <MobileAgendaRows items={items} timezone={timezone} selectedItemID={selectedItemID} onSelect={onSelect} />
      )}
    </div>
  );
}

function MobileAgendaRows({
  items,
  timezone,
  selectedItemID,
  onSelect
}: {
  items: CalendarItem[];
  timezone?: string;
  selectedItemID: string;
  onSelect: (itemID: string) => void;
}) {
  return (
    <div className="divide-y divide-line border-b border-line bg-white">
      {items.map((item) => {
        const state = item.warning ? "Warning" : item.kind === "pending" ? "Pending" : humanizeStatus(item.status);
        return (
          <button
            key={item.id}
            type="button"
            className={cn(
              "grid min-h-[4.5rem] w-full grid-cols-[3.25rem_minmax(0,1fr)_auto_1rem] items-center gap-2 px-3 py-2 text-left outline-none focus:ring-2 focus:ring-inset focus:ring-brand",
              item.warning ? "bg-amber-50" : item.kind === "pending" ? "bg-amber-50/40" : "bg-white",
              item.id === selectedItemID ? "ring-2 ring-inset ring-brand" : ""
            )}
            onClick={() => onSelect(item.id)}
          >
            <span className="min-w-0 text-xs font-bold text-ink">
              <span className="block">{formatCalendarTime(item.start, timezone)}</span>
              <span className="mt-1 block text-[10px] font-semibold text-muted">{formatCalendarTime(item.end, timezone)}</span>
            </span>
            <span className="min-w-0">
              <span className="block truncate text-sm font-bold text-ink">{item.customerName || "Unknown customer"}</span>
              <span className="mt-1 block truncate text-xs text-muted">{item.serviceLabel || "Unknown service"} · {item.technicianLabel}</span>
            </span>
            <span className={cn("flex items-center gap-1 text-[10px] font-semibold", item.warning || item.kind === "pending" ? "text-amber-700" : "text-emerald-700")}>
              {item.warning ? <AlertTriangle className="h-3 w-3" /> : <span className={cn("h-1.5 w-1.5 rounded-full", item.kind === "pending" ? "bg-amber-500" : "bg-emerald-500")} />}
              <span className="hidden min-[360px]:inline">{state}</span>
            </span>
            <ChevronRight className="h-4 w-4 text-slate-400" />
          </button>
        );
      })}
    </div>
  );
}

function SheetAction({
  icon,
  title,
  description,
  onClick,
  disabled = false,
  destructive = false
}: {
  icon: ReactNode;
  title: string;
  description: string;
  onClick: () => void;
  disabled?: boolean;
  destructive?: boolean;
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex min-h-16 w-full items-center gap-3 py-3 text-left disabled:cursor-not-allowed disabled:opacity-50",
        destructive ? "text-accent" : "text-ink"
      )}
      onClick={onClick}
      disabled={disabled}
    >
      <span className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-slate-100">{icon}</span>
      <span className="min-w-0">
        <span className="block text-sm font-bold">{title}</span>
        <span className="mt-1 block text-xs leading-5 text-muted">{description}</span>
      </span>
    </button>
  );
}

function StatusField({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-line bg-slate-50 p-3">
      <dt className="text-[10px] font-semibold uppercase tracking-wide text-muted">{label}</dt>
      <dd className="mt-1 break-words font-semibold text-ink">{value}</dd>
    </div>
  );
}

function MobileEmptyState({ title, message }: { title: string; message: string }) {
  return (
    <div className="flex min-h-52 flex-col items-center justify-center p-6 text-center">
      <CalendarClock className="h-6 w-6 text-muted" />
      <div className="mt-3 text-sm font-bold text-ink">{title}</div>
      <div className="mt-1 max-w-xs text-xs leading-5 text-muted">{message}</div>
    </div>
  );
}

function dayHeading(date: string, timezone?: string) {
  const today = isTodayInput(date, timezone);
  const tomorrow = date === addDaysInput(calendarDateKey(new Date().toISOString(), timezone), 1);
  const prefix = today ? "Today · " : tomorrow ? "Tomorrow · " : "";
  return `${prefix}${formatFullInputDateLabel(date)}`;
}

function formatCalendarTime(value: string, timezone?: string) {
  if (!value) return "--";
  return new Date(value).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timezone
  });
}

function humanizeStatus(value: string) {
  return value.replaceAll("_", " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}
