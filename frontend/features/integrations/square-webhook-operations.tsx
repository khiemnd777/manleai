"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Activity, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { RequestError } from "@/lib/api/client";
import {
  getSquareWebhookEvent,
  listSquareWebhookEvents,
  newSquareWebhookActionKey,
  requeueSquareWebhookEvent,
  SQUARE_WEBHOOK_STATUS_FILTERS
} from "@/lib/api/square-webhook-events";
import type { SquareWebhookListStatus } from "@/lib/api/square-webhook-events";
import type {
  SquareCalendarRepairHealth,
  SquareWebhookEvent,
  SquareWebhookMetrics
} from "@/types/api";

const pageSize = 25;

type SquareWebhookOperationsProps = {
  salonID: string;
  enabled: boolean;
  webhookConfigured: boolean;
  surface?: "tenant" | "platform";
};

export function SquareWebhookOperations({
  salonID,
  enabled,
  webhookConfigured,
  surface = "tenant"
}: SquareWebhookOperationsProps) {
  const listRequestRef = useRef(0);
  const detailRequestRef = useRef(0);
  const requeueInFlightRef = useRef(false);
  const requeueIntentRef = useRef<{ eventID: string; actionKey: string } | null>(null);
  const [rows, setRows] = useState<SquareWebhookEvent[]>([]);
  const [metrics, setMetrics] = useState<SquareWebhookMetrics | null>(null);
  const [calendarRepair, setCalendarRepair] = useState<SquareCalendarRepairHealth | null>(null);
  const [status, setStatus] = useState<SquareWebhookListStatus>("");
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(webhookConfigured);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<SquareWebhookEvent | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [requeueing, setRequeueing] = useState(false);
  const [requeueError, setRequeueError] = useState("");
  const [requeueUnknown, setRequeueUnknown] = useState(false);
  const [success, setSuccess] = useState("");

  const load = useCallback(async () => {
    if (!enabled || !webhookConfigured) {
      setLoading(false);
      return;
    }
    const requestID = ++listRequestRef.current;
    setLoading(true);
    setError("");
    try {
      const response = await listSquareWebhookEvents(salonID, status, pageSize, offset, surface);
      if (requestID !== listRequestRef.current) return;
      setRows(response.events);
      setMetrics(response.metrics);
      setCalendarRepair(response.calendar_repair);
      setHasMore(response.has_more);
    } catch (err) {
      if (requestID !== listRequestRef.current) return;
      setError(err instanceof Error ? err.message : "Could not load Square webhook operations.");
    } finally {
      if (requestID === listRequestRef.current) setLoading(false);
    }
  }, [enabled, offset, salonID, status, surface, webhookConfigured]);

  useEffect(() => {
    listRequestRef.current += 1;
    detailRequestRef.current += 1;
    requeueIntentRef.current = null;
    setRows([]);
    setMetrics(null);
    setCalendarRepair(null);
    setStatus("");
    setOffset(0);
    setHasMore(false);
    setSelected(null);
    setRequeueUnknown(false);
    setSuccess("");
  }, [salonID]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(
    () => () => {
      listRequestRef.current += 1;
      detailRequestRef.current += 1;
    },
    []
  );

  if (!enabled) return null;

  async function openDetail(row: SquareWebhookEvent) {
    if (requeueIntentRef.current?.eventID !== row.id) {
      requeueIntentRef.current = null;
      setRequeueUnknown(false);
    }
    setSelected(row);
    setDetailLoading(true);
    setDetailError("");
    setRequeueError("");
    const requestID = ++detailRequestRef.current;
    try {
      const response = await getSquareWebhookEvent(salonID, row.id, surface);
      if (requestID === detailRequestRef.current) setSelected(response.event);
    } catch (err) {
      if (requestID === detailRequestRef.current) {
        setDetailError(err instanceof Error ? err.message : "Could not load webhook event detail.");
      }
    } finally {
      if (requestID === detailRequestRef.current) setDetailLoading(false);
    }
  }

  function closeDetail() {
    if (requeueing) return;
    detailRequestRef.current += 1;
    setSelected(null);
    setDetailError("");
    setRequeueError("");
    if (!requeueUnknown) requeueIntentRef.current = null;
  }

  async function requeue() {
    if (!selected || requeueInFlightRef.current || requeueing || (!selected.can_requeue && !requeueUnknown)) return;
    if (requeueIntentRef.current?.eventID !== selected.id) {
      requeueIntentRef.current = {
        eventID: selected.id,
        actionKey: newSquareWebhookActionKey()
      };
    }
    const intent = requeueIntentRef.current;
    requeueInFlightRef.current = true;
    setRequeueing(true);
    setRequeueError("");
    try {
      const result = await requeueSquareWebhookEvent(salonID, selected.id, intent.actionKey, surface);
      setSelected(result.event);
      setRequeueUnknown(false);
      requeueIntentRef.current = null;
      setSuccess(
        result.replayed
          ? "The exact webhook requeue was recovered from its saved action. No second requeue was created."
          : "The webhook event was safely requeued for the worker."
      );
      await load();
    } catch (err) {
      if (err instanceof RequestError && err.status < 500) {
        setRequeueUnknown(false);
        requeueIntentRef.current = null;
        setRequeueError(err.message);
      } else {
        setRequeueUnknown(true);
        setRequeueError(
          "The requeue result is unknown. Retry the exact action to recover its saved result; do not start a new requeue."
        );
      }
    } finally {
      requeueInFlightRef.current = false;
      setRequeueing(false);
    }
  }

  return (
    <section className="mt-6 border-t border-line pt-6" aria-labelledby="square-webhook-operations-title">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 id="square-webhook-operations-title" className="text-sm font-semibold text-ink">
              Webhook operations
            </h3>
            <Badge value={webhookConfigured ? "configured" : "disabled"} />
          </div>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted">
            Monitor safe Square booking-event processing and calendar repair evidence. Webhook delivery alone never confirms an appointment.
          </p>
        </div>
        {webhookConfigured ? (
          <Button type="button" variant="secondary" disabled={loading} onClick={() => void load()}>
            <RefreshCcw className="h-4 w-4" /> Refresh operations
          </Button>
        ) : null}
      </div>

      {!webhookConfigured ? (
        <div className="mt-4">
          <Alert
            title="Webhook operations unavailable"
            message="Save the HTTPS notification URL and write-only signature key in the Square configuration above. Configured verification does not prove that Square is delivering events."
          />
        </div>
      ) : null}

      {webhookConfigured && metrics ? <WebhookMetrics metrics={metrics} /> : null}
      {webhookConfigured && calendarRepair?.relevant ? <CalendarRepair health={calendarRepair} /> : null}
      {webhookConfigured && success ? (
        <div className="mt-4">
          <Alert type="success" title="Webhook action saved" message={success} />
        </div>
      ) : null}

      {webhookConfigured ? (
        <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <label className="block text-sm font-medium text-ink">
            Processing status
            <select
              className="mt-1 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink sm:w-56"
              value={status}
              disabled={loading}
              onChange={(event) => {
                listRequestRef.current += 1;
                setRows([]);
                setHasMore(false);
                setOffset(0);
                setStatus(event.target.value as SquareWebhookListStatus);
              }}
            >
              <option value="">All statuses</option>
              {SQUARE_WEBHOOK_STATUS_FILTERS.map((item) => (
                <option key={item} value={item}>
                  {statusLabel(item)}
                </option>
              ))}
            </select>
          </label>
          <div className="text-xs text-muted">Newest received events first · 25 per page</div>
        </div>
      ) : null}

      {webhookConfigured && loading && rows.length === 0 ? (
        <div className="mt-4 space-y-3">
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
        </div>
      ) : null}
      {webhookConfigured && error ? (
        <div className="mt-4 space-y-3" role="alert">
          <Alert title="Webhook operations unavailable" message={error} />
          <Button type="button" variant="secondary" onClick={() => void load()}>
            Retry webhook operations
          </Button>
        </div>
      ) : null}
      {webhookConfigured && !loading && !error && rows.length === 0 ? (
        <div className="mt-4 rounded-md border border-dashed border-line bg-slate-50 p-8 text-center">
          <Activity className="mx-auto h-6 w-6 text-muted" />
          <div className="mt-3 text-sm font-semibold text-ink">No webhook events in this view</div>
          <div className="mt-1 text-sm text-muted">
            {status ? `No events currently have ${statusLabel(status)} status.` : "No Square booking events have been recorded for this salon."}
          </div>
        </div>
      ) : null}

      {webhookConfigured && rows.length > 0 ? (
        <>
          <WebhookEventTable rows={rows} onOpen={openDetail} />
          <div className="mt-4 flex flex-col-reverse justify-between gap-3 border-t border-line pt-4 sm:flex-row sm:items-center">
            <div className="text-xs text-muted">
              Showing {offset + 1}–{offset + rows.length}
            </div>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="secondary"
                className="flex-1 sm:flex-none"
                disabled={loading || offset === 0}
                onClick={() => {
                  setRows([]);
                  setHasMore(false);
                  setOffset((current) => Math.max(0, current - pageSize));
                }}
              >
                Previous
              </Button>
              <Button
                type="button"
                variant="secondary"
                className="flex-1 sm:flex-none"
                disabled={loading || !hasMore}
                onClick={() => {
                  setRows([]);
                  setHasMore(false);
                  setOffset((current) => current + pageSize);
                }}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      ) : null}

      <Dialog
        open={Boolean(selected)}
        title="Square webhook event"
        description="This view omits provider identifiers, raw payloads, signatures, tokens, and raw errors."
        onClose={closeDetail}
        closeDisabled={requeueing}
        className="max-w-2xl"
      >
        {selected ? (
          <WebhookEventDetail
            event={selected}
            detailError={detailError}
            detailLoading={detailLoading}
            requeueError={requeueError}
            requeueUnknown={requeueUnknown}
            requeueing={requeueing}
            onRequeue={() => void requeue()}
          />
        ) : null}
      </Dialog>
    </section>
  );
}

function WebhookMetrics({ metrics }: { metrics: SquareWebhookMetrics }) {
  return (
    <div className="mt-4" aria-label="Square webhook metrics">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="Pending" value={metrics.pending} />
        <Metric label="Processing" value={metrics.processing} />
        <Metric label="Failed" value={metrics.failed} />
        <Metric label="Dead letter" value={metrics.dead_letter} />
        <Metric label={`Succeeded · ${metrics.recent_window_hours}h`} value={metrics.succeeded_recent} />
      </div>
      <div className="mt-2 text-xs text-muted">
        Last received {formatTimestamp(metrics.last_delivered_at)} · Last succeeded {formatTimestamp(metrics.last_succeeded_at)}
      </div>
    </div>
  );
}

function CalendarRepair({ health }: { health: SquareCalendarRepairHealth }) {
  return (
    <div className="mt-4 rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-center">
        <div>
          <div className="text-sm font-semibold text-ink">Calendar repair backstop</div>
          <div className="mt-1 text-xs text-muted">
            Last repaired {formatTimestamp(health.last_repaired_at)} · {health.repair_attempts} attempt{health.repair_attempts === 1 ? "" : "s"}
          </div>
          {health.next_repair_at || health.lease_expires_at ? (
            <div className="mt-1 text-xs text-muted">
              Next repair {formatTimestamp(health.next_repair_at)} · Lease expires {formatTimestamp(health.lease_expires_at)}
            </div>
          ) : null}
        </div>
        <Badge value={health.status} />
      </div>
      {health.last_error_code ? (
        <div className="mt-2 text-xs text-red-700">
          {health.last_error_class || "dependency"} · {health.last_error_code}
        </div>
      ) : null}
    </div>
  );
}

function WebhookEventTable({
  rows,
  onOpen
}: {
  rows: SquareWebhookEvent[];
  onOpen: (event: SquareWebhookEvent) => void;
}) {
  return (
    <div className="mt-4">
      <div className="hidden overflow-x-auto rounded-md border border-line md:block">
        <table className="w-full min-w-[720px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase tracking-wide text-muted">
            <tr>
              <th className="px-4 py-3">Received</th>
              <th className="px-4 py-3">Event</th>
              <th className="px-4 py-3">Status</th>
              <th className="px-4 py-3">Attempts</th>
              <th className="px-4 py-3 text-right">Action</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {rows.map((row) => (
              <tr key={row.id}>
                <td className="px-4 py-3 text-muted">{formatTimestamp(row.delivered_at || row.created_at)}</td>
                <td className="px-4 py-3 font-medium text-ink">{eventTypeLabel(row.event_type)}</td>
                <td className="px-4 py-3"><Badge value={row.processing_status} /></td>
                <td className="px-4 py-3 text-muted">{row.processing_attempts} processing · {row.requeue_count} requeue</td>
                <td className="px-4 py-3 text-right">
                  <Button type="button" variant="secondary" onClick={() => void onOpen(row)}>View event</Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="space-y-3 md:hidden">
        {rows.map((row) => (
          <div key={row.id} className="rounded-md border border-line bg-white p-4">
            <div className="flex flex-wrap items-start justify-between gap-2">
              <div className="font-medium text-ink">{eventTypeLabel(row.event_type)}</div>
              <Badge value={row.processing_status} />
            </div>
            <div className="mt-2 text-xs text-muted">Received {formatTimestamp(row.delivered_at || row.created_at)}</div>
            <div className="mt-1 text-xs text-muted">{row.processing_attempts} processing attempts · {row.requeue_count} requeues</div>
            <Button type="button" variant="secondary" className="mt-3 w-full" onClick={() => void onOpen(row)}>
              View event
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}

function WebhookEventDetail({
  event,
  detailError,
  detailLoading,
  requeueError,
  requeueUnknown,
  requeueing,
  onRequeue
}: {
  event: SquareWebhookEvent;
  detailError: string;
  detailLoading: boolean;
  requeueError: string;
  requeueUnknown: boolean;
  requeueing: boolean;
  onRequeue: () => void;
}) {
  const timeline = webhookTimeline(event);
  return (
    <div className="space-y-5">
      {detailLoading ? <Skeleton className="h-20" /> : null}
      {detailError ? <Alert title="Webhook detail unavailable" message={detailError} /> : null}
      {requeueError ? (
        <Alert title={requeueUnknown ? "Webhook requeue result unknown" : "Webhook not requeued"} message={requeueError} />
      ) : null}
      <dl className="grid grid-cols-[8.5rem_1fr] gap-x-3 gap-y-2 text-sm">
        <dt className="text-muted">Event type</dt><dd className="break-words text-ink">{eventTypeLabel(event.event_type)}</dd>
        <dt className="text-muted">Status</dt><dd><Badge value={event.processing_status} /></dd>
        <dt className="text-muted">Processing attempts</dt><dd>{event.processing_attempts}</dd>
        <dt className="text-muted">Owner requeues</dt><dd>{event.requeue_count}</dd>
        <dt className="text-muted">Next attempt</dt><dd>{formatTimestamp(event.next_attempt_at)}</dd>
        <dt className="text-muted">Safe diagnostic</dt>
        <dd className="break-words">{event.last_error_code ? `${event.last_error_class || "dependency"} · ${event.last_error_code}` : "None"}</dd>
      </dl>

      <div>
        <div className="text-sm font-semibold text-ink">Available timeline evidence</div>
        <ol className="mt-3 space-y-3">
          {timeline.map((item) => (
            <li key={item.key} className="rounded-md border border-line bg-slate-50 p-3 text-sm">
              <div className="font-medium text-ink">{item.label}</div>
              <div className="mt-1 text-xs text-muted">{formatTimestamp(item.at)}</div>
            </li>
          ))}
        </ol>
      </div>

      <div className="border-t border-line pt-4">
        {event.can_requeue || requeueUnknown ? (
          <Button type="button" className="w-full sm:w-auto" disabled={requeueing || detailLoading} onClick={onRequeue}>
            {requeueing ? "Requeueing…" : requeueUnknown ? "Retry exact requeue" : "Requeue failed event"}
          </Button>
        ) : (
          <Alert
            title={event.processing_status === "ignored" ? "No replay needed" : "Requeue unavailable"}
            message={event.requeue_blocked_reason || "The backend has not authorized a safe requeue for this event."}
          />
        )}
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md border border-line bg-white p-3">
      <div className="text-xs font-medium text-muted">{label}</div>
      <div className="mt-1 text-xl font-semibold text-ink">{value.toLocaleString()}</div>
    </div>
  );
}

function webhookTimeline(event: SquareWebhookEvent) {
  return [
    event.delivered_at ? { key: "delivered", label: "Received by ManleAI", at: event.delivered_at } : null,
    { key: "created", label: "Durable event recorded", at: event.created_at },
    event.processed_at ? { key: "processed", label: "Processing completed", at: event.processed_at } : null,
    event.dead_lettered_at ? { key: "dead-lettered", label: "Moved to dead letter", at: event.dead_lettered_at } : null,
    { key: "updated", label: "Operational state last updated", at: event.updated_at }
  ].filter((item): item is { key: string; label: string; at: string } => Boolean(item));
}

function statusLabel(value: string) {
  return value.replaceAll("_", " ");
}

function eventTypeLabel(value: string) {
  return value.replaceAll(".", " · ").replaceAll("_", " ");
}

function formatTimestamp(value?: string) {
  if (!value) return "Not recorded";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime())
    ? "Invalid timestamp"
    : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(parsed);
}
