import type {
  ManleAICalendarAggregate,
  ManleAICalendarReadinessBlocker,
  SchedulingAuthorityReadinessBlocker
} from "../../types/api";

export type CalendarSetupSurface = "tenant" | "platform";

type CalendarBlocker = ManleAICalendarReadinessBlocker | SchedulingAuthorityReadinessBlocker;

export type CalendarSetupProgress = {
  policyReady: boolean;
  hoursReady: boolean;
  configuredServiceCount: number;
  eligibleServiceCount: number;
  enabledServiceCount: number;
  scheduledStaffCount: number;
  requiredStaffCount: number;
  staffReady: boolean;
  activationCurrent: boolean;
};

export function calendarSetupLinks(surface: CalendarSetupSurface, salonID = "") {
  if (surface === "platform") {
    const base = `/platform/tenants/${encodeURIComponent(salonID)}`;
    return {
      calendar: `${base}/scheduling/calendar`,
      staff: `${base}/staff`,
      services: `${base}/services`
    };
  }
  return {
    calendar: "/dashboard/settings",
    staff: "/dashboard/staff",
    services: "/dashboard/services"
  };
}

export function calendarSetupProgress(calendar: ManleAICalendarAggregate): CalendarSetupProgress {
  const eligiblePolicies = calendar.service_policies.filter(({ service }) =>
    service.active && service.ai_bookable && !service.archived_at && service.duration_minutes > 0
  );
  const enabledPolicies = eligiblePolicies.filter((policy) => policy.configured && policy.enabled);
  const requiredStaffIDs = new Set(enabledPolicies.flatMap((policy) => policy.eligible_staff.map((staff) => staff.id)));
  const scheduledStaffCount = calendar.staff_profiles.filter(
    (profile) => requiredStaffIDs.has(profile.staff.id) && profile.weekly_periods.length > 0
  ).length;
  const activationCurrent = Boolean(
    calendar.config?.activated_at &&
    calendar.config.activated_version !== null &&
    calendar.config.activated_version === calendar.config.version
  );

  return {
    policyReady: Boolean(calendar.config),
    hoursReady: calendar.hours.length > 0,
    configuredServiceCount: eligiblePolicies.filter((policy) => policy.configured).length,
    eligibleServiceCount: eligiblePolicies.length,
    enabledServiceCount: enabledPolicies.length,
    scheduledStaffCount,
    requiredStaffCount: requiredStaffIDs.size,
    staffReady: requiredStaffIDs.size > 0 && scheduledStaffCount === requiredStaffIDs.size,
    activationCurrent
  };
}

export function calendarBlockerPresentation(
  blocker: CalendarBlocker,
  calendar: ManleAICalendarAggregate,
  surface: CalendarSetupSurface,
  salonID = ""
) {
  const links = calendarSetupLinks(surface, salonID);
  const service = blocker.scope === "service" && blocker.entity_id
    ? calendar.service_policies.find((item) => item.service.id === blocker.entity_id)?.service
    : undefined;
  const staff = blocker.scope === "staff" && blocker.entity_id
    ? calendar.staff_profiles.find((item) => item.staff.id === blocker.entity_id)?.staff
    : undefined;

  if (service) {
    return {
      label: service.name,
      message: serviceBlockerMessage(blocker.code, blocker.message),
      href: `${links.services}?edit=${encodeURIComponent(service.id)}`,
      action: "Open service"
    };
  }
  if (staff) {
    return {
      label: staff.name,
      message: staffBlockerMessage(blocker.code, blocker.message),
      href: `${links.staff}?edit=${encodeURIComponent(staff.id)}`,
      action: "Open staff"
    };
  }

  const target = calendarBlockerTarget(blocker.code, links.calendar);
  return {
    label: calendarBlockerLabel(blocker.code),
    message: blocker.message,
    href: target,
    action: "Open setup"
  };
}

export function actionableCalendarBlockers<T extends CalendarBlocker>(blockers: T[]) {
  const granular = blockers.some((blocker) =>
    !["CONFIGURATION_READY", "STAFF_ONLY_AVAILABILITY_READY", "STAFF_ONLY_CREATE_READY"].includes(blocker.code)
  );
  if (!granular) return blockers;
  return blockers.filter((blocker) =>
    !["CONFIGURATION_READY", "STAFF_ONLY_AVAILABILITY_READY", "STAFF_ONLY_CREATE_READY"].includes(blocker.code)
  );
}

function calendarBlockerTarget(code: string, calendarPath: string) {
  if (code === "CONFIG_REQUIRED") return `${calendarPath}#calendar-policy`;
  if (code === "LOCAL_HOURS_REQUIRED") return `${calendarPath}#calendar-hours`;
  if (code === "CONFIG_NOT_ACTIVATED") return `${calendarPath}#calendar-activation`;
  if (code.includes("RESOURCE") || code.includes("POOL")) return `${calendarPath}#calendar-resources`;
  return calendarPath;
}

function calendarBlockerLabel(code: string) {
  if (code === "CONFIG_REQUIRED") return "Scheduling policy";
  if (code === "LOCAL_HOURS_REQUIRED") return "Local salon hours";
  if (code === "ENABLED_SERVICE_REQUIRED") return "Enabled service";
  if (code === "CONFIG_NOT_ACTIVATED") return "Activation";
  return code.toLocaleLowerCase().replace(/_/g, " ").replace(/^./, (value) => value.toUpperCase());
}

function serviceBlockerMessage(code: string, fallback: string) {
  if (code === "SERVICE_POLICY_REQUIRED") return "Choose explicitly whether this service is enabled for ManleAI Calendar.";
  if (code === "SERVICE_STAFF_REQUIRED") return "Assign at least one eligible staff member.";
  if (code === "SERVICE_CAPACITY_MODE_REQUIRED") return "Choose staff-only or pooled capacity.";
  return fallback;
}

function staffBlockerMessage(code: string, fallback: string) {
  if (code === "STAFF_SCHEDULE_REQUIRED") return "Add at least one recurring weekly schedule period.";
  if (code === "STAFF_INELIGIBLE") return "Staff must be active, AI-bookable, and not archived.";
  return fallback;
}
