import assert from "node:assert/strict";
import test from "node:test";

import type { AppointmentRecord, BookingAttempt } from "../../types/api";
import {
  hasExternalAppointmentConfirmation,
  hasExternalAttemptConfirmation,
  hasInternalAppointmentConfirmation,
  normalizeSchedulingAuthority,
  serviceEligibleForAuthority,
  staffEligibleForAuthority
} from "./scheduling-evidence";

test("legacy POS IDs never select external authority", () => {
  const internal = appointment({
    scheduling_authority: "manleai_calendar",
    authority_appointment_id: "appointment-1",
    authority_appointment_version: 2,
    pos_provider: "square",
    pos_appointment_id: "legacy-pos-id"
  });
  assert.equal(hasInternalAppointmentConfirmation(internal), true);
  assert.equal(hasExternalAppointmentConfirmation(internal), false);
});

test("external confirmation uses authority-native evidence without a legacy alias", () => {
  const external = appointment({
    scheduling_authority: "external_provider",
    authority_provider: "square",
    authority_appointment_id: "provider-booking-1",
    authority_appointment_version: 0,
    pos_provider: undefined,
    pos_appointment_id: undefined
  });
  assert.equal(hasExternalAppointmentConfirmation(external), true);
});

test("owner and unknown origins fail closed despite provider-shaped fields", () => {
  for (const authority of ["owner_manual", "future_authority"] as const) {
    const value = appointment({
      scheduling_authority: authority as AppointmentRecord["scheduling_authority"],
      authority_provider: "square",
      authority_appointment_id: "provider-booking-1",
      authority_appointment_version: 9,
      pos_appointment_id: "legacy-pos-id"
    });
    assert.equal(hasExternalAppointmentConfirmation(value), false);
    assert.equal(hasInternalAppointmentConfirmation(value), false);
  }
  assert.equal(normalizeSchedulingAuthority("future_authority"), "unknown");
});

test("external attempt confirmation ignores pos_booking_id as a discriminator", () => {
  const canonical = attempt({
    scheduling_authority: "external_provider",
    authority_provider: "square",
    authority_appointment_id: "provider-booking-1",
    authority_appointment_version: 3,
    pos_booking_id: undefined
  });
  const misleading = attempt({
    scheduling_authority: "manleai_calendar",
    authority_provider: undefined,
    authority_appointment_id: "internal-1",
    authority_appointment_version: 1,
    pos_booking_id: "legacy-pos-id"
  });
  assert.equal(hasExternalAttemptConfirmation(canonical), true);
  assert.equal(hasExternalAttemptConfirmation(misleading), false);
});

test("canonical AI-bookable records do not require a POS link for owner or internal authority", () => {
  const service = {
    id: "service-1", pos_provider: "", pos_service_id: "", name: "Manicure",
    duration_minutes: 45, ai_bookable: true, active: true, sync_status: "local_only",
    source: "manleai", pos_linked: false, category_source: "owner"
  };
  const staff = {
    id: "staff-1", pos_provider: "", pos_staff_id: "", name: "Technician",
    ai_bookable: true, active: true, sync_status: "local_only", source: "manleai", pos_linked: false
  };
  for (const authority of ["owner_manual", "manleai_calendar"] as const) {
    assert.equal(serviceEligibleForAuthority(service, authority, "square"), true);
    assert.equal(staffEligibleForAuthority(staff, authority, "square"), true);
  }
  assert.equal(serviceEligibleForAuthority(service, "external_provider", "square"), false);
  assert.equal(staffEligibleForAuthority(staff, "external_provider", "square"), false);
  assert.equal(serviceEligibleForAuthority({ ...service, pos_provider: "square", pos_service_id: "provider-service", pos_service_version: 2, sync_status: "synced", pos_linked: true }, "external_provider", "square"), true);
  assert.equal(staffEligibleForAuthority({ ...staff, pos_provider: "square", pos_staff_id: "provider-staff", sync_status: "synced", pos_linked: true }, "external_provider", "square"), true);
  assert.equal(serviceEligibleForAuthority(service, "future_authority", "square"), false);
});

function appointment(overrides: Partial<AppointmentRecord>): AppointmentRecord {
  return {
    id: "appointment-1",
    salon_id: "salon-1",
    booking_attempt_id: "attempt-1",
    scheduling_authority: "owner_manual",
    authority_appointment_id: "",
    status: "confirmed",
    customer_name: "Customer",
    customer_phone: "+13125550100",
    staff_selection_mode: "anyone",
    start_time: "2026-07-24T10:00:00Z",
    end_time: "2026-07-24T11:00:00Z",
    created_at: "2026-07-24T09:00:00Z",
    updated_at: "2026-07-24T09:00:00Z",
    ...overrides
  };
}

function attempt(overrides: Partial<BookingAttempt>): BookingAttempt {
  return {
    id: "attempt-1",
    salon_id: "salon-1",
    scheduling_authority: "external_provider",
    source: "owner_dashboard",
    status: "confirmed",
    operation_type: "book",
    provider_outcome: "succeeded",
    retry_policy: "none",
    reconciliation_status: "not_required",
    customer_name: "Customer",
    customer_phone: "+13125550100",
    staff_selection_mode: "anyone",
    requested_start_time: "2026-07-24T10:00:00Z",
    requested_end_time: "2026-07-24T11:00:00Z",
    can_retry: false,
    created_at: "2026-07-24T09:00:00Z",
    updated_at: "2026-07-24T09:00:00Z",
    ...overrides
  };
}
