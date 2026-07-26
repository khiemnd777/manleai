import assert from "node:assert/strict";
import test from "node:test";

import type { AppointmentRecord, BookingAttempt } from "../types/api";
import {
  hasExternalAppointmentConfirmation,
  hasExternalAttemptConfirmation,
  normalizeSchedulingAuthority
} from "./scheduling-evidence";

test("calendar mapper does not infer an external appointment from a POS ID", () => {
  const internal = appointment({
    scheduling_authority: "manleai_calendar",
    authority_appointment_id: "appointment-1",
    authority_appointment_version: 2,
    pos_provider: "square",
    pos_appointment_id: "legacy-pos-id"
  });
  assert.equal(hasExternalAppointmentConfirmation(internal), false);
});

test("historical external origin remains actionable after a salon switch", () => {
  const external = appointment({
    scheduling_authority: "external_provider",
    authority_provider: "square",
    authority_appointment_id: "provider-booking-1",
    authority_appointment_version: 7,
    pos_provider: undefined,
    pos_appointment_id: undefined
  });
  assert.equal(hasExternalAppointmentConfirmation(external), true);
});

test("unknown authority fails closed despite complete provider-shaped aliases", () => {
  const unknown = appointment({
    scheduling_authority: "future_authority" as AppointmentRecord["scheduling_authority"],
    authority_provider: "square",
    authority_appointment_id: "provider-booking-1",
    authority_appointment_version: 7,
    pos_appointment_id: "legacy-pos-id"
  });
  assert.equal(normalizeSchedulingAuthority(unknown.scheduling_authority), "unknown");
  assert.equal(hasExternalAppointmentConfirmation(unknown), false);
});

test("attempt confirmation uses canonical origin and evidence", () => {
  assert.equal(hasExternalAttemptConfirmation(attempt({ pos_booking_id: "legacy-only" })), false);
  assert.equal(hasExternalAttemptConfirmation(attempt({
    authority_provider: "square",
    authority_appointment_id: "provider-booking-1",
    authority_appointment_version: 0
  })), true);
});

function appointment(overrides: Partial<AppointmentRecord>): AppointmentRecord {
  return {
    id: "appointment-1",
    salon_id: "salon-1",
    booking_attempt_id: "attempt-1",
    scheduling_authority: "owner_manual",
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
    source: "owner_dashboard",
    status: "confirmed",
    scheduling_authority: "external_provider",
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
