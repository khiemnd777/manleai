"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { AlertTriangle, MessageSquarePlus, MessageSquareText, PhoneCall, Plus, RefreshCcw, Send, X } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  assignedTechniciansLabel,
  conversationBookingRecord,
  orderedSegments,
  serviceNamesLabel,
  technicianPreferenceLabel,
  technicianPreferenceValue
} from "@/features/dashboard/booking-display";
import { apiRequest } from "@/lib/api/client";
import type {
  ConversationSession,
  OfferedSlot,
  OwnerCorrection,
  POSConnection,
  POSService,
  POSStaffMember,
  Salon,
  SquareReadiness,
  SyncLog,
  TranscriptMessage,
  VoiceStatus
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type SessionsResponse = {
  sessions: ConversationSession[];
};

type ServicesResponse = {
  services: POSService[];
};

type StaffResponse = {
  staff: POSStaffMember[];
};

type CorrectionTarget = {
  sessionID: string;
  item: TranscriptMessage;
};

export function CallsDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [voiceStatus, setVoiceStatus] = useState<VoiceStatus | null>(null);
  const [sessions, setSessions] = useState<ConversationSession[]>([]);
  const [selectedSession, setSelectedSession] = useState<ConversationSession | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [message, setMessage] = useState("");
  const [correctionTarget, setCorrectionTarget] = useState<CorrectionTarget | null>(null);
  const [correctionText, setCorrectionText] = useState("");
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [savingCorrection, setSavingCorrection] = useState(false);
  const [error, setError] = useState("");
  const [actionError, setActionError] = useState("");
  const [success, setSuccess] = useState("");

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setVoiceStatus(null);
        setSessions([]);
        setSelectedSession(null);
        setServices([]);
        setStaff([]);
        return;
      }

      const [statusResponse, voiceResponse, sessionsResponse, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<VoiceStatus>(`/api/salons/${firstSalon.id}/voice/status`),
        apiRequest<SessionsResponse>(`/api/salons/${firstSalon.id}/conversation-sessions?limit=25`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(statusResponse);
      setVoiceStatus(voiceResponse);
      setSessions(sessionsResponse.sessions);
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);

      const currentID = selectedSession?.id;
      const nextSummary =
        sessionsResponse.sessions.find((item) => item.id === currentID) ?? sessionsResponse.sessions[0] ?? null;
      if (nextSummary) {
        const detail = await apiRequest<ConversationSession>(
          `/api/salons/${firstSalon.id}/conversation-sessions/${nextSummary.id}`
        );
        setSelectedSession(detail);
      } else {
        setSelectedSession(null);
        clearCorrectionDraft();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load call sessions.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function startSession() {
    if (!salon) return null;
    setActionError("");
    setSuccess("");
    setSending(true);
    try {
      const session = await apiRequest<ConversationSession>(`/api/salons/${salon.id}/conversation-sessions`, {
        method: "POST",
        body: JSON.stringify({ channel: "simulator" })
      });
      setSelectedSession(session);
      await reloadSessions(salon.id);
      return session;
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not start simulator session.");
      return null;
    } finally {
      setSending(false);
    }
  }

  async function sendMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !message.trim()) return;

    setActionError("");
    setSuccess("");
    setSending(true);
    try {
      let session = selectedSession;
      if (!session || session.status !== "active" || session.channel !== "simulator") {
        session = await apiRequest<ConversationSession>(`/api/salons/${salon.id}/conversation-sessions`, {
          method: "POST",
          body: JSON.stringify({ channel: "simulator" })
        });
      }
      const updated = await apiRequest<ConversationSession>(
        `/api/salons/${salon.id}/conversation-sessions/${session.id}/messages`,
        {
          method: "POST",
          body: JSON.stringify({ message })
        }
      );
      setSelectedSession(updated);
      clearCorrectionDraft();
      setMessage("");
      await reloadSessions(salon.id);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not process simulator message.");
    } finally {
      setSending(false);
    }
  }

  async function selectSession(sessionID: string) {
    if (!salon) return;
    setActionError("");
    setSuccess("");
    try {
      const detail = await apiRequest<ConversationSession>(
        `/api/salons/${salon.id}/conversation-sessions/${sessionID}`
      );
      setSelectedSession(detail);
      clearCorrectionDraft();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not load transcript.");
    }
  }

  async function reloadSessions(salonID: string) {
    const response = await apiRequest<SessionsResponse>(`/api/salons/${salonID}/conversation-sessions?limit=25`);
    setSessions(response.sessions);
  }

  async function saveCorrection(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !selectedSession || !correctionTarget || !correctionText.trim()) return;

    setActionError("");
    setSuccess("");
    setSavingCorrection(true);
    try {
      await apiRequest<OwnerCorrection>(`/api/salons/${salon.id}/owner-corrections`, {
        method: "POST",
        body: JSON.stringify({
          call_session_id: correctionTarget.sessionID,
          transcript_message_id: correctionTarget.item.id,
          correction: correctionText
        })
      });
      setSuccess("Correction captured for AI Training.");
      clearCorrectionDraft();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not save owner correction.");
    } finally {
      setSavingCorrection(false);
    }
  }

  function startCorrection(item: TranscriptMessage) {
    if (!selectedSession) return;
    setCorrectionTarget({ sessionID: selectedSession.id, item });
    setCorrectionText("");
    setActionError("");
    setSuccess("");
  }

  function clearCorrectionDraft() {
    setCorrectionTarget(null);
    setCorrectionText("");
  }

  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
  const phoneCount = useMemo(() => sessions.filter((item) => item.channel === "phone").length, [sessions]);
  const simulatorCount = useMemo(() => sessions.filter((item) => item.channel === "simulator").length, [sessions]);
  const handoffCount = useMemo(
    () => sessions.filter((item) => item.status === "handoff" || item.outcome === "handoff_requested").length,
    [sessions]
  );
  const confirmedCount = useMemo(
    () => sessions.filter((item) => item.outcome === "booking_confirmed").length,
    [sessions]
  );
  const fallbackCount = useMemo(
    () => sessions.filter((item) => item.outcome === "booking_fallback_pending" || item.outcome === "ai_disabled").length,
    [sessions]
  );
  const serviceNames = useMemo(
    () => new Map(services.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [services]
  );
  const staffNames = useMemo(
    () => new Map(staff.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [staff]
  );
  const selectedBookingRecord = selectedSession ? conversationBookingRecord(selectedSession) : null;

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <div className="grid gap-4 xl:grid-cols-[1.4fr_0.6fr]">
          <Skeleton className="h-[520px]" />
          <Skeleton className="h-[520px]" />
        </div>
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>
          Call sessions are scoped by salon, so the owner profile must exist first.
        </CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Calls</h1>
          <p className="mt-1 text-sm text-muted">
            Live phone and simulator transcripts, booking outcomes, and owner handoffs.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={voiceStatus?.ready ? "ready" : "not_configured"} />
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Calls unavailable" message={error} /> : null}
      {actionError ? <Alert title="Call action failed" message={actionError} /> : null}
      {success ? <Alert type="success" title="Call review updated" message={success} /> : null}

      <ReadinessPanel aiEnabled={aiEnabled} voiceStatus={voiceStatus} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-5">
        <Metric label="Live phone calls" value={String(phoneCount)} />
        <Metric label="Simulator sessions" value={String(simulatorCount)} />
        <Metric label="Owner handoffs" value={String(handoffCount)} />
        <Metric label="Confirmed bookings" value={String(confirmedCount)} />
        <Metric label="Pending requests" value={String(fallbackCount)} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.35fr_0.65fr]">
        <Card>
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <CardTitle>Conversation transcript</CardTitle>
              <CardDescription>
                Simulator input stays separate from live phone transcripts.
              </CardDescription>
            </div>
            <Button type="button" variant="secondary" onClick={() => void startSession()} disabled={sending}>
              <Plus className="h-4 w-4" />
              New session
            </Button>
          </div>

          <div className="mt-5 h-[360px] overflow-y-auto rounded-md border border-line bg-slate-50 p-4">
            {selectedSession?.transcript?.length ? (
              <div className="space-y-3">
                {selectedSession.transcript.map((item) => (
                  <TranscriptBubble key={item.id} item={item} onAddCorrection={() => startCorrection(item)} />
                ))}
              </div>
            ) : (
              <EmptyTranscript />
            )}
          </div>

          {correctionTarget ? (
            <form className="mt-4 rounded-md border border-line bg-white p-4" onSubmit={saveCorrection}>
              <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
                <div>
                  <div className="text-sm font-semibold text-ink">Add correction</div>
                  <div className="mt-1 text-xs leading-5 text-muted">
                    {correctionTarget.item.speaker} message from {selectedSession?.channel || "session"}
                  </div>
                </div>
                <Button type="button" variant="ghost" onClick={clearCorrectionDraft} disabled={savingCorrection}>
                  <X className="h-4 w-4" />
                  Cancel
                </Button>
              </div>
              <div className="mt-3 rounded-md border border-line bg-slate-50 p-3 text-sm leading-6 text-muted">
                {correctionTarget.item.body}
              </div>
              <label className="mt-4 block">
                <span className="text-sm font-medium text-ink">Correction</span>
                <textarea
                  className="mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm leading-6 text-ink outline-none focus:border-brand"
                  value={correctionText}
                  onChange={(event) => setCorrectionText(event.target.value)}
                  placeholder="Describe what the AI should say or remember next time"
                  disabled={savingCorrection}
                />
              </label>
              <div className="mt-3 flex flex-wrap gap-3">
                <Button type="submit" disabled={savingCorrection || !correctionText.trim()}>
                  <MessageSquarePlus className="h-4 w-4" />
                  Save correction
                </Button>
              </div>
            </form>
          ) : null}

          <form className="mt-4 flex flex-col gap-3 sm:flex-row" onSubmit={sendMessage}>
            <label className="sr-only" htmlFor="customer-message">
              Customer message
            </label>
            <input
              id="customer-message"
              className="h-10 min-w-0 flex-1 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
              value={message}
              onChange={(event) => setMessage(event.target.value)}
              placeholder="Simulator customer message"
              disabled={sending}
            />
            <Button type="submit" disabled={sending || !message.trim()}>
              <Send className="h-4 w-4" />
              Send
            </Button>
          </form>
        </Card>

        <Card>
          <div className="flex items-start gap-3">
            <MessageSquareText className="mt-1 h-5 w-5 text-brand" />
            <div>
              <CardTitle>Detected details</CardTitle>
              <CardDescription>Current session fields and booking outcome.</CardDescription>
            </div>
          </div>

          {selectedSession ? (
            <dl className="mt-5 space-y-4">
              <Info label="Channel" value={<Badge value={selectedSession.channel} />} />
              <Info label="Intent" value={<Badge value={selectedSession.intent} />} />
              <Info label="Outcome" value={<Badge value={selectedSession.outcome} />} />
              <Info label="Customer" value={selectedSession.customer_name || "Not collected"} />
              <Info label="Phone" value={selectedSession.customer_phone || "Not collected"} />
              <Info
                label="Service(s)"
                value={selectedBookingRecord ? serviceNamesLabel(selectedBookingRecord, serviceNames) : "Not collected"}
              />
              <Info
                label="Technician preference"
                value={selectedBookingRecord ? <Badge value={technicianPreferenceValue(selectedBookingRecord)} /> : "Not collected"}
              />
              <Info
                label="Assigned technicians"
                value={selectedBookingRecord ? assignedTechniciansLabel(selectedBookingRecord, staffNames) : "Not assigned"}
              />
              <Info
                label="Requested time"
                value={selectedSession.requested_start_time ? formatDateTime(selectedSession.requested_start_time) : "Not collected"}
              />
              <Info label="Booking attempt" value={selectedSession.booking_attempt_id || "None"} />
              <Info label="Provider call" value={selectedSession.provider_call_id || "None"} />
            </dl>
          ) : (
            <div className="mt-5 rounded-md border border-line p-4 text-sm text-muted">
              No session selected.
            </div>
          )}

          <BookingNegotiationPanel session={selectedSession} serviceNames={serviceNames} staffNames={staffNames} />
        </Card>
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Recent call sessions</CardTitle>
            <CardDescription>
              Review live phone and simulator outcomes before owner follow-up.
            </CardDescription>
          </div>
          <Badge value={sessions.length > 0 ? "active" : "disabled"} />
        </div>

        {sessions.length === 0 ? (
          <div className="mt-5 rounded-md border border-line p-6 text-center">
            <PhoneCall className="mx-auto h-5 w-5 text-muted" />
            <div className="mt-3 text-sm font-semibold text-ink">No call sessions yet</div>
            <div className="mt-1 text-sm leading-6 text-muted">
              Transcript rows will appear after a simulator message or live phone webhook.
            </div>
          </div>
        ) : (
          <>
            <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[960px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Updated</th>
                    <th className="px-4 py-3">Channel</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Intent</th>
                    <th className="px-4 py-3">Outcome</th>
                    <th className="px-4 py-3">Booking</th>
                    <th className="px-4 py-3">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {sessions.map((item) => (
                    <tr key={item.id}>
                      <td className="px-4 py-3 text-muted">{formatDateTime(item.updated_at)}</td>
                      <td className="px-4 py-3">
                        <Badge value={item.channel} />
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{item.customer_name || "Unknown customer"}</div>
                        <div className="mt-1 text-xs text-muted">{item.customer_phone || "No phone"}</div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={item.intent} />
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={item.outcome} />
                      </td>
                      <td className="px-4 py-3 text-muted">{item.booking_attempt_id || "None"}</td>
                      <td className="px-4 py-3">
                        <Button type="button" variant="secondary" onClick={() => void selectSession(item.id)}>
                          Open
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {sessions.map((item) => (
                <SessionCard key={item.id} item={item} onOpen={() => void selectSession(item.id)} />
              ))}
            </div>
          </>
        )}
      </Card>
    </div>
  );
}

function ReadinessPanel({ aiEnabled, voiceStatus }: { aiEnabled: boolean; voiceStatus: VoiceStatus | null }) {
  const aiReady = Boolean(voiceStatus?.ai?.ready);
  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card className={voiceStatus?.ready ? "border-emerald-200 bg-emerald-50 shadow-none" : "border-amber-200 bg-amber-50 shadow-none"}>
        <div className="flex gap-3">
          {!voiceStatus?.ready ? <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" /> : null}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>Live phone webhook</CardTitle>
              <Badge value={voiceStatus?.ready ? "ready" : "not_configured"} />
            </div>
            <CardDescription className={voiceStatus?.ready ? "text-emerald-800" : "text-amber-900"}>
              {voiceStatus?.ready
                ? "Twilio webhooks can create phone call sessions and transcripts."
                : voiceStatus?.blocked_reason || "Voice provider readiness could not be verified."}
            </CardDescription>
            <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
              <Info label="Provider" value={voiceStatus?.provider || "twilio"} />
              <Info label="Signature" value={<Badge value={voiceStatus?.signature_verification ? "active" : "disabled"} />} />
              <Info label="Input mode" value={<Badge value={voiceStatus?.input_mode || "gather"} />} />
              <Info label="Salon phone" value={voiceStatus?.salon_phone || "Not configured"} />
              <Info label="Inbound webhook" value={<span className="break-all font-mono text-xs">{voiceStatus?.inbound_webhook_url || "Not configured"}</span>} />
              <Info label="Recording webhook" value={<span className="break-all font-mono text-xs">{voiceStatus?.recording_webhook_url || "Not configured"}</span>} />
            </dl>
          </div>
        </div>
      </Card>

      <Card className={aiReady ? "border-emerald-200 bg-emerald-50 shadow-none" : "border-amber-200 bg-amber-50 shadow-none"}>
        <div className="flex gap-3">
          {!aiReady ? <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" /> : null}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>External AI providers</CardTitle>
              <Badge value={aiReady ? "ready" : "not_configured"} />
            </div>
            <CardDescription className={aiReady ? "text-emerald-800" : "text-amber-900"}>
              {aiReady
                ? "External STT, LLM, and TTS are configured behind the voice runtime."
                : "Configure OpenAI voice settings before external AI voice turns are ready."}
            </CardDescription>
            <div className="mt-4 space-y-3 text-sm">
              <CapabilityRow label="STT" capability={voiceStatus?.ai?.stt} />
              <CapabilityRow label="LLM" capability={voiceStatus?.ai?.llm} />
              <CapabilityRow label="TTS" capability={voiceStatus?.ai?.tts} />
            </div>
          </div>
        </div>
      </Card>

      <Card className={aiEnabled ? "border-emerald-200 bg-emerald-50 shadow-none" : "border-amber-200 bg-amber-50 shadow-none"}>
        <div className="flex gap-3">
          {!aiEnabled ? <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" /> : null}
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>AI booking readiness</CardTitle>
              <Badge value={aiEnabled ? "active" : "disabled"} />
            </div>
            <CardDescription className={aiEnabled ? "text-emerald-800" : "text-amber-900"}>
              {aiEnabled
                ? "Confirmed bookings still require Square Appointments success through the booking service."
                : "Booking requests will be routed to owner review until Square readiness checks pass."}
            </CardDescription>
            {!aiEnabled ? (
              <a
                className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
                href="/dashboard/integrations"
              >
                Open Square integration
              </a>
            ) : null}
          </div>
        </div>
      </Card>
    </div>
  );
}

function CapabilityRow({ label, capability }: { label: string; capability?: VoiceStatus["ai"]["stt"] }) {
  return (
    <div className="rounded-md border border-line bg-white/70 p-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</span>
        <Badge value={capability?.ready ? "ready" : "not_configured"} />
      </div>
      <div className="mt-2 text-sm font-medium text-ink">{capability?.provider || "openai"}</div>
      <div className="mt-1 text-xs leading-5 text-muted">
        {capability?.model || "Model not configured"}
        {capability?.voice ? ` / ${capability.voice}` : ""}
      </div>
      {!capability?.ready && capability?.blocked_reason ? (
        <div className="mt-2 text-xs leading-5 text-amber-900">{capability.blocked_reason}</div>
      ) : null}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-2 text-2xl font-bold text-ink">{value}</div>
    </Card>
  );
}

function BookingNegotiationPanel({
  session,
  serviceNames,
  staffNames
}: {
  session: ConversationSession | null;
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
}) {
  const slots = session?.offered_slots ?? [];
  const status = bookingNegotiationStatus(session);

  return (
    <div className="mt-5 rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Booking negotiation</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            Slot offers, selected time, and Square Appointments confirmation state.
          </div>
        </div>
        <Badge value={status} />
      </div>

      {!session ? (
        <div className="mt-4 rounded-md border border-line bg-white p-3 text-sm leading-6 text-muted">
          No session selected.
        </div>
      ) : (
        <div className="mt-4 space-y-4">
          {slots.length > 0 ? (
            <div>
              <div className="text-xs font-semibold uppercase tracking-wide text-muted">Offered Square slots</div>
              <div className="mt-2 space-y-2">
                {slots.map((slot) => (
                  <OfferedSlotRow
                    key={`${slot.start_time}-${slot.staff_id}`}
                    slot={slot}
                    serviceNames={serviceNames}
                    staffNames={staffNames}
                  />
                ))}
              </div>
            </div>
          ) : (
            <div className="rounded-md border border-line bg-white p-3 text-sm leading-6 text-muted">
              No active slot offers for this session. Selected or completed sessions clear offered slots.
            </div>
          )}

          <dl className="space-y-4">
            <Info
              label="Selected slot"
              value={session.requested_start_time ? selectedSlotLabel(session, serviceNames, staffNames) : "Not selected"}
            />
            <Info label="Square confirmation" value={squareConfirmationLabel(session)} />
          </dl>

          {session.requested_start_time ? (
            <div>
              <div className="text-xs font-semibold uppercase tracking-wide text-muted">Selected segment assignment</div>
              <SegmentAssignmentList
                record={conversationBookingRecord(session)}
                serviceNames={serviceNames}
                staffNames={staffNames}
              />
            </div>
          ) : null}

          <div className="rounded-md border border-line bg-white p-3 text-xs leading-5 text-muted">
            Confirmed only after Square Appointments returns a booking ID through the booking service.
          </div>
        </div>
      )}
    </div>
  );
}

function OfferedSlotRow({
  slot,
  serviceNames,
  staffNames
}: {
  slot: OfferedSlot;
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
}) {
  return (
    <div className="rounded-md border border-line bg-white p-3">
      <div className="text-sm font-semibold text-ink">{formatDateTime(slot.start_time)}</div>
      <div className="mt-1 text-xs leading-5 text-muted">
        {formatTimeRange(slot.start_time, slot.end_time)}
      </div>
      <div className="mt-1 text-xs leading-5 text-muted">
        Customer-facing: {technicianPreferenceValue(slot) === "anyone" ? "Anyone available" : assignedTechniciansLabel(slot, staffNames)}
      </div>
      <div className="mt-1 text-xs leading-5 text-muted">
        Assigned: {assignedTechniciansLabel(slot, staffNames)}
      </div>
      <SegmentAssignmentList record={slot} serviceNames={serviceNames} staffNames={staffNames} />
    </div>
  );
}

function SegmentAssignmentList({
  record,
  serviceNames,
  staffNames
}: {
  record: OfferedSlot | ReturnType<typeof conversationBookingRecord>;
  serviceNames: Map<string, string>;
  staffNames: Map<string, string>;
}) {
  const segments = orderedSegments(record);
  if (segments.length === 0) {
    return null;
  }
  return (
    <div className="mt-3 space-y-2 border-t border-line pt-3">
      {segments.map((segment, index) => (
        <div key={`${segment.service_id ?? "service"}-${segment.staff_id ?? "staff"}-${index}`} className="text-xs leading-5 text-muted">
          <span className="font-semibold text-ink">
            {index + 1}. {segment.service_name || (segment.service_id ? serviceNames.get(segment.service_id) : "") || "Unknown service"}
          </span>
          {" -> "}
          <span>{segment.staff_name || (segment.staff_id ? staffNames.get(segment.staff_id) : "") || "Unassigned technician"}</span>
        </div>
      ))}
    </div>
  );
}

function TranscriptBubble({ item, onAddCorrection }: { item: TranscriptMessage; onAddCorrection: () => void }) {
  const isCustomer = item.speaker === "customer";
  const isTool = item.speaker === "tool";
  return (
    <div className={isCustomer ? "flex justify-end" : "flex justify-start"}>
      <div
        className={[
          "max-w-[85%] rounded-md border px-3 py-2 text-sm leading-6",
          isCustomer ? "border-brand bg-brand text-white" : "border-line bg-white text-ink",
          isTool ? "border-amber-200 bg-amber-50 text-amber-900" : ""
        ].join(" ")}
      >
        <div className="mb-1 flex flex-wrap items-center justify-between gap-2 text-[11px] font-semibold uppercase opacity-75">
          <span>{item.speaker}</span>
          <button
            type="button"
            className={[
              "rounded-md border px-2 py-1 text-[11px] font-semibold normal-case opacity-100 transition",
              isCustomer
                ? "border-white/35 bg-white/10 text-white hover:bg-white/20"
                : "border-line bg-white text-ink hover:bg-slate-50"
            ].join(" ")}
            onClick={onAddCorrection}
          >
            Add correction
          </button>
        </div>
        <div>{item.body}</div>
      </div>
    </div>
  );
}

function EmptyTranscript() {
  return (
    <div className="flex h-full items-center justify-center text-center">
      <div>
        <MessageSquareText className="mx-auto h-5 w-5 text-muted" />
        <div className="mt-3 text-sm font-semibold text-ink">No transcript selected</div>
        <div className="mt-1 text-sm leading-6 text-muted">Start a session or open a recent transcript.</div>
      </div>
    </div>
  );
}

function Info({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</dt>
      <dd className="mt-1 text-sm font-medium text-ink">{value}</dd>
    </div>
  );
}

function SessionCard({ item, onOpen }: { item: ConversationSession; onOpen: () => void }) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{item.customer_name || "Unknown customer"}</div>
          <div className="mt-1 text-xs text-muted">{formatDateTime(item.updated_at)}</div>
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          <Badge value={item.channel} />
          <Badge value={item.outcome} />
        </div>
      </div>
      <div className="mt-4 grid gap-3 text-sm">
        <div className="flex items-center justify-between gap-3">
          <span className="text-muted">Intent</span>
          <Badge value={item.intent} />
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-muted">Booking</span>
          <span className="font-medium text-ink">{item.booking_attempt_id || "None"}</span>
        </div>
      </div>
      <Button type="button" variant="secondary" className="mt-4 w-full" onClick={onOpen}>
        Open
      </Button>
    </div>
  );
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit"
  }).format(new Date(value));
}

function formatTimeRange(start: string, end: string) {
  const formatter = new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit"
  });
  return `${formatter.format(new Date(start))} - ${formatter.format(new Date(end))}`;
}

function selectedSlotLabel(session: ConversationSession, serviceNames: Map<string, string>, staffNames: Map<string, string>) {
  if (!session.requested_start_time) return "Not selected";
  const record = conversationBookingRecord(session);
  return `${formatDateTime(session.requested_start_time)} · ${serviceNamesLabel(record, serviceNames)} · Preference: ${technicianPreferenceLabel(record)} · Assigned: ${assignedTechniciansLabel(record, staffNames)}`;
}

function squareConfirmationLabel(session: ConversationSession) {
  if (session.outcome === "booking_confirmed" && session.booking_attempt_id && session.appointment_id) {
    return `Confirmed by Square Appointments (${session.booking_attempt_id})`;
  }
  if (session.outcome === "booking_fallback_pending") {
    return session.booking_attempt_id
      ? `Pending owner review (${session.booking_attempt_id})`
      : "Pending owner review";
  }
  return "Not confirmed";
}

function bookingNegotiationStatus(session: ConversationSession | null) {
  if (!session) return "not_started";
  if (session.outcome === "booking_confirmed" && session.booking_attempt_id && session.appointment_id) {
    return "confirmed";
  }
  if (session.outcome === "booking_fallback_pending") {
    return "pending_request";
  }
  if ((session.offered_slots ?? []).length > 0) {
    return "slots_offered";
  }
  if (session.requested_start_time) {
    return "slot_selected";
  }
  return "not_started";
}
