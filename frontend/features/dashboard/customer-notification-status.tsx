"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  attestCustomerSMSConsent,
  getAppointmentCustomerNotifications,
  getRequestCustomerNotifications,
  newCustomerNotificationActionKey,
  requeueCustomerNotification,
  requeueRequestCustomerNotification,
  type CustomerNotificationDelivery,
  type CustomerNotificationDetail
} from "@/lib/api/customer-notifications";

export function CustomerNotificationStatus({
  salonID,
  appointmentID,
  requestID,
  customerPhone,
  redacted = false
}: {
  salonID: string;
  appointmentID?: string;
  requestID?: string;
  customerPhone: string;
  redacted?: boolean;
}) {
  const requestRef = useRef(0);
  const actionKeyRef = useRef("");
  const requeueKeysRef = useRef<Record<string, string>>({});
  const [detail, setDetail] = useState<CustomerNotificationDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [attested, setAttested] = useState(false);

  const load = useCallback(async () => {
    const requestSequence = ++requestRef.current;
    setLoading(true);
    setError("");
    try {
      const next = appointmentID
        ? await getAppointmentCustomerNotifications(salonID, appointmentID)
        : await getRequestCustomerNotifications(salonID, requestID || "");
      if (requestSequence === requestRef.current) {
        setDetail(next);
        requeueKeysRef.current = {};
      }
    } catch (loadError) {
      if (requestSequence === requestRef.current) setError(loadError instanceof Error ? loadError.message : "Could not load customer SMS status.");
    } finally {
      if (requestSequence === requestRef.current) setLoading(false);
    }
  }, [appointmentID, requestID, salonID]);

  useEffect(() => {
    setDetail(null);
    setAttested(false);
    setSuccess("");
    actionKeyRef.current = "";
    requeueKeysRef.current = {};
    void load();
    return () => { requestRef.current += 1; };
  }, [load]);

  async function recordAttestation() {
    if (!attested || busy) return;
    if (!actionKeyRef.current) actionKeyRef.current = newCustomerNotificationActionKey("customer-consent-attestation");
    setBusy("attest");
    setError("");
    try {
      await attestCustomerSMSConsent(salonID, { destination: customerPhone, attested: true, actionKey: actionKeyRef.current });
      setSuccess("Explicit consent was recorded by the signed-in owner. Consent alone does not prove any message was delivered.");
      setAttested(false);
      actionKeyRef.current = "";
      await load();
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : "Could not record explicit customer consent.");
    } finally {
      setBusy("");
    }
  }

  async function requeue(delivery: CustomerNotificationDelivery) {
    if (!delivery.can_requeue || busy) return;
    const actionKey = requeueKeysRef.current[delivery.id] || newCustomerNotificationActionKey("customer-notification-requeue");
    requeueKeysRef.current[delivery.id] = actionKey;
    setBusy(delivery.id);
    setError("");
    try {
      const next = appointmentID
        ? await requeueCustomerNotification(salonID, appointmentID, delivery.id, actionKey)
        : await requeueRequestCustomerNotification(salonID, requestID || "", delivery.id, actionKey);
      setDetail(next);
      delete requeueKeysRef.current[delivery.id];
      setSuccess("The dead-letter delivery was safely requeued. Queued does not mean delivered.");
    } catch (actionError) {
      setError(actionError instanceof Error ? actionError.message : "Could not safely requeue the delivery.");
    } finally {
      setBusy("");
    }
  }

  const consent = detail?.consent;
  const mayAttest = !redacted && Boolean(customerPhone) && (!consent || (consent.status !== "consented" && consent.status !== "opted_out"));
  return (
    <section className="mt-5 border-t border-line pt-5" aria-label="Customer SMS consent and delivery">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Customer SMS</div>
          <div className="mt-1 text-sm leading-6 text-muted">Consent and delivery are separate. Full phone numbers, message bodies, and provider message IDs are not shown here.</div>
        </div>
        <Button type="button" variant="secondary" className="w-full sm:w-auto" disabled={loading || Boolean(busy)} onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" /> Refresh
        </Button>
      </div>

      {loading && !detail ? <div className="mt-4 space-y-3"><Skeleton className="h-16" /><Skeleton className="h-20" /></div> : null}
      {error ? <div className="mt-4"><Alert title="Customer SMS action unavailable" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Customer SMS updated" message={success} /></div> : null}

      {detail ? (
        <div className="mt-4 space-y-4">
          <div className="rounded-md border border-line bg-slate-50 p-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="text-sm font-medium text-ink">Consent</div>
              <Badge value={consentStatusLabel(consent?.status)} />
            </div>
            <div className="mt-2 text-sm text-muted">Destination: {consent?.destination_masked || "Hidden"}</div>
            {consent?.source === "owner_attested" ? <div className="mt-1 text-xs text-muted">Recorded by the signed-in owner at {formatTimestamp(consent.updated_at)}.</div> : null}
            {consent?.status === "opted_out" ? <div className="mt-2 text-sm leading-6 text-amber-900">This destination opted out. Owner attestation cannot override STOP; only a signed Twilio START event can restore consent.</div> : null}
          </div>

          {mayAttest ? (
            <div className="rounded-md border border-line bg-white p-4">
              <label className="flex items-start gap-3">
                <input type="checkbox" className="mt-1 h-4 w-4" checked={attested} disabled={Boolean(busy)} onChange={(event) => setAttested(event.target.checked)} />
                <span className="text-sm leading-6 text-ink">I personally obtained explicit consent from this customer to receive appointment SMS at the phone number already stored on this {appointmentID ? "appointment" : "request"}.</span>
              </label>
              <Button type="button" className="mt-3 w-full sm:w-auto" disabled={!attested || Boolean(busy)} onClick={() => void recordAttestation()}>
                {busy === "attest" ? "Recording…" : "Record owner attestation"}
              </Button>
            </div>
          ) : null}

          <div>
            <div className="text-sm font-semibold text-ink">Delivery history</div>
            {detail.deliveries.length === 0 ? (
              <div className="mt-2 rounded-md border border-dashed border-line p-4 text-sm text-muted">No customer SMS has been queued for this {appointmentID ? "appointment" : "request"}.</div>
            ) : (
              <div className="mt-3 space-y-3">
                {detail.deliveries.map((delivery) => (
                  <div key={delivery.id} className="rounded-md border border-line bg-white p-4">
                    <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
                      <div>
                        <div className="flex flex-wrap gap-2"><Badge value={deliveryStatusLabel(delivery.delivery_status)} /><Badge value={delivery.notification_type.replaceAll("_", " ")} /></div>
                        <div className="mt-2 text-sm text-muted">{delivery.destination_masked || "Destination hidden"} · {delivery.delivery_attempts} attempt{delivery.delivery_attempts === 1 ? "" : "s"}</div>
                        <div className="mt-1 text-xs text-muted">Created {formatTimestamp(delivery.created_at)}{delivery.last_delivery_error_code ? ` · ${delivery.last_delivery_error_code}` : ""}</div>
                      </div>
                      {delivery.delivery_status === "dead_letter" ? (
                        delivery.can_requeue ? (
                          <Button type="button" variant="secondary" className="w-full sm:w-auto" disabled={Boolean(busy)} onClick={() => void requeue(delivery)}>{busy === delivery.id ? "Requeueing…" : "Retry delivery"}</Button>
                        ) : <div className="text-xs leading-5 text-muted">Retry blocked: {delivery.requeue_blocked_reason || "manual verification required"}</div>
                      ) : null}
                    </div>
                    {delivery.events?.length ? (
                      <ol className="mt-3 space-y-2 border-t border-line pt-3">
                        {delivery.events.map((event, index) => (
                          <li key={`${event.created_at}:${index}`} className="text-xs leading-5 text-muted">{formatTimestamp(event.created_at)} · {event.event_type.replaceAll("_", " ")} · {deliveryStatusLabel(event.delivery_status)}{event.error_code ? ` · ${event.error_code}` : ""}</li>
                        ))}
                      </ol>
                    ) : null}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : null}
    </section>
  );
}

function consentStatusLabel(status?: string) {
  if (!status) return "Not requested";
  if (status === "pending") return "Waiting consent";
  if (status === "opted_out") return "Opted out";
  if (status === "consented") return "Consented";
  if (status === "declined") return "Declined";
  return "Not requested";
}

function deliveryStatusLabel(status: string) {
  if (status === "delivered") return "Delivered";
  if (status === "quiet_hours") return "Quiet hours";
  if (status === "suppressed") return "Suppressed";
  if (status === "provider_accepted") return "Accepted by provider";
  if (status === "sent") return "Sent — delivery pending";
  if (status === "delivering") return "Dispatching";
  if (["failed", "dead_letter", "undelivered"].includes(status)) return "Failed";
  if (status === "queued") return "Queued";
  return "Not requested";
}

function formatTimestamp(value: string) {
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? "Unknown time" : new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(parsed);
}
