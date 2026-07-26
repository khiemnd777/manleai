"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { BellRing, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
  getOwnerNotificationDelivery,
  listOwnerNotificationDeliveries,
  newOwnerNotificationDeliveryActionKey,
  requeueOwnerNotificationDelivery
} from "@/lib/api/owner-notification-deliveries";
import type { OwnerNotificationSurface } from "@/lib/api/owner-notification-deliveries";
import type {
  OwnerNotificationDelivery,
  OwnerNotificationDeliveryMetrics
} from "@/types/api";

const pageSize = 25;

export function OwnerNotificationDeliveries({ salonID, surface = "tenant" }: { salonID: string; surface?: OwnerNotificationSurface }) {
  const listRequestRef = useRef(0);
  const detailRequestRef = useRef(0);
  const requeueKeyRef = useRef("");
  const [rows, setRows] = useState<OwnerNotificationDelivery[]>([]);
  const [metrics, setMetrics] = useState<OwnerNotificationDeliveryMetrics | null>(null);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<OwnerNotificationDelivery | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [requeueing, setRequeueing] = useState(false);
  const [requeueError, setRequeueError] = useState("");
  const [success, setSuccess] = useState("");

  const load = useCallback(async () => {
    const requestID = ++listRequestRef.current;
    setLoading(true);
    setError("");
    try {
      const response = await listOwnerNotificationDeliveries(salonID, pageSize, offset, surface);
      if (requestID !== listRequestRef.current) return;
      setRows(response.deliveries);
      setMetrics(response.metrics);
      setHasMore(response.has_more);
    } catch (err) {
      if (requestID !== listRequestRef.current) return;
      setError(err instanceof Error ? err.message : "Could not load owner notification delivery.");
    } finally {
      if (requestID === listRequestRef.current) setLoading(false);
    }
  }, [offset, salonID, surface]);

  useEffect(() => { setOffset(0); }, [salonID]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => () => { listRequestRef.current += 1; detailRequestRef.current += 1; }, []);

  async function openDetail(row: OwnerNotificationDelivery) {
    setSelected(row);
    setDetailLoading(true);
    setDetailError("");
    setRequeueError("");
    requeueKeyRef.current = "";
    const requestID = ++detailRequestRef.current;
    try {
      const response = await getOwnerNotificationDelivery(salonID, row.id, surface);
      if (requestID === detailRequestRef.current) setSelected(response.delivery);
    } catch (err) {
      if (requestID === detailRequestRef.current) {
        setDetailError(err instanceof Error ? err.message : "Could not load delivery history.");
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
    requeueKeyRef.current = "";
  }

  async function requeue() {
    if (!selected?.can_requeue || selected.redacted || requeueing) return;
    if (!requeueKeyRef.current) requeueKeyRef.current = newOwnerNotificationDeliveryActionKey();
    setRequeueing(true);
    setRequeueError("");
    try {
      const response = await requeueOwnerNotificationDelivery(salonID, selected.id, requeueKeyRef.current, surface);
      setSelected(response.delivery);
      setSuccess("Delivery was safely requeued. Queued means waiting for the delivery worker, not delivered.");
      requeueKeyRef.current = "";
      await load();
    } catch (err) {
      setRequeueError(err instanceof Error ? err.message : "Could not safely requeue delivery.");
    } finally {
      setRequeueing(false);
    }
  }

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Owner notification delivery</CardTitle>
          <CardDescription>
            SMS delivery is separate from the in-app review queue. Queued or provider accepted does not mean delivered to the owner.
          </CardDescription>
        </div>
        <Button type="button" variant="secondary" disabled={loading} onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" /> Refresh
        </Button>
      </div>

      {metrics ? (
        <div className="mt-4 flex flex-wrap gap-2" aria-label="Owner delivery metrics">
          <Badge value={`queued ${metrics.queued}`} />
          <Badge value={`in progress ${metrics.delivering + metrics.provider_accepted + metrics.sent}`} />
          <Badge value={`delivered ${metrics.delivered}`} />
          <Badge value={`dead letter ${metrics.dead_letter}`} />
          <Badge value={`disabled ${metrics.disabled}`} />
        </div>
      ) : null}
      {success ? <div className="mt-4"><Alert type="success" title="Delivery action saved" message={success} /></div> : null}
      {loading && rows.length === 0 ? <div className="mt-5 space-y-3"><Skeleton className="h-16" /><Skeleton className="h-16" /></div> : null}
      {error ? <div className="mt-5 space-y-3" role="alert"><Alert title="Delivery history unavailable" message={error} /><Button type="button" variant="secondary" onClick={() => void load()}>Retry delivery history</Button></div> : null}
      {!loading && !error && rows.length === 0 ? (
        <div className="mt-5 rounded-md border border-dashed border-line bg-slate-50 p-8 text-center">
          <BellRing className="mx-auto h-6 w-6 text-muted" />
          <div className="mt-3 text-sm font-semibold text-ink">No owner delivery records</div>
          <div className="mt-1 text-sm text-muted">New in-app notifications appear here after the durable outbox row is created.</div>
        </div>
      ) : null}

      {rows.length > 0 ? (
        <div className="mt-5 space-y-3">
          {rows.map((row) => (
            <div key={row.id} className="flex flex-col justify-between gap-3 rounded-md border border-line bg-white p-4 sm:flex-row sm:items-center">
              <div className="min-w-0">
                <div className="flex flex-wrap gap-2"><Badge value={row.delivery_status} /><Badge value={notificationTypeLabel(row.notification_type)} />{row.redacted ? <Badge value="Redacted" /> : null}</div>
                <div className="mt-2 text-sm text-ink">SMS {row.redacted ? "personal details removed" : row.destination_masked || "destination hidden"} · {row.delivery_attempts} attempt{row.delivery_attempts === 1 ? "" : "s"}</div>
                <div className="mt-1 text-xs text-muted">Created {formatTimestamp(row.created_at)}{row.last_delivery_error_code ? ` · ${row.last_delivery_error_code}` : ""}</div>
              </div>
              <Button type="button" variant="secondary" className="w-full sm:w-auto" onClick={() => void openDetail(row)}>View delivery</Button>
            </div>
          ))}
          <div className="flex flex-col-reverse justify-between gap-3 border-t border-line pt-4 sm:flex-row sm:items-center">
            <div className="text-xs text-muted">Showing {offset + 1}–{offset + rows.length}</div>
            <div className="flex gap-2">
              <Button type="button" variant="secondary" className="flex-1 sm:flex-none" disabled={loading || offset === 0} onClick={() => setOffset((current) => Math.max(0, current - pageSize))}>Previous</Button>
              <Button type="button" variant="secondary" className="flex-1 sm:flex-none" disabled={loading || !hasMore} onClick={() => setOffset((current) => current + pageSize)}>Next</Button>
            </div>
          </div>
        </div>
      ) : null}

      <Dialog open={Boolean(selected)} title="Owner notification delivery" description="This history omits the message body, full phone number, credentials, and provider message ID." onClose={closeDetail} closeDisabled={requeueing} className="max-w-2xl">
        {selected ? (
          <div className="space-y-5">
            {detailLoading ? <Skeleton className="h-20" /> : null}
            {detailError ? <Alert title="Delivery detail unavailable" message={detailError} /> : null}
            {requeueError ? <Alert title="Delivery not requeued" message={requeueError} /> : null}
            {selected.redacted ? <Alert title="Personal details removed" message="The retention policy removed the message and destination. Delivery status, attempts, timestamps, and safe audit events remain available; requeue is disabled." /> : null}
            <dl className="grid grid-cols-[9rem_1fr] gap-x-3 gap-y-2 text-sm">
              <dt className="text-muted">Delivery status</dt><dd className="flex flex-wrap gap-2"><Badge value={selected.delivery_status} />{selected.redacted ? <Badge value="Redacted" /> : null}</dd>
              <dt className="text-muted">In-app status</dt><dd><Badge value={selected.in_app_status} /></dd>
              <dt className="text-muted">Destination</dt><dd>{selected.redacted ? "Removed by retention policy" : selected.destination_masked || "Hidden"}</dd>
              <dt className="text-muted">Attempts</dt><dd>{selected.delivery_attempts}</dd>
              <dt className="text-muted">Provider state</dt><dd>{selected.provider_status || "No provider acceptance evidence"}</dd>
            </dl>
            <div>
              <div className="text-sm font-semibold text-ink">Delivery history</div>
              {selected.events?.length ? (
                <ol className="mt-3 space-y-3">
                  {selected.events.map((event) => (
                    <li key={event.id} className="rounded-md border border-line bg-slate-50 p-3 text-sm">
                      <div className="flex flex-wrap gap-2"><Badge value={event.delivery_status} /><span className="font-medium text-ink">{eventLabel(event.event_type)}</span></div>
                      <div className="mt-1 text-xs text-muted">{formatTimestamp(event.created_at)}{event.error_code ? ` · ${event.error_code}` : ""}</div>
                    </li>
                  ))}
                </ol>
              ) : <div className="mt-2 text-sm text-muted">No delivery events have been recorded.</div>}
            </div>
            {selected.delivery_status === "dead_letter" ? (
              <div className="border-t border-line pt-4">
                {selected.can_requeue ? (
                  <Button type="button" variant="primary" className="w-full sm:w-auto" disabled={requeueing || detailLoading} onClick={() => void requeue()}>{requeueing ? "Requeueing…" : "Retry dead-letter delivery"}</Button>
                ) : (
                  <Alert title="Automatic retry blocked" message={selected.requeue_blocked_reason || "Manual provider verification is required before another send."} />
                )}
              </div>
            ) : null}
          </div>
        ) : null}
      </Dialog>
    </Card>
  );
}

function notificationTypeLabel(value: string) { return value.replaceAll("_", " "); }
function eventLabel(value: string) { return value.replaceAll("_", " "); }
function formatTimestamp(value: string) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "Unknown time" : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(parsed);
}
