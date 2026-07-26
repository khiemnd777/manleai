import assert from "node:assert/strict";
import test from "node:test";

import type { ConversationSession, ConversationSchedulingResultEvidence } from "../../types/api";
import {
  currentSchedulingCompletion,
  hasCurrentSessionConfirmation,
  isHistoricalCompletedEvidence
} from "./conversation-scheduling-evidence";

test("local appointment and attempt IDs never manufacture confirmation", () => {
  const session = fixture();
  assert.equal(hasCurrentSessionConfirmation(session), false);
  assert.equal(currentSchedulingCompletion(session), undefined);
});

test("backend-complete authority evidence drives a current confirmation", () => {
  const session = fixture({ scheduling_result_evidence: evidence() });
  assert.equal(hasCurrentSessionConfirmation(session), true);
  assert.equal(currentSchedulingCompletion(session), "confirmed");
});

test("historical success is not presented as a current confirmation", () => {
  const historical = evidence({
    current_status: "cancelled",
    current_authority_appointment_version: 3,
    current_active_child_count: 0,
    is_current: false
  });
  const session = fixture({ scheduling_result_evidence: historical });
  assert.equal(hasCurrentSessionConfirmation(session), false);
  assert.equal(currentSchedulingCompletion(session), undefined);
  assert.equal(isHistoricalCompletedEvidence(historical), true);
});

test("mixed or incomplete party state fails closed", () => {
  const session = fixture({
    scheduling_result_evidence: evidence({
      complete: false,
      current_status: "mixed",
      is_current: false,
      root_count: 2,
      result_child_count: 2,
      incomplete_reason: "party_result_invalid"
    })
  });
  assert.equal(hasCurrentSessionConfirmation(session), false);
});

test("complete flag without durable IDs or child counts still fails closed", () => {
  for (const malformed of [
    evidence({ appointment_id: undefined }),
    evidence({ booking_attempt_id: undefined }),
    evidence({ result_child_count: 0 }),
    evidence({ current_active_child_count: 0 })
  ]) {
    assert.equal(hasCurrentSessionConfirmation(fixture({ scheduling_result_evidence: malformed })), false);
  }
});

test("resolved or dismissed owner review evidence is always non-confirming", () => {
  for (const currentStatus of ["resolved", "dismissed"] as const) {
    const session = fixture({
      scheduling_result_evidence: evidence({
        kind: "pending_owner_review",
        scheduling_authority: "owner_manual",
        target_scheduling_authority: "external_provider",
        operation_type: "book",
        result_status: "owner_review_pending",
        current_status: currentStatus,
        is_current: true,
        appointment_id: undefined,
        booking_attempt_id: undefined,
        scheduling_request_id: "request-1",
        root_count: 0,
        result_child_count: 0,
        current_active_child_count: 0
      })
    });
    assert.equal(hasCurrentSessionConfirmation(session), false);
    assert.equal(currentSchedulingCompletion(session), undefined);
  }
});

function fixture(overrides: Partial<ConversationSession> = {}): ConversationSession {
  return {
    id: "session-1",
    salon_id: "salon-1",
    channel: "simulator",
    status: "completed",
    intent: "booking",
    outcome: "booking_confirmed",
    booking_action: "book",
    booking_attempt_id: "unverified-attempt",
    appointment_id: "unverified-appointment",
    dialog_state: {
      version: 6,
      phase: "review",
      review_required: true,
      review_accepted: true,
      no_progress_count: 0,
      draft_revision: 1,
      reviewed_revision: 1,
      authorized_revision: 1
    },
    lifecycle_status: "active",
    retention_expires_at: "2026-10-22T00:00:00Z",
    started_at: "2026-07-24T00:00:00Z",
    created_at: "2026-07-24T00:00:00Z",
    updated_at: "2026-07-24T00:00:00Z",
    ...overrides
  };
}

function evidence(overrides: Partial<ConversationSchedulingResultEvidence> = {}): ConversationSchedulingResultEvidence {
  return {
    complete: true,
    kind: "completed_operation",
    scheduling_authority: "external_provider",
    operation_type: "book",
    result_status: "confirmed",
    current_status: "confirmed",
    is_current: true,
    appointment_id: "appointment-1",
    booking_attempt_id: "attempt-1",
    authority_appointment_version: 2,
    current_authority_appointment_version: 2,
    root_count: 1,
    result_child_count: 1,
    current_active_child_count: 1,
    ...overrides
  };
}
