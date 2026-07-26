import type {
  AppointmentRecord,
  BookingAttempt,
  POSService,
  POSStaffMember,
  SchedulingAuthority
} from "../../types/api";

export type KnownSchedulingAuthority = SchedulingAuthority | "unknown";

export function normalizeSchedulingAuthority(value: unknown): KnownSchedulingAuthority {
  return value === "owner_manual" || value === "manleai_calendar" || value === "external_provider"
    ? value
    : "unknown";
}

export function hasExternalAppointmentConfirmation(appointment: AppointmentRecord) {
  return normalizeSchedulingAuthority(appointment.scheduling_authority) === "external_provider"
    && isConfirmedAppointmentStatus(appointment.status)
    && hasText(appointment.authority_provider)
    && hasText(appointment.authority_appointment_id)
    && isNonNegativeVersion(appointment.authority_appointment_version);
}

export function hasExternalAttemptConfirmation(attempt: BookingAttempt) {
  return normalizeSchedulingAuthority(attempt.scheduling_authority) === "external_provider"
    && attempt.status === "confirmed"
    && hasText(attempt.authority_provider)
    && hasText(attempt.authority_appointment_id)
    && isNonNegativeVersion(attempt.authority_appointment_version);
}

export function hasInternalAppointmentConfirmation(appointment: AppointmentRecord) {
  return normalizeSchedulingAuthority(appointment.scheduling_authority) === "manleai_calendar"
    && isConfirmedAppointmentStatus(appointment.status)
    && hasText(appointment.id)
    && appointment.authority_appointment_id === appointment.id
    && isPositiveVersion(appointment.authority_appointment_version);
}

export function serviceEligibleForAuthority(
  service: POSService,
  authorityValue: unknown,
  activeProvider?: string
) {
  const authority = normalizeSchedulingAuthority(authorityValue);
  const canonicalEligible = Boolean(service.id)
    && service.active
    && !service.archived_at
    && service.ai_bookable
    && service.duration_minutes > 0;
  if (!canonicalEligible || authority === "unknown") return false;
  if (authority !== "external_provider") return true;
  return hasText(activeProvider)
    && service.pos_provider === activeProvider
    && service.sync_status === "synced"
    && service.pos_linked
    && hasText(service.pos_service_id)
    && isPositiveVersion(service.pos_service_version);
}

export function staffEligibleForAuthority(
  member: POSStaffMember,
  authorityValue: unknown,
  activeProvider?: string
) {
  const authority = normalizeSchedulingAuthority(authorityValue);
  const canonicalEligible = Boolean(member.id)
    && member.active
    && !member.archived_at
    && member.ai_bookable;
  if (!canonicalEligible || authority === "unknown") return false;
  if (authority !== "external_provider") return true;
  return hasText(activeProvider)
    && member.pos_provider === activeProvider
    && member.sync_status === "synced"
    && member.pos_linked
    && hasText(member.pos_staff_id);
}

function isConfirmedAppointmentStatus(status: string) {
  return status === "confirmed" || status === "rescheduled";
}

function hasText(value: string | null | undefined) {
  return typeof value === "string" && value.trim().length > 0;
}

function isNonNegativeVersion(value: number | null | undefined) {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}

function isPositiveVersion(value: number | null | undefined) {
  return typeof value === "number" && Number.isInteger(value) && value > 0;
}
