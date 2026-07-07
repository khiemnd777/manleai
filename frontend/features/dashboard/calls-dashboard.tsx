"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  Archive,
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Eraser,
  MessageSquarePlus,
  MessageSquareText,
  PhoneCall,
  Plus,
  RefreshCcw,
  Send,
  Users,
  X
} from "lucide-react";
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
  PartyBookingRequest,
  POSConnection,
  POSService,
  POSStaffMember,
  RealtimeEventLog,
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
  limit?: number;
  offset?: number;
  has_more?: boolean;
};

type RealtimeEventsResponse = {
  events: RealtimeEventLog[];
};

type PartyRequestsResponse = {
  party_booking_requests: PartyBookingRequest[];
  limit?: number;
  offset?: number;
  has_more?: boolean;
};

type PartyRequestResponse = {
  party_booking_request: PartyBookingRequest;
};

type LifecycleFilter = "active" | "archived" | "redacted";
type PartyStatusFilter = "pending" | "contacted" | "resolved" | "dismissed";

const lifecycleFilters: LifecycleFilter[] = ["active", "archived", "redacted"];
const partyStatusFilters: PartyStatusFilter[] = ["pending", "contacted", "resolved", "dismissed"];
const defaultSessionPageSize = 10;
const defaultPartyRequestPageSize = 10;
const sessionPageSizeOptions = [10, 25, 50] as const;
const partyRequestPageSizeOptions = [10, 25, 50] as const;
const readinessMetricLimit = 100;

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
  const [readinessSessions, setReadinessSessions] = useState<ConversationSession[]>([]);
  const [selectedSession, setSelectedSession] = useState<ConversationSession | null>(null);
  const [realtimeEvents, setRealtimeEvents] = useState<RealtimeEventLog[]>([]);
  const [partyRequests, setPartyRequests] = useState<PartyBookingRequest[]>([]);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [message, setMessage] = useState("");
  const [correctionTarget, setCorrectionTarget] = useState<CorrectionTarget | null>(null);
  const [correctionText, setCorrectionText] = useState("");
  const [lifecycleFilter, setLifecycleFilter] = useState<LifecycleFilter>("active");
  const [partyStatusFilter, setPartyStatusFilter] = useState<PartyStatusFilter>("pending");
  const [sessionLimit, setSessionLimit] = useState(defaultSessionPageSize);
  const [sessionOffset, setSessionOffset] = useState(0);
  const [sessionHasMore, setSessionHasMore] = useState(false);
  const [partyLimit, setPartyLimit] = useState(defaultPartyRequestPageSize);
  const [partyOffset, setPartyOffset] = useState(0);
  const [partyHasMore, setPartyHasMore] = useState(false);
  const [readinessSessionsHasMore, setReadinessSessionsHasMore] = useState(false);
  const [initialLoading, setInitialLoading] = useState(true);
  const [sessionListLoading, setSessionListLoading] = useState(false);
  const [realtimeEventsLoading, setRealtimeEventsLoading] = useState(false);
  const [partyRequestsLoading, setPartyRequestsLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [savingCorrection, setSavingCorrection] = useState(false);
  const [sessionActionID, setSessionActionID] = useState("");
  const [partyActionID, setPartyActionID] = useState("");
  const [error, setError] = useState("");
  const [actionError, setActionError] = useState("");
  const [realtimeEventsError, setRealtimeEventsError] = useState("");
  const [success, setSuccess] = useState("");

  function sessionListPath(salonID: string, filter: LifecycleFilter, limit: number, offset: number) {
    const params = new URLSearchParams({
      limit: String(limit),
      offset: String(offset),
      lifecycle_status: filter
    });
    return `/api/salons/${salonID}/conversation-sessions?${params.toString()}`;
  }

  async function fetchSessionPage(salonID: string, filter: LifecycleFilter, limit: number, offset: number) {
    return apiRequest<SessionsResponse>(sessionListPath(salonID, filter, limit, offset));
  }

  async function fetchSessionPageWithFallback(salonID: string, filter: LifecycleFilter, limit: number, offset: number) {
    let response = await fetchSessionPage(salonID, filter, limit, offset);
    if (response.sessions.length === 0 && offset > 0) {
      const previousOffset = Math.max(0, offset - limit);
      response = await fetchSessionPage(salonID, filter, limit, previousOffset);
    }
    return response;
  }

  function partyRequestsPath(salonID: string, status: PartyStatusFilter, limit: number, offset: number) {
    const params = new URLSearchParams({
      status,
      limit: String(limit),
      offset: String(offset)
    });
    return `/api/salons/${salonID}/party-booking-requests?${params.toString()}`;
  }

  async function fetchPartyRequests(salonID: string, status: PartyStatusFilter, limit: number, offset: number) {
    return apiRequest<PartyRequestsResponse>(partyRequestsPath(salonID, status, limit, offset));
  }

  async function fetchPartyRequestsWithFallback(salonID: string, status: PartyStatusFilter, limit: number, offset: number) {
    let response = await fetchPartyRequests(salonID, status, limit, offset);
    if (response.party_booking_requests.length === 0 && offset > 0) {
      const previousOffset = Math.max(0, offset - limit);
      response = await fetchPartyRequests(salonID, status, limit, previousOffset);
    }
    return response;
  }

  function applySessionPage(response: SessionsResponse, requestedLimit: number, requestedOffset: number) {
    setSessions(response.sessions);
    setSessionLimit(response.limit ?? requestedLimit);
    setSessionOffset(response.offset ?? requestedOffset);
    setSessionHasMore(Boolean(response.has_more));
  }

  function applyPartyRequestPage(response: PartyRequestsResponse, requestedLimit: number, requestedOffset: number) {
    setPartyRequests(response.party_booking_requests);
    setPartyLimit(response.limit ?? requestedLimit);
    setPartyOffset(response.offset ?? requestedOffset);
    setPartyHasMore(Boolean(response.has_more));
  }

  async function reloadReadinessSessions(salonID: string) {
    const response = await fetchSessionPage(salonID, "active", readinessMetricLimit, 0);
    setReadinessSessions(response.sessions);
    setReadinessSessionsHasMore(Boolean(response.has_more));
  }

  async function load(
    filter: LifecycleFilter = lifecycleFilter,
    fullPage = initialLoading,
    offset = sessionOffset,
    limit = sessionLimit
  ) {
    setError("");
    if (fullPage) {
      setInitialLoading(true);
    } else {
      setSessionListLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setVoiceStatus(null);
        setSessions([]);
        setReadinessSessions([]);
        setSessionHasMore(false);
        setPartyHasMore(false);
        setReadinessSessionsHasMore(false);
        setSelectedSession(null);
        setRealtimeEvents([]);
        setPartyRequests([]);
        setServices([]);
        setStaff([]);
        return;
      }

      const [statusResponse, voiceResponse, sessionsResponse, readinessResponse, partyResponse, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<VoiceStatus>(`/api/salons/${firstSalon.id}/voice/status`),
        fetchSessionPageWithFallback(firstSalon.id, filter, limit, offset),
        fetchSessionPage(firstSalon.id, "active", readinessMetricLimit, 0),
        fetchPartyRequestsWithFallback(firstSalon.id, partyStatusFilter, partyLimit, partyOffset),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(statusResponse);
      setVoiceStatus(voiceResponse);
      applySessionPage(sessionsResponse, limit, offset);
      setReadinessSessions(readinessResponse.sessions);
      setReadinessSessionsHasMore(Boolean(readinessResponse.has_more));
      applyPartyRequestPage(partyResponse, partyLimit, partyOffset);
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
        await loadRealtimeEventsForSession(firstSalon.id, detail);
      } else {
        setSelectedSession(null);
        setRealtimeEvents([]);
        clearCorrectionDraft();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load call sessions.");
    } finally {
      if (fullPage) {
        setInitialLoading(false);
      } else {
        setSessionListLoading(false);
      }
    }
  }

  useEffect(() => {
    void load(lifecycleFilter, initialLoading, sessionOffset, sessionLimit);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lifecycleFilter, sessionOffset, sessionLimit]);

  useEffect(() => {
    if (!salon) return;
    void reloadPartyRequests(salon.id, partyStatusFilter, partyOffset, partyLimit);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [salon?.id, partyStatusFilter, partyOffset, partyLimit]);

  useEffect(() => {
    if (!salon || !selectedSession || selectedSession.channel !== "phone") {
      setRealtimeEvents([]);
      setRealtimeEventsError("");
      setRealtimeEventsLoading(false);
      return;
    }
    void loadRealtimeEvents(salon.id, selectedSession.id);
  }, [salon?.id, selectedSession?.id, selectedSession?.updated_at, selectedSession?.channel]);

  async function loadRealtimeEvents(salonID: string, sessionID: string) {
    setRealtimeEventsLoading(true);
    setRealtimeEventsError("");
    try {
      const response = await apiRequest<RealtimeEventsResponse>(
        `/api/salons/${salonID}/conversation-sessions/${sessionID}/realtime-events?limit=50`
      );
      setRealtimeEvents(response.events);
    } catch (err) {
      setRealtimeEvents([]);
      setRealtimeEventsError(err instanceof Error ? err.message : "Could not load realtime events.");
    } finally {
      setRealtimeEventsLoading(false);
    }
  }

  async function loadRealtimeEventsForSession(salonID: string, session: ConversationSession) {
    if (session.channel !== "phone") {
      setRealtimeEvents([]);
      setRealtimeEventsError("");
      setRealtimeEventsLoading(false);
      return;
    }
    await loadRealtimeEvents(salonID, session.id);
  }

  async function reloadPartyRequests(salonID: string, status: PartyStatusFilter, offset = partyOffset, limit = partyLimit) {
    setPartyRequestsLoading(true);
    setActionError("");
    try {
      const response = await fetchPartyRequestsWithFallback(salonID, status, limit, offset);
      applyPartyRequestPage(response, limit, offset);
    } catch (err) {
      setPartyRequests([]);
      setPartyHasMore(false);
      setActionError(err instanceof Error ? err.message : "Could not load party booking requests.");
    } finally {
      setPartyRequestsLoading(false);
    }
  }

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
      if (lifecycleFilter !== "active" || sessionOffset !== 0) {
        setSessionOffset(0);
        setLifecycleFilter("active");
      } else {
        await reloadSessions(salon.id, "active", 0, sessionLimit);
      }
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
      await reloadSessions(salon.id, lifecycleFilter, sessionOffset, sessionLimit);
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
      await loadRealtimeEventsForSession(salon.id, detail);
      clearCorrectionDraft();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not load transcript.");
    }
  }

  async function reloadSessions(salonID: string, filter: LifecycleFilter, offset = sessionOffset, limit = sessionLimit) {
    const response = await fetchSessionPageWithFallback(salonID, filter, limit, offset);
    applySessionPage(response, limit, offset);
    await reloadReadinessSessions(salonID);
    return response;
  }

  async function archiveSession(item: ConversationSession) {
    if (!salon || item.lifecycle_status === "redacted") return;
    setActionError("");
    setSuccess("");
    setSessionActionID(`${item.id}:archive`);
    try {
      const updated = await apiRequest<ConversationSession>(
        `/api/salons/${salon.id}/conversation-sessions/${item.id}/archive`,
        { method: "POST" }
      );
      if (selectedSession?.id === item.id) {
        setSelectedSession(updated);
      }
      setSuccess("Call session archived.");
      await reloadSessions(salon.id, lifecycleFilter, sessionOffset, sessionLimit);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not archive call session.");
    } finally {
      setSessionActionID("");
    }
  }

  async function redactSession(item: ConversationSession) {
    if (!salon || item.status === "active" || item.lifecycle_status === "redacted") return;
    if (!window.confirm("Redact this transcript and customer details? This cannot be undone from the dashboard.")) return;
    setActionError("");
    setSuccess("");
    setSessionActionID(`${item.id}:redact`);
    try {
      const updated = await apiRequest<ConversationSession>(
        `/api/salons/${salon.id}/conversation-sessions/${item.id}/redact`,
        { method: "POST" }
      );
      if (selectedSession?.id === item.id) {
        setSelectedSession(updated);
        clearCorrectionDraft();
      }
      setSuccess("Transcript and customer details redacted.");
      await reloadSessions(salon.id, lifecycleFilter, sessionOffset, sessionLimit);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not redact call session.");
    } finally {
      setSessionActionID("");
    }
  }

  async function updatePartyRequestStatus(item: PartyBookingRequest, status: PartyStatusFilter) {
    if (!salon) return;
    setActionError("");
    setSuccess("");
    setPartyActionID(`${item.id}:${status}`);
    try {
      const response = await apiRequest<PartyRequestResponse>(
        `/api/salons/${salon.id}/party-booking-requests/${item.id}/status`,
        {
          method: "PATCH",
          body: JSON.stringify({ status })
        }
      );
      setSuccess(partyStatusSuccess(status));
      if (selectedSession?.id === response.party_booking_request.call_session_id) {
        setSelectedSession((current) =>
          current ? { ...current, party_request: response.party_booking_request } : current
        );
      }
      await reloadPartyRequests(salon.id, partyStatusFilter, partyOffset, partyLimit);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not update party booking request.");
    } finally {
      setPartyActionID("");
    }
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
    if (!selectedSession || selectedSession.lifecycle_status === "redacted") return;
    setCorrectionTarget({ sessionID: selectedSession.id, item });
    setCorrectionText("");
    setActionError("");
    setSuccess("");
  }

  function clearCorrectionDraft() {
    setCorrectionTarget(null);
    setCorrectionText("");
  }

  function updateLifecycleFilter(filter: LifecycleFilter) {
    setSessionOffset(0);
    setLifecycleFilter(filter);
  }

  function updatePartyStatusFilter(filter: PartyStatusFilter) {
    setPartyOffset(0);
    setPartyStatusFilter(filter);
  }

  function updateSessionPageSize(limit: number) {
    setSessionLimit(limit);
    setSessionOffset(0);
  }

  function updatePartyPageSize(limit: number) {
    setPartyLimit(limit);
    setPartyOffset(0);
  }

  function goToPreviousSessionPage() {
    setSessionOffset((current) => Math.max(0, current - sessionLimit));
  }

  function goToNextSessionPage() {
    if (!sessionHasMore) return;
    setSessionOffset((current) => current + sessionLimit);
  }

  function goToPreviousPartyPage() {
    setPartyOffset((current) => Math.max(0, current - partyLimit));
  }

  function goToNextPartyPage() {
    if (!partyHasMore) return;
    setPartyOffset((current) => current + partyLimit);
  }

  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
  const phoneCount = useMemo(
    () => countLabel(readinessSessions.filter((item) => item.channel === "phone").length, readinessSessionsHasMore),
    [readinessSessions, readinessSessionsHasMore]
  );
  const simulatorCount = useMemo(
    () => countLabel(readinessSessions.filter((item) => item.channel === "simulator").length, readinessSessionsHasMore),
    [readinessSessions, readinessSessionsHasMore]
  );
  const handoffCount = useMemo(
    () =>
      countLabel(
        readinessSessions.filter((item) => item.status === "handoff" || item.outcome === "handoff_requested").length,
        readinessSessionsHasMore
      ),
    [readinessSessions, readinessSessionsHasMore]
  );
  const confirmedCount = useMemo(
    () => countLabel(readinessSessions.filter((item) => item.outcome === "booking_confirmed").length, readinessSessionsHasMore),
    [readinessSessions, readinessSessionsHasMore]
  );
  const fallbackCount = useMemo(
    () =>
      countLabel(
        readinessSessions.filter((item) => item.outcome === "booking_fallback_pending" || item.outcome === "ai_disabled").length,
        readinessSessionsHasMore
      ),
    [readinessSessions, readinessSessionsHasMore]
  );
  const partyRequestCount = partyRequestsLoading ? "..." : countLabel(partyRequests.length, partyHasMore);
  const serviceNames = useMemo(
    () => new Map(services.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [services]
  );
  const staffNames = useMemo(
    () => new Map(staff.flatMap((item) => (item.id ? [[item.id, item.name] as const] : []))),
    [staff]
  );
  const selectedBookingRecord = selectedSession ? conversationBookingRecord(selectedSession) : null;
  const lastRealtimeEvent = realtimeEvents.length > 0 ? realtimeEvents[realtimeEvents.length - 1] : null;

  if (initialLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
          <Skeleton className="h-28" />
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
          <Button type="button" variant="secondary" onClick={() => void load(lifecycleFilter, false, sessionOffset, sessionLimit)}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Calls unavailable" message={error} /> : null}
      {actionError ? <Alert title="Call action failed" message={actionError} /> : null}
      {success ? <Alert type="success" title="Call review updated" message={success} /> : null}

      <ReadinessPanel aiEnabled={aiEnabled} voiceStatus={voiceStatus} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
        <Metric label="Live phone calls" value={phoneCount} />
        <Metric label="Simulator sessions" value={simulatorCount} />
        <Metric label="Owner handoffs" value={handoffCount} />
        <Metric label="Confirmed bookings" value={confirmedCount} />
        <Metric label="Fallback requests" value={fallbackCount} />
        <Metric label={`${partyStatusFilter} parties`} value={partyRequestCount} />
      </div>

      <div className="grid items-stretch gap-4 xl:grid-cols-[1.35fr_0.65fr]">
        <Card className="flex min-h-[520px] flex-col">
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

          <div className="mt-5 min-h-[360px] flex-1 overflow-y-auto rounded-md border border-line bg-slate-50 p-4">
            {selectedSession?.transcript?.length ? (
              <div className="space-y-3">
                {selectedSession.transcript.map((item) => (
                  <TranscriptBubble
                    key={item.id}
                    item={item}
                    canAddCorrection={selectedSession.lifecycle_status !== "redacted"}
                    onAddCorrection={() => startCorrection(item)}
                  />
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
              <Info label="Lifecycle" value={<Badge value={selectedSession.lifecycle_status} />} />
              <Info label="Intent" value={<Badge value={selectedSession.intent} />} />
              <Info label="Action" value={<Badge value={bookingActionValue(selectedSession)} />} />
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
              <Info label="Target appointment" value={selectedSession.target_appointment_id || "None"} />
              {selectedSession.party_request ? (
                <>
                  <Info label="Party request" value={<Badge value={selectedSession.party_request.status} />} />
                  <Info label="Party summary" value={selectedSession.party_request.summary || "No summary recorded"} />
                </>
              ) : null}
              <Info label="Retention" value={retentionLabel(selectedSession)} />
              <Info label="Provider call" value={selectedSession.provider_call_id || "None"} />
              {selectedSession.channel === "phone" ? (
                <>
                  <Info label="Last realtime status" value={lastRealtimeEvent ? <Badge value={lastRealtimeEvent.event_type} /> : "No event"} />
                  <Info label="Last realtime stage" value={lastRealtimeEvent?.stage || lastRealtimeEvent?.stream_event || "Not recorded"} />
                </>
              ) : null}
            </dl>
          ) : (
            <div className="mt-5 rounded-md border border-line p-4 text-sm text-muted">
              No session selected.
            </div>
          )}

          <BookingNegotiationPanel session={selectedSession} serviceNames={serviceNames} staffNames={staffNames} />
        </Card>
      </div>

      <RealtimeEventsPanel
        session={selectedSession}
        events={realtimeEvents}
        loading={realtimeEventsLoading}
        error={realtimeEventsError}
      />

      <PartyRequestsPanel
        requests={partyRequests}
        statusFilter={partyStatusFilter}
        loading={partyRequestsLoading}
        limit={partyLimit}
        offset={partyOffset}
        hasMore={partyHasMore}
        busy={partyActionID}
        onStatusFilterChange={updatePartyStatusFilter}
        onPrevious={goToPreviousPartyPage}
        onNext={goToNextPartyPage}
        onLimitChange={updatePartyPageSize}
        onUpdateStatus={(item, status) => void updatePartyRequestStatus(item, status)}
        onOpenSession={(sessionID) => void selectSession(sessionID)}
      />

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Call session log</CardTitle>
            <CardDescription>
              Active transcripts retain for 90 days, then the worker redacts customer details and transcript text.
            </CardDescription>
          </div>
          <Badge value={sessions.length > 0 ? lifecycleFilter : "disabled"} />
        </div>
        <div className="mt-5 flex flex-wrap gap-2">
          {lifecycleFilters.map((filter) => (
            <Button
              key={filter}
              type="button"
              variant={filter === lifecycleFilter ? "primary" : "secondary"}
              onClick={() => updateLifecycleFilter(filter)}
              disabled={sessionListLoading || sessionActionID !== ""}
            >
              {filterLabel(filter)}
            </Button>
          ))}
        </div>
        {sessionListLoading ? (
          <div className="mt-4 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
            Loading {filterLabel(lifecycleFilter).toLowerCase()} sessions...
          </div>
        ) : null}

        {sessions.length === 0 ? (
          <div className="mt-5 rounded-md border border-line p-6 text-center">
            <PhoneCall className="mx-auto h-5 w-5 text-muted" />
            <div className="mt-3 text-sm font-semibold text-ink">{emptySessionsTitle(lifecycleFilter)}</div>
            <div className="mt-1 text-sm leading-6 text-muted">
              {emptySessionsDescription(lifecycleFilter)}
            </div>
          </div>
        ) : (
          <>
            <SessionPaginationControls
              className="mt-5"
              count={sessions.length}
              filter={lifecycleFilter}
              limit={sessionLimit}
              offset={sessionOffset}
              hasMore={sessionHasMore}
              busy={sessionListLoading || sessionActionID !== ""}
              onPrevious={goToPreviousSessionPage}
              onNext={goToNextSessionPage}
              onLimitChange={updateSessionPageSize}
            />
            <div className="mt-3 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[1260px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Updated</th>
                    <th className="px-4 py-3">Channel</th>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Intent</th>
                    <th className="px-4 py-3">Action</th>
                    <th className="px-4 py-3">Outcome</th>
                    <th className="px-4 py-3">Booking</th>
                    <th className="px-4 py-3">Retention</th>
                    <th className="px-4 py-3">Actions</th>
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
                        <Badge value={bookingActionValue(item)} />
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={item.outcome} />
                      </td>
                      <td className="px-4 py-3 text-muted">{item.booking_attempt_id || "None"}</td>
                      <td className="px-4 py-3 text-muted">{retentionLabel(item)}</td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-2">
                          <Button type="button" variant="secondary" onClick={() => void selectSession(item.id)}>
                            Open
                          </Button>
                          <Button
                            type="button"
                            variant="secondary"
                            onClick={() => void archiveSession(item)}
                            disabled={sessionActionID !== "" || item.lifecycle_status !== "active"}
                          >
                            <Archive className="h-4 w-4" />
                            Archive
                          </Button>
                          <Button
                            type="button"
                            variant="danger"
                            onClick={() => void redactSession(item)}
                            disabled={sessionActionID !== "" || item.status === "active" || item.lifecycle_status === "redacted"}
                          >
                            <Eraser className="h-4 w-4" />
                            Redact
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {sessions.map((item) => (
                <SessionCard
                  key={item.id}
                  item={item}
                  busy={sessionActionID !== ""}
                  onOpen={() => void selectSession(item.id)}
                  onArchive={() => void archiveSession(item)}
                  onRedact={() => void redactSession(item)}
                />
              ))}
            </div>
            <SessionPaginationControls
              className="mt-4"
              count={sessions.length}
              filter={lifecycleFilter}
              limit={sessionLimit}
              offset={sessionOffset}
              hasMore={sessionHasMore}
              busy={sessionListLoading || sessionActionID !== ""}
              onPrevious={goToPreviousSessionPage}
              onNext={goToNextSessionPage}
              onLimitChange={updateSessionPageSize}
            />
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
              <Info label="Realtime stream" value={<span className="break-all font-mono text-xs">{voiceStatus?.stream_webhook_url || "Not configured"}</span>} />
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
                ? "External STT, LLM, TTS, and realtime audio settings are configured behind the voice runtime."
                : "Configure OpenAI voice settings before external AI voice turns are ready."}
            </CardDescription>
            <div className="mt-4 space-y-3 text-sm">
              <CapabilityRow label="STT" capability={voiceStatus?.ai?.stt} />
              <CapabilityRow label="LLM" capability={voiceStatus?.ai?.llm} />
              <CapabilityRow label="TTS" capability={voiceStatus?.ai?.tts} />
              <CapabilityRow label="Realtime" capability={voiceStatus?.ai?.realtime} />
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

function PartyRequestsPanel({
  requests,
  statusFilter,
  loading,
  limit,
  offset,
  hasMore,
  busy,
  onStatusFilterChange,
  onPrevious,
  onNext,
  onLimitChange,
  onUpdateStatus,
  onOpenSession
}: {
  requests: PartyBookingRequest[];
  statusFilter: PartyStatusFilter;
  loading: boolean;
  limit: number;
  offset: number;
  hasMore: boolean;
  busy: string;
  onStatusFilterChange: (status: PartyStatusFilter) => void;
  onPrevious: () => void;
  onNext: () => void;
  onLimitChange: (limit: number) => void;
  onUpdateStatus: (request: PartyBookingRequest, status: PartyStatusFilter) => void;
  onOpenSession: (sessionID: string) => void;
}) {
  const controlsBusy = loading || busy !== "";
  const actionBusy = controlsBusy ? busy || "loading" : "";
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="flex items-start gap-3">
          <Users className="mt-1 h-5 w-5 text-brand" />
          <div>
            <CardTitle>Party booking requests</CardTitle>
            <CardDescription>
              Group requests are owner-review handoffs. They are not confirmed appointments until staff handles them in the POS.
            </CardDescription>
          </div>
        </div>
        <Badge value={statusFilter} />
      </div>

      <div className="mt-5 flex flex-wrap gap-2">
        {partyStatusFilters.map((status) => (
          <Button
            key={status}
            type="button"
            variant={status === statusFilter ? "primary" : "secondary"}
            onClick={() => onStatusFilterChange(status)}
            disabled={controlsBusy}
          >
            {partyStatusLabel(status)}
          </Button>
        ))}
      </div>

      {loading ? (
        <div className="mt-4 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
          Loading party booking requests...
        </div>
      ) : null}

      {!loading && requests.length === 0 ? (
        <div className="mt-5 rounded-md border border-line p-6 text-center">
          <Users className="mx-auto h-5 w-5 text-muted" />
          <div className="mt-3 text-sm font-semibold text-ink">No {partyStatusLabel(statusFilter).toLowerCase()} party requests</div>
          <div className="mt-1 text-sm leading-6 text-muted">
            Party requests appear here when a caller asks to book for a group or multiple guests.
          </div>
        </div>
      ) : null}

      {requests.length > 0 ? (
        <>
          <PartyPaginationControls
            className="mt-5"
            count={requests.length}
            status={statusFilter}
            limit={limit}
            offset={offset}
            hasMore={hasMore}
            busy={controlsBusy}
            onPrevious={onPrevious}
            onNext={onNext}
            onLimitChange={onLimitChange}
          />
          <div className="mt-3 hidden overflow-x-auto rounded-md border border-line lg:block">
            <table className="w-full min-w-[1080px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Created</th>
                  <th className="px-4 py-3">Representative</th>
                  <th className="px-4 py-3">Party</th>
                  <th className="px-4 py-3">Requested time</th>
                  <th className="px-4 py-3">Services</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {requests.map((request) => (
                  <tr key={request.id}>
                    <td className="px-4 py-3 text-muted">{formatDateTime(request.created_at)}</td>
                    <td className="px-4 py-3">
                      <div className="font-medium text-ink">{request.representative_name || "Not collected"}</div>
                      <div className="mt-1 text-xs text-muted">{request.representative_phone || "No phone"}</div>
                    </td>
                    <td className="px-4 py-3 text-muted">{request.party_size ? `${request.party_size} guests` : "Size not collected"}</td>
                    <td className="px-4 py-3 text-muted">{partyTimeLabel(request)}</td>
                    <td className="px-4 py-3 text-muted">{partyServicesLabel(request)}</td>
                    <td className="px-4 py-3">
                      <Badge value={request.status} />
                    </td>
                    <td className="px-4 py-3">
                      <PartyRequestActions
                        request={request}
                        busy={actionBusy}
                        onUpdateStatus={onUpdateStatus}
                        onOpenSession={onOpenSession}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-5 space-y-3 lg:hidden">
            {requests.map((request) => (
              <PartyRequestCard
                key={request.id}
                request={request}
                busy={actionBusy}
                onUpdateStatus={onUpdateStatus}
                onOpenSession={onOpenSession}
              />
            ))}
          </div>
          <PartyPaginationControls
            className="mt-4"
            count={requests.length}
            status={statusFilter}
            limit={limit}
            offset={offset}
            hasMore={hasMore}
            busy={controlsBusy}
            onPrevious={onPrevious}
            onNext={onNext}
            onLimitChange={onLimitChange}
          />
        </>
      ) : null}
    </Card>
  );
}

function PartyRequestCard({
  request,
  busy,
  onUpdateStatus,
  onOpenSession
}: {
  request: PartyBookingRequest;
  busy: string;
  onUpdateStatus: (request: PartyBookingRequest, status: PartyStatusFilter) => void;
  onOpenSession: (sessionID: string) => void;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">{request.representative_name || "Party request"}</div>
          <div className="mt-1 text-xs text-muted">{formatDateTime(request.created_at)}</div>
        </div>
        <Badge value={request.status} />
      </div>
      <dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
        <Info label="Phone" value={request.representative_phone || "No phone"} />
        <Info label="Party size" value={request.party_size ? `${request.party_size} guests` : "Not collected"} />
        <Info label="Requested time" value={partyTimeLabel(request)} />
        <Info label="Services" value={partyServicesLabel(request)} />
      </dl>
      <div className="mt-3 rounded-md border border-line bg-slate-50 p-3 text-sm leading-6 text-muted">
        {request.summary || "No summary recorded."}
      </div>
      <PartyRequestActions request={request} busy={busy} onUpdateStatus={onUpdateStatus} onOpenSession={onOpenSession} />
    </div>
  );
}

function PartyRequestActions({
  request,
  busy,
  onUpdateStatus,
  onOpenSession
}: {
  request: PartyBookingRequest;
  busy: string;
  onUpdateStatus: (request: PartyBookingRequest, status: PartyStatusFilter) => void;
  onOpenSession: (sessionID: string) => void;
}) {
  const disabled = busy !== "";
  return (
    <div className="mt-3 flex flex-wrap gap-2 lg:mt-0">
      <Button type="button" variant="secondary" onClick={() => onOpenSession(request.call_session_id)} disabled={disabled}>
        Open transcript
      </Button>
      {request.status === "pending" ? (
        <Button type="button" variant="secondary" onClick={() => onUpdateStatus(request, "contacted")} disabled={disabled}>
          <PhoneCall className="h-4 w-4" />
          {busy === `${request.id}:contacted` ? "Saving..." : "Mark contacted"}
        </Button>
      ) : null}
      {request.status === "pending" || request.status === "contacted" ? (
        <>
          <Button type="button" onClick={() => onUpdateStatus(request, "resolved")} disabled={disabled}>
            <CheckCircle2 className="h-4 w-4" />
            {busy === `${request.id}:resolved` ? "Saving..." : "Resolve"}
          </Button>
          <Button type="button" variant="danger" onClick={() => onUpdateStatus(request, "dismissed")} disabled={disabled}>
            <X className="h-4 w-4" />
            {busy === `${request.id}:dismissed` ? "Saving..." : "Dismiss"}
          </Button>
        </>
      ) : null}
      {request.status === "dismissed" ? (
        <Button type="button" variant="secondary" onClick={() => onUpdateStatus(request, "pending")} disabled={disabled}>
          Reopen
        </Button>
      ) : null}
    </div>
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

function RealtimeEventsPanel({
  session,
  events,
  loading,
  error
}: {
  session: ConversationSession | null;
  events: RealtimeEventLog[];
  loading: boolean;
  error: string;
}) {
  if (!session || session.channel !== "phone") {
    return null;
  }

  const last = events.length > 0 ? events[events.length - 1] : null;

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Realtime event timeline</CardTitle>
          <CardDescription>Twilio stream lifecycle and backend realtime failures for the selected call.</CardDescription>
        </div>
        <Badge value={last?.event_type || (loading ? "loading" : "no_events")} />
      </div>

      {error ? <Alert title="Realtime events unavailable" message={error} /> : null}

      {loading ? (
        <div className="mt-5 rounded-md border border-line bg-slate-50 px-3 py-2 text-sm text-muted">
          Loading realtime events...
        </div>
      ) : null}

      {!loading && events.length === 0 ? (
        <div className="mt-5 rounded-md border border-line p-6 text-center">
          <MessageSquareText className="mx-auto h-5 w-5 text-muted" />
          <div className="mt-3 text-sm font-semibold text-ink">No realtime events recorded for this session.</div>
          <div className="mt-1 text-sm leading-6 text-muted">Events appear after Twilio starts or stops a realtime stream.</div>
        </div>
      ) : null}

      {events.length > 0 ? (
        <>
          <div className="mt-5 hidden overflow-x-auto rounded-md border border-line md:block">
            <table className="w-full min-w-[900px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Time</th>
                  <th className="px-4 py-3">Event</th>
                  <th className="px-4 py-3">Stage</th>
                  <th className="px-4 py-3">Stream SID</th>
                  <th className="px-4 py-3">Detail</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {events.map((event) => (
                  <tr key={event.id}>
                    <td className="px-4 py-3 text-muted">{formatDateTime(event.created_at)}</td>
                    <td className="px-4 py-3">
                      <Badge value={event.event_type} />
                    </td>
                    <td className="px-4 py-3 text-muted">{event.stage || "Not recorded"}</td>
                    <td className="px-4 py-3 font-mono text-xs text-muted">{event.stream_sid || "-"}</td>
                    <td className="max-w-[360px] break-words px-4 py-3 text-muted">{realtimeEventDetail(event)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-5 space-y-3 md:hidden">
            {events.map((event) => (
              <div key={event.id} className="rounded-md border border-line p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="text-sm font-semibold text-ink">{formatDateTime(event.created_at)}</div>
                  <Badge value={event.event_type} />
                </div>
                <dl className="mt-3 grid gap-3 text-sm">
                  <Info label="Stage" value={event.stage || "Not recorded"} />
                  <Info label="Stream SID" value={<span className="break-all font-mono text-xs">{event.stream_sid || "-"}</span>} />
                  <Info label="Detail" value={realtimeEventDetail(event)} />
                </dl>
              </div>
            ))}
          </div>
        </>
      ) : null}
    </Card>
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

function TranscriptBubble({
  item,
  canAddCorrection,
  onAddCorrection
}: {
  item: TranscriptMessage;
  canAddCorrection: boolean;
  onAddCorrection: () => void;
}) {
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
          {canAddCorrection ? (
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
          ) : null}
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

function SessionCard({
  item,
  busy,
  onOpen,
  onArchive,
  onRedact
}: {
  item: ConversationSession;
  busy: boolean;
  onOpen: () => void;
  onArchive: () => void;
  onRedact: () => void;
}) {
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
          <span className="text-muted">Action</span>
          <Badge value={bookingActionValue(item)} />
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-muted">Booking</span>
          <span className="font-medium text-ink">{item.booking_attempt_id || "None"}</span>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-muted">Retention</span>
          <span className="text-right font-medium text-ink">{retentionLabel(item)}</span>
        </div>
      </div>
      <div className="mt-4 grid gap-2 sm:grid-cols-3">
        <Button type="button" variant="secondary" onClick={onOpen}>
          Open
        </Button>
        <Button type="button" variant="secondary" onClick={onArchive} disabled={busy || item.lifecycle_status !== "active"}>
          <Archive className="h-4 w-4" />
          Archive
        </Button>
        <Button type="button" variant="danger" onClick={onRedact} disabled={busy || item.status === "active" || item.lifecycle_status === "redacted"}>
          <Eraser className="h-4 w-4" />
          Redact
        </Button>
      </div>
    </div>
  );
}

function SessionPaginationControls({
  className = "",
  count,
  filter,
  limit,
  offset,
  hasMore,
  busy,
  onPrevious,
  onNext,
  onLimitChange
}: {
  className?: string;
  count: number;
  filter: LifecycleFilter;
  limit: number;
  offset: number;
  hasMore: boolean;
  busy: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onLimitChange: (limit: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;

  return (
    <div
      className={`flex flex-col gap-3 rounded-md border border-line bg-slate-50 px-3 py-3 sm:flex-row sm:items-center sm:justify-between ${className}`}
    >
      <div className="text-sm leading-6 text-muted">{sessionRangeLabel(count, offset, hasMore, filter)}</div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <label className="flex items-center gap-2 text-sm text-muted">
          Rows per page
          <select
            className="h-9 rounded-md border border-line bg-white px-2 text-sm font-medium text-ink outline-none focus:border-brand disabled:text-slate-400"
            value={limit}
            onChange={(event) => onLimitChange(Number(event.target.value))}
            disabled={busy}
          >
            {sessionPageSizeOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <div className="flex items-center gap-2">
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onPrevious} disabled={busy || offset === 0}>
            <ChevronLeft className="h-4 w-4" />
            Previous
          </Button>
          <span className="min-w-16 text-center text-sm font-semibold text-ink">Page {page}</span>
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onNext} disabled={busy || !hasMore}>
            Next
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function PartyPaginationControls({
  className = "",
  count,
  status,
  limit,
  offset,
  hasMore,
  busy,
  onPrevious,
  onNext,
  onLimitChange
}: {
  className?: string;
  count: number;
  status: PartyStatusFilter;
  limit: number;
  offset: number;
  hasMore: boolean;
  busy: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onLimitChange: (limit: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;

  return (
    <div
      className={`flex flex-col gap-3 rounded-md border border-line bg-slate-50 px-3 py-3 sm:flex-row sm:items-center sm:justify-between ${className}`}
    >
      <div className="text-sm leading-6 text-muted">{partyRequestRangeLabel(count, offset, hasMore, status)}</div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <label className="flex items-center gap-2 text-sm text-muted">
          Rows per page
          <select
            className="h-9 rounded-md border border-line bg-white px-2 text-sm font-medium text-ink outline-none focus:border-brand disabled:text-slate-400"
            value={limit}
            onChange={(event) => onLimitChange(Number(event.target.value))}
            disabled={busy}
          >
            {partyRequestPageSizeOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <div className="flex items-center gap-2">
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onPrevious} disabled={busy || offset === 0}>
            <ChevronLeft className="h-4 w-4" />
            Previous
          </Button>
          <span className="min-w-16 text-center text-sm font-semibold text-ink">Page {page}</span>
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onNext} disabled={busy || !hasMore}>
            Next
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function sessionRangeLabel(count: number, offset: number, hasMore: boolean, filter: LifecycleFilter) {
  const label = filterLabel(filter).toLowerCase();
  if (count === 0) {
    return `No ${label} sessions`;
  }
  const start = offset + 1;
  const end = offset + count;
  const total = hasMore ? `at least ${end + 1}` : String(end);
  return `Showing ${start}-${end} of ${total} ${label} sessions`;
}

function partyRequestRangeLabel(count: number, offset: number, hasMore: boolean, status: PartyStatusFilter) {
  const label = partyStatusLabel(status).toLowerCase();
  if (count === 0) {
    return `No ${label} party requests`;
  }
  const start = offset + 1;
  const end = offset + count;
  const total = hasMore ? `at least ${end + 1}` : String(end);
  return `Showing ${start}-${end} of ${total} ${label} party requests`;
}

function countLabel(count: number, hasMore: boolean) {
  return hasMore ? `${count}+` : String(count);
}

function filterLabel(filter: LifecycleFilter) {
  if (filter === "active") return "Active";
  if (filter === "archived") return "Archived";
  return "Redacted";
}

function emptySessionsTitle(filter: LifecycleFilter) {
  if (filter === "archived") return "No archived sessions";
  if (filter === "redacted") return "No redacted sessions";
  return "No active call sessions";
}

function emptySessionsDescription(filter: LifecycleFilter) {
  if (filter === "archived") return "Archived sessions appear here after an owner hides them from the active review list.";
  if (filter === "redacted") return "Redacted sessions appear here after transcript and customer details are removed.";
  return "Archived or redacted sessions may still be available from the filters.";
}

function partyStatusLabel(status: PartyStatusFilter) {
  if (status === "contacted") return "Contacted";
  if (status === "resolved") return "Resolved";
  if (status === "dismissed") return "Dismissed";
  return "Pending";
}

function partyStatusSuccess(status: PartyStatusFilter) {
  if (status === "contacted") return "Party request marked contacted.";
  if (status === "resolved") return "Party request resolved. This did not create a confirmed appointment.";
  if (status === "dismissed") return "Party request dismissed.";
  return "Party request reopened.";
}

function partyTimeLabel(request: PartyBookingRequest) {
  const date = request.requested_date || "Date not collected";
  return request.requested_time_window ? `${date}, ${request.requested_time_window}` : date;
}

function partyServicesLabel(request: PartyBookingRequest) {
  const services = request.guest_service_requests ?? [];
  if (services.length === 0) {
    return "Services not collected";
  }
  return services
    .map((service) => [service.service_name || "Unknown service", service.notes].filter(Boolean).join(" - "))
    .join(", ");
}

function retentionLabel(session: ConversationSession) {
  if (session.lifecycle_status === "redacted") {
    return session.redacted_at ? `Transcript redacted ${formatDateTime(session.redacted_at)}` : "Transcript redacted";
  }
  if (session.lifecycle_status === "archived") {
    return session.archived_at ? `Archived ${formatDateTime(session.archived_at)}` : "Archived";
  }
  return session.retention_expires_at ? `Retains until ${formatRetentionDate(session.retention_expires_at)}` : "Retention not set";
}

function realtimeEventDetail(event: RealtimeEventLog) {
  if (event.redacted) return "Payload redacted by retention policy.";
  if (event.error) return event.error;
  if (event.stream_error) return event.stream_error;
  if (event.stream_event) return event.stream_event;
  return "No additional detail recorded.";
}

function formatDateTime(value: string) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit"
  }).format(new Date(value));
}

function formatRetentionDate(value: string) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric"
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

function bookingActionValue(session: ConversationSession) {
  if (session.booking_action === "reschedule") return "reschedule";
  if (session.booking_action === "cancel") return "cancel";
  return "book";
}

function squareConfirmationLabel(session: ConversationSession) {
  if (session.outcome === "booking_cancelled" && session.appointment_id) {
    return `Cancelled by Square Appointments (${session.appointment_id})`;
  }
  if (session.outcome === "booking_rescheduled" && session.appointment_id) {
    return `Rescheduled by Square Appointments (${session.appointment_id})`;
  }
  if (session.outcome === "booking_confirmed" && session.booking_attempt_id && session.appointment_id) {
    return `Confirmed by Square Appointments (${session.booking_attempt_id})`;
  }
  if (session.outcome === "booking_fallback_pending") {
    if (bookingActionValue(session) === "cancel") {
      return session.booking_attempt_id
        ? `Cancellation pending owner review (${session.booking_attempt_id})`
        : "Cancellation pending owner review";
    }
    if (bookingActionValue(session) === "reschedule") {
      return session.booking_attempt_id
        ? `Reschedule pending owner review (${session.booking_attempt_id})`
        : "Reschedule pending owner review";
    }
    return session.booking_attempt_id
      ? `Pending owner review (${session.booking_attempt_id})`
      : "Pending owner review";
  }
  return "Not confirmed";
}

function bookingNegotiationStatus(session: ConversationSession | null) {
  if (!session) return "not_started";
  if (session.outcome === "booking_cancelled" && session.appointment_id) {
    return "cancelled";
  }
  if (session.outcome === "booking_rescheduled" && session.appointment_id) {
    return "rescheduled";
  }
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
