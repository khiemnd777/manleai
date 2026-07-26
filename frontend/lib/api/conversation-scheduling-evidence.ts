import type { ConversationSession, ConversationSchedulingResultEvidence } from "../../types/api";

export type CurrentSchedulingCompletion = "confirmed" | "rescheduled" | "cancelled";

export function currentSchedulingCompletion(session: ConversationSession): CurrentSchedulingCompletion | undefined {
  const evidence = session.scheduling_result_evidence;
  if (!isCompletedEvidence(evidence) || !evidence.is_current) return undefined;
  if (evidence.operation_type === "book" && evidence.result_status === "confirmed" &&
      evidence.current_status === "confirmed" && evidence.current_active_child_count >= evidence.root_count &&
      session.outcome === "booking_confirmed") {
    return "confirmed";
  }
  if (evidence.operation_type === "reschedule" && evidence.result_status === "rescheduled" &&
      evidence.current_status === "rescheduled" && evidence.current_active_child_count >= evidence.root_count &&
      session.outcome === "booking_rescheduled") {
    return "rescheduled";
  }
  if (evidence.operation_type === "cancel" && evidence.result_status === "cancelled" &&
      evidence.current_status === "cancelled" && evidence.current_active_child_count === 0 &&
      session.outcome === "booking_cancelled") {
    return "cancelled";
  }
  return undefined;
}

export function hasCurrentSessionConfirmation(session: ConversationSession) {
  return currentSchedulingCompletion(session) === "confirmed";
}

export function isHistoricalCompletedEvidence(
  evidence: ConversationSchedulingResultEvidence | undefined
): evidence is ConversationSchedulingResultEvidence & { kind: "completed_operation" } {
  return isCompletedEvidence(evidence) && !evidence.is_current;
}

function isCompletedEvidence(
  evidence: ConversationSchedulingResultEvidence | undefined
): evidence is ConversationSchedulingResultEvidence & { kind: "completed_operation" } {
  return Boolean(
    evidence?.complete &&
    evidence.kind === "completed_operation" &&
    Boolean(evidence.appointment_id?.trim()) &&
    Boolean(evidence.booking_attempt_id?.trim()) &&
    evidence.root_count >= 1 &&
    evidence.result_child_count >= evidence.root_count &&
    (evidence.scheduling_authority === "manleai_calendar" || evidence.scheduling_authority === "external_provider")
  );
}
