import type { AppointmentRecord, BookingAttempt, SchedulingAuthority } from "../types/api";

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

function isConfirmedAppointmentStatus(status: string) {
  return status === "confirmed" || status === "rescheduled";
}

function hasText(value: string | null | undefined) {
  return typeof value === "string" && value.trim().length > 0;
}

function isNonNegativeVersion(value: number | null | undefined) {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}
