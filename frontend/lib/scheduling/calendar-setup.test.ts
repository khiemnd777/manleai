import assert from "node:assert/strict";
import {
  actionableCalendarBlockers,
  calendarBlockerPresentation,
  calendarSetupLinks,
  calendarSetupProgress
} from "./calendar-setup";
import type { ManleAICalendarAggregate } from "../../types/api";

const calendar = {
  salon_id: "salon-1",
  config_version: 7,
  config: { version: 7, activated_at: "2026-08-08T00:00:00Z", activated_version: 7 },
  hours: [{ id: "hours-1" }],
  service_policies: [
    {
      configured: true,
      enabled: true,
      service: { id: "service-1", name: "Classic Manicure", active: true, ai_bookable: true, archived_at: null, duration_minutes: 30 },
      eligible_staff: [{ id: "staff-1", name: "Alex", active: true, ai_bookable: true, archived_at: null }]
    },
    {
      configured: false,
      enabled: false,
      service: { id: "service-2", name: "Gel Pedicure", active: true, ai_bookable: true, archived_at: null, duration_minutes: 45 },
      eligible_staff: []
    }
  ],
  staff_profiles: [
    { staff: { id: "staff-1", name: "Alex", active: true, ai_bookable: true, archived_at: null }, weekly_periods: [{ id: "period-1" }], eligible_services: [] }
  ]
} as unknown as ManleAICalendarAggregate;

assert.deepEqual(calendarSetupLinks("platform", "salon / one"), {
  calendar: "/platform/tenants/salon%20%2F%20one/scheduling/calendar",
  staff: "/platform/tenants/salon%20%2F%20one/staff",
  services: "/platform/tenants/salon%20%2F%20one/services"
});

assert.deepEqual(calendarSetupProgress(calendar), {
  policyReady: true,
  hoursReady: true,
  configuredServiceCount: 1,
  eligibleServiceCount: 2,
  enabledServiceCount: 1,
  scheduledStaffCount: 1,
  requiredStaffCount: 1,
  staffReady: true,
  activationCurrent: true
});

const serviceBlocker = { code: "SERVICE_POLICY_REQUIRED", scope: "service", entity_id: "service-2", message: "fallback" };
assert.deepEqual(calendarBlockerPresentation(serviceBlocker, calendar, "platform", "salon-1"), {
  label: "Gel Pedicure",
  message: "Choose explicitly whether this service is enabled for ManleAI Calendar.",
  href: "/platform/tenants/salon-1/services?edit=service-2",
  action: "Open service"
});

assert.deepEqual(
  actionableCalendarBlockers([
    { code: "CONFIGURATION_READY", message: "generic" },
    serviceBlocker
  ]),
  [serviceBlocker]
);
