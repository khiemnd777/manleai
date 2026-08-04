import assert from "node:assert/strict";
import test from "node:test";

import {
  defaultCalendarView,
  groupCalendarItemsByDay,
  mobileCalendarActionPolicy,
  mobileMonthDays,
  mobileWeekDays,
  schedulingAuthorityPresentation
} from "./calendar-view-model";
import type { CalendarItem } from "./calendar-view-model";

function calendarItem(overrides: Partial<CalendarItem> & Pick<CalendarItem, "id" | "start">): CalendarItem {
  return {
    kind: "appointment",
    status: overrides.status ?? "confirmed",
    authority: overrides.authority ?? "external_provider",
    customerName: overrides.customerName ?? "Jordan Lee",
    serviceLabel: overrides.serviceLabel ?? "Signature Service",
    technicians: overrides.technicians ?? [{ id: "staff-1", name: "Taylor" }],
    technicianLabel: overrides.technicianLabel ?? "Taylor",
    subtitle: overrides.subtitle ?? "Signature Service · Taylor",
    detail: overrides.detail ?? "External appointment",
    warning: overrides.warning ?? "",
    ...overrides,
    id: overrides.id,
    start: overrides.start,
    end: overrides.end ?? overrides.start
  };
}

test("mobile and desktop use the approved distinct initial views", () => {
  assert.equal(defaultCalendarView("mobile"), "agenda");
  assert.equal(defaultCalendarView("desktop"), "week");
});

test("agenda grouping follows the salon timezone and keeps stable chronological order", () => {
  const groups = groupCalendarItemsByDay(
    [
      calendarItem({ id: "later", start: "2026-08-05T06:30:00Z" }),
      calendarItem({ id: "earlier", start: "2026-08-05T03:30:00Z", kind: "pending" }),
      calendarItem({ id: "warning", start: "2026-08-05T07:30:00Z", warning: "Staff mapping needs review" })
    ],
    "America/Los_Angeles"
  );

  assert.deepEqual(groups.map((group) => group.date), ["2026-08-04", "2026-08-05"]);
  assert.deepEqual(groups[0].items.map((item) => item.id), ["earlier", "later"]);
  assert.equal(groups[0].pendingCount, 1);
  assert.equal(groups[1].warningCount, 1);
});

test("week presentation always returns seven days without changing the requested range", () => {
  const days = mobileWeekDays(
    "2026-08-05",
    [calendarItem({ id: "sunday", start: "2026-08-02T16:00:00Z" }), calendarItem({ id: "saturday", start: "2026-08-08T16:00:00Z" })],
    "UTC"
  );

  assert.equal(days.length, 7);
  assert.equal(days[0].date, "2026-08-02");
  assert.equal(days[6].date, "2026-08-08");
  assert.equal(days[0].appointmentCount, 1);
  assert.equal(days[6].appointmentCount, 1);
});

test("month presentation includes adjacent dates and preserves status counts", () => {
  const days = mobileMonthDays(
    "2026-08-04",
    [
      calendarItem({ id: "month-warning", start: "2026-08-04T15:00:00Z", warning: "Calendar sync warning" }),
      calendarItem({ id: "next-month", start: "2026-09-01T15:00:00Z", kind: "pending" })
    ],
    "UTC"
  );

  assert.equal(days.length, 42);
  assert.equal(days[0].date, "2026-07-26");
  assert.equal(days.find((day) => day.date === "2026-08-04")?.warningCount, 1);
  assert.equal(days.find((day) => day.date === "2026-09-01")?.inCurrentMonth, false);
});

test("mobile external actions remain gated by selected authority and backend readiness", () => {
  assert.deepEqual(mobileCalendarActionPolicy("external_provider", true), {
    showAdd: true,
    addEnabled: true,
    showSync: true
  });
  assert.deepEqual(mobileCalendarActionPolicy("external_provider", false), {
    showAdd: true,
    addEnabled: false,
    showSync: true
  });
  assert.deepEqual(mobileCalendarActionPolicy("owner_manual", true), {
    showAdd: false,
    addEnabled: false,
    showSync: false
  });
  assert.deepEqual(mobileCalendarActionPolicy("manleai_calendar", true), {
    showAdd: false,
    addEnabled: false,
    showSync: false
  });
  assert.deepEqual(mobileCalendarActionPolicy(undefined, true), {
    showAdd: false,
    addEnabled: false,
    showSync: false
  });
});

test("authority presentation stays source-specific without inferring Square for internal modes", () => {
  assert.equal(schedulingAuthorityPresentation("external_provider", 7, "Square Appointments", true).compactLabel, "Square Appointments · Ready");
  assert.equal(schedulingAuthorityPresentation("external_provider", 7, "Square Appointments", false).tone, "warning");
  assert.equal(schedulingAuthorityPresentation("owner_manual", 3, "Square Appointments", true).compactLabel, "Owner requests · Request only");
  assert.equal(schedulingAuthorityPresentation("manleai_calendar", 4, "Square Appointments", true).compactLabel, "ManleAI Calendar · Read only here");
  assert.equal(schedulingAuthorityPresentation(undefined, undefined, "Square Appointments", true).tone, "warning");
});
