"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { AlertTriangle, MessageSquareText, PhoneCall, Plus, RefreshCcw, Send } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  ConversationSession,
  POSConnection,
  Salon,
  SquareReadiness,
  SyncLog,
  TranscriptMessage
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

export function CallsDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [sessions, setSessions] = useState<ConversationSession[]>([]);
  const [selectedSession, setSelectedSession] = useState<ConversationSession | null>(null);
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [actionError, setActionError] = useState("");

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setSessions([]);
        setSelectedSession(null);
        return;
      }

      const [statusResponse, sessionsResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<SessionsResponse>(`/api/salons/${firstSalon.id}/conversation-sessions?limit=25`)
      ]);
      setStatus(statusResponse);
      setSessions(sessionsResponse.sessions);

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
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load call simulator.");
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
    setSending(true);
    try {
      let session = selectedSession;
      if (!session || session.status !== "active") {
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
    try {
      const detail = await apiRequest<ConversationSession>(
        `/api/salons/${salon.id}/conversation-sessions/${sessionID}`
      );
      setSelectedSession(detail);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not load simulator transcript.");
    }
  }

  async function reloadSessions(salonID: string) {
    const response = await apiRequest<SessionsResponse>(`/api/salons/${salonID}/conversation-sessions?limit=25`);
    setSessions(response.sessions);
  }

  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
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

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 md:grid-cols-4">
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
          Simulator sessions are scoped by salon, so the owner profile must exist first.
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
            Simulated AI receptionist sessions, transcripts, booking outcomes, and owner handoffs.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Calls unavailable" message={error} /> : null}
      {actionError ? <Alert title="Simulator action failed" message={actionError} /> : null}

      <ReadinessPanel aiEnabled={aiEnabled} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Simulator sessions" value={String(sessions.length)} />
        <Metric label="Owner handoffs" value={String(handoffCount)} />
        <Metric label="Confirmed bookings" value={String(confirmedCount)} />
        <Metric label="Pending requests" value={String(fallbackCount)} />
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.35fr_0.65fr]">
        <Card>
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <CardTitle>Conversation simulator</CardTitle>
              <CardDescription>
                Booking confirmation still requires a successful Square Appointments booking ID.
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
                  <TranscriptBubble key={item.id} item={item} />
                ))}
              </div>
            ) : (
              <EmptyTranscript />
            )}
          </div>

          <form className="mt-4 flex flex-col gap-3 sm:flex-row" onSubmit={sendMessage}>
            <label className="sr-only" htmlFor="customer-message">
              Customer message
            </label>
            <input
              id="customer-message"
              className="h-10 min-w-0 flex-1 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
              value={message}
              onChange={(event) => setMessage(event.target.value)}
              placeholder="Customer message"
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
              <Info label="Intent" value={<Badge value={selectedSession.intent} />} />
              <Info label="Outcome" value={<Badge value={selectedSession.outcome} />} />
              <Info label="Customer" value={selectedSession.customer_name || "Not collected"} />
              <Info label="Phone" value={selectedSession.customer_phone || "Not collected"} />
              <Info label="Service" value={selectedSession.service_name || "Not collected"} />
              <Info label="Staff" value={selectedSession.staff_name || "Not collected"} />
              <Info
                label="Requested time"
                value={selectedSession.requested_start_time ? formatDateTime(selectedSession.requested_start_time) : "Not collected"}
              />
              <Info label="Booking attempt" value={selectedSession.booking_attempt_id || "None"} />
            </dl>
          ) : (
            <div className="mt-5 rounded-md border border-line p-4 text-sm text-muted">
              No simulator session selected.
            </div>
          )}
        </Card>
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Recent simulator sessions</CardTitle>
            <CardDescription>
              Review deterministic simulator outcomes before live telephony is connected.
            </CardDescription>
          </div>
          <Badge value={sessions.length > 0 ? "active" : "disabled"} />
        </div>

        {sessions.length === 0 ? (
          <div className="mt-5 rounded-md border border-line p-6 text-center">
            <PhoneCall className="mx-auto h-5 w-5 text-muted" />
            <div className="mt-3 text-sm font-semibold text-ink">No simulator sessions yet</div>
            <div className="mt-1 text-sm leading-6 text-muted">
              Transcript rows will appear here after the first simulated customer message.
            </div>
          </div>
        ) : (
          <>
            <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[860px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Updated</th>
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

function ReadinessPanel({ aiEnabled }: { aiEnabled: boolean }) {
  if (aiEnabled) {
    return (
      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <CardTitle>AI booking simulator is active</CardTitle>
        <CardDescription className="text-emerald-800">
          Confirmed simulator bookings still require Square Appointments success through the booking service.
        </CardDescription>
      </Card>
    );
  }

  return (
    <Card className="border-amber-200 bg-amber-50 shadow-none">
      <div className="flex gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
        <div>
          <CardTitle>AI booking is gated</CardTitle>
          <CardDescription className="text-amber-900">
            Booking requests in simulator sessions will be routed to owner review until Square readiness checks pass.
          </CardDescription>
          <a
            className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/integrations"
          >
            Open Square integration
          </a>
        </div>
      </div>
    </Card>
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

function TranscriptBubble({ item }: { item: TranscriptMessage }) {
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
        <div className="mb-1 text-[11px] font-semibold uppercase opacity-75">{item.speaker}</div>
        {item.body}
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
        <Badge value={item.outcome} />
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
