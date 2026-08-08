import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const staff = readFileSync("features/business/business-staff.tsx", "utf8");
const staffCalendar = readFileSync("features/dashboard/staff-calendar-profile.tsx", "utf8");
const services = readFileSync("features/dashboard/services-dashboard.tsx", "utf8");
const setup = readFileSync("features/platform/platform-internal-calendar-settings.tsx", "utf8");
const authority = readFileSync("features/platform/platform-scheduling-authority-control.tsx", "utf8");
const api = readFileSync("lib/api/internal-calendar.ts", "utf8");

for (const required of [
  "surface=\"platform\"",
  "manageServiceEligibility={false}",
  "canonicalEligibleServiceIDs={editing.service_ids}",
  "StaffCalendarProfile",
  "getManleAICalendar(surface.salonID, \"platform\")"
]) assert.match(staff, new RegExp(required.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));

assert.match(staffCalendar, /updateManleAICalendarStaffProfile[\s\S]*}, surface\)/);
assert.match(staffCalendar, /createManleAICalendarException[\s\S]*}, surface\)/);
assert.match(staffCalendar, /cancelManleAICalendarException[\s\S]*}, surface\)/);
assert.match(staffCalendar, /does not create a second eligibility source/);

assert.match(staff, /useSearchParams/);
assert.match(services, /useSearchParams/);
assert.match(setup, /Production setup checklist/);
assert.match(setup, /initialCalendar=\{calendar\}/);
assert.match(setup, /onCalendarChange=\{setCalendar\}/);
assert.match(authority, /calendarBlockerPresentation/);
assert.doesNotMatch(setup, /<InternalCalendarSetup[^>]*\/>[\s\S]*<InternalCalendarSetup/);

for (const apiFunction of [
  "updateManleAICalendarConfig",
  "replaceManleAICalendarHours",
  "updateManleAICalendarStaffProfile",
  "updateManleAICalendarServicePolicy",
  "createManleAICalendarResource",
  "archiveManleAICalendarResource",
  "createManleAICalendarException",
  "cancelManleAICalendarException",
  "activateManleAICalendar"
]) assert.match(api, new RegExp(`export function ${apiFunction}`));
