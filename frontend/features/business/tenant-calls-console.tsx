"use client";

import { useCallback, useEffect, useState } from "react";
import { Archive, PhoneCall, RefreshCcw, ShieldX } from "lucide-react";
import { useTenantSalon } from "@/components/layout/tenant-salon-context";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type { ConversationSession } from "@/types/api";

type SessionsResponse = { sessions: ConversationSession[]; has_more?: boolean };

export function TenantCallsConsole() {
  const { activeSalon, loading: salonLoading, error: salonError } = useTenantSalon();
  const [sessions, setSessions] = useState<ConversationSession[]>([]);
  const [selected, setSelected] = useState<ConversationSession | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    if (!activeSalon) return;
    setLoading(true);
    setError("");
    try {
      const result = await apiRequest<SessionsResponse>(`/api/salons/${activeSalon.id}/conversation-sessions?limit=100`);
      setSessions(result.sessions ?? []);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load salon calls.");
    } finally {
      setLoading(false);
    }
  }, [activeSalon]);

  useEffect(() => { void load(); }, [load]);

  async function openSession(item: ConversationSession) {
    if (!activeSalon) return;
    setSelected(item);
    setBusy(`detail-${item.id}`);
    setError("");
    try {
      setSelected(await apiRequest<ConversationSession>(`/api/salons/${activeSalon.id}/conversation-sessions/${item.id}`));
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load the call transcript.");
    } finally {
      setBusy("");
    }
  }

  async function lifecycleAction(item: ConversationSession, action: "archive" | "redact") {
    if (!activeSalon) return;
    if (action === "redact" && !window.confirm("Redact this call? Personal transcript and customer details cannot be restored.")) return;
    setBusy(`${action}-${item.id}`);
    setError("");
    try {
      const result = await apiRequest<ConversationSession>(`/api/salons/${activeSalon.id}/conversation-sessions/${item.id}/${action}`, { method: "POST" });
      setSelected(result);
      await load();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : `Could not ${action} this call.`);
    } finally {
      setBusy("");
    }
  }

  if (salonLoading || loading) return <div className="space-y-4"><Skeleton className="h-20" /><Skeleton className="h-72" /></div>;
  if (salonError || !activeSalon) return <Alert title="Salon unavailable" message={salonError || "No active salon is available."} />;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div><h1 className="text-2xl font-bold text-ink">Calls</h1><p className="mt-1 text-sm text-muted">Review customer conversations for {activeSalon.name}. Provider diagnostics and voice configuration are available only to Platform Ops.</p></div>
        <Button type="button" variant="secondary" onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Refresh</Button>
      </div>
      {error ? <Alert title="Call history needs attention" message={error} /> : null}
      {!sessions.length ? <Alert title="No calls" message="Customer call sessions for this salon will appear here." /> : <div className="space-y-3">{sessions.map((item) => <Card key={item.id}><div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div className="min-w-0"><div className="flex flex-wrap gap-2"><Badge value={item.lifecycle_status} /><Badge value={item.outcome || item.status} />{item.scheduling_result_evidence?.result_status ? <Badge value={item.scheduling_result_evidence.result_status} /> : null}</div><CardTitle className="mt-3">{item.customer_name || item.customer_phone || "Customer call"}</CardTitle><CardDescription>{new Date(item.started_at).toLocaleString()} · {item.summary || item.intent || "Conversation recorded"}</CardDescription></div><Button type="button" variant="secondary" onClick={() => void openSession(item)}><PhoneCall className="h-4 w-4" />View conversation</Button></div></Card>)}</div>}
      <Dialog open={Boolean(selected)} title="Customer conversation" description="Business transcript and scheduling outcome for this salon. Technical provider events are intentionally omitted." onClose={() => setSelected(null)} closeDisabled={Boolean(busy)} className="max-w-3xl">
        {selected ? <div className="space-y-5"><div className="flex flex-wrap gap-2"><Badge value={selected.lifecycle_status} /><Badge value={selected.outcome || selected.status} /></div><dl className="grid gap-3 text-sm sm:grid-cols-2"><Info label="Customer" value={selected.customer_name || "Not captured"} /><Info label="Phone" value={selected.customer_phone || "Not captured"} /><Info label="Service" value={selected.service_name || "Not selected"} /><Info label="Staff" value={selected.staff_name || "Not selected"} /><Info label="Started" value={new Date(selected.started_at).toLocaleString()} /><Info label="Scheduling result" value={selected.scheduling_result_evidence?.result_status || "No scheduling result"} /></dl><div><div className="text-sm font-semibold text-ink">Transcript</div>{busy === `detail-${selected.id}` ? <Skeleton className="mt-3 h-32" /> : selected.transcript?.length ? <ol className="mt-3 space-y-2">{selected.transcript.map((message) => <li key={message.id} className="rounded-md border border-line bg-slate-50 p-3"><div className="text-xs font-bold uppercase text-muted">{message.speaker}</div><div className="mt-1 whitespace-pre-wrap text-sm text-ink">{message.body}</div></li>)}</ol> : <p className="mt-2 text-sm text-muted">No transcript is available.</p>}</div><div className="flex flex-col gap-2 border-t border-line pt-4 sm:flex-row sm:justify-end">{selected.lifecycle_status === "active" ? <Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => void lifecycleAction(selected, "archive")}><Archive className="h-4 w-4" />Archive</Button> : null}{selected.lifecycle_status !== "redacted" ? <Button type="button" variant="danger" disabled={Boolean(busy)} onClick={() => void lifecycleAction(selected, "redact")}><ShieldX className="h-4 w-4" />Redact personal data</Button> : null}</div></div> : null}
      </Dialog>
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) { return <div><dt className="text-xs font-semibold uppercase text-muted">{label}</dt><dd className="mt-1 font-medium text-ink">{value}</dd></div>; }
