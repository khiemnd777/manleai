"use client";

import { useCallback, useEffect, useState } from "react";
import { RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest, RequestError } from "@/lib/api/client";
import { SquareWebhookOperations } from "@/features/integrations/square-webhook-operations";
import { OwnerNotificationDeliveries } from "@/features/dashboard/owner-notification-deliveries";
import { TenantRuntimeControls } from "@/features/platform/tenant-runtime-controls";
import type { OperationsHealthResponse } from "@/types/api";

export function PlatformOperationsConsole({ tenantID }: { tenantID: string }) {
	const [section, setSection] = useState<"health" | "limits" | "webhooks" | "notifications">("health");
  const [status, setStatus] = useState<OperationsHealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [blocked, setBlocked] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    setBlocked(false);
    try {
      const response = await apiRequest<{data: OperationsHealthResponse}>(`/api/v2/platform/tenants/${encodeURIComponent(tenantID)}/operations/overview`);
      setStatus(response.data);
    } catch (failure) {
      if (failure instanceof RequestError && failure.status === 403) setBlocked(true);
      else setError(failure instanceof Error ? failure.message : "Could not load tenant operations health.");
    } finally {
      setLoading(false);
    }
  }, [tenantID]);

  useEffect(() => { void load(); }, [load]);

  const choices = [
    ["health", "Health & queues"], ["limits", "Runtime limits"], ["webhooks", "Square webhooks"], ["notifications", "Owner notifications"]
  ] as const;

  return <div className="space-y-5">
    <div><h2 className="text-lg font-bold text-ink">Operations</h2><p className="mt-1 text-sm text-muted">Inspect one operational workflow at a time. Provider payloads and customer data stay outside health summaries.</p></div>
    <nav className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4" aria-label="Operations workflows">{choices.map(([key,label])=><button key={key} type="button" onClick={()=>setSection(key)} className={`rounded-lg border p-3 text-left text-sm font-semibold shadow-soft ${section===key?"border-teal-300 bg-teal-50 text-brand":"border-line bg-white text-slate-700 hover:border-teal-200"}`}>{label}</button>)}</nav>
    {section === "health" ? <HealthPanel status={status} loading={loading} blocked={blocked} error={error} onReload={load}/> : null}
    {section === "limits" ? <TenantRuntimeControls tenantID={tenantID}/> : null}
    {section === "webhooks" ? <Card><SquareWebhookOperations salonID={tenantID} enabled surface="platform"/></Card> : null}
    {section === "notifications" ? <OwnerNotificationDeliveries salonID={tenantID} surface="platform"/> : null}
  </div>;
}

function HealthPanel({status,loading,blocked,error,onReload}:{status:OperationsHealthResponse|null;loading:boolean;blocked:boolean;error:string;onReload:()=>Promise<void>}) {
  if (loading) return <div className="space-y-4"><Skeleton className="h-24"/><Skeleton className="h-72"/></div>;
  if (blocked) return <Alert title="Queue health grants required" message="Queue health needs operations.read plus active calls, appointments, and notifications grants for this salon."/>;
  if (error) return <Alert title="Operations health unavailable" message={error}/>;
  if (!status) return <Alert title="Operations health unavailable" message="No health evidence was returned."/>;
  return <div className="space-y-5"><Card><div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><CardTitle>Tenant operations health</CardTitle><CardDescription>Safe per-salon worker and queue evidence.</CardDescription></div><div className="flex items-center gap-2"><Badge value={status.status}/><Button type="button" variant="secondary" onClick={() => void onReload()}><RefreshCcw className="h-4 w-4"/>Refresh</Button></div></div><p className="mt-3 text-xs text-muted">Evaluated {new Date(status.evaluated_at).toLocaleString()}</p></Card><section><h3 className="mb-3 text-sm font-bold uppercase tracking-wide text-muted">Recurring workers</h3><div className="grid gap-3 lg:grid-cols-2">{status.jobs.map(job => <Card key={job.key}><div className="flex items-start justify-between gap-3"><div><CardTitle>{job.label}</CardTitle><CardDescription>{job.error_code || `Stale after ${job.stale_after_seconds}s`}</CardDescription></div><Badge value={job.status}/></div><dl className="mt-4 grid grid-cols-2 gap-3 text-sm"><Metric label="Last success" value={formatDate(job.last_success_at)}/><Metric label="Processed" value={job.last_processed_count?.toString() || "—"}/></dl></Card>)}</div></section><section><h3 className="mb-3 text-sm font-bold uppercase tracking-wide text-muted">Tenant queues</h3><div className="grid gap-3 lg:grid-cols-2">{status.queues.map(queue => <Card key={queue.key}><div className="flex items-start justify-between gap-3"><div><CardTitle>{queue.label}</CardTitle><CardDescription>{queue.error_code || "Tenant-scoped aggregate"}</CardDescription></div><Badge value={queue.status}/></div><dl className="mt-4 grid grid-cols-3 gap-3 text-sm"><Metric label="Backlog" value={String(queue.backlog_count)}/><Metric label="Dead letter" value={String(queue.dead_letter_count)}/><Metric label="Oldest" value={formatDate(queue.oldest_at)}/></dl></Card>)}</div></section></div>;
}

function Metric({ label, value }: { label: string; value: string }) { return <div><dt className="text-xs font-semibold text-muted">{label}</dt><dd className="mt-1 font-semibold text-ink">{value}</dd></div>; }
function formatDate(value?: string) { return value ? new Date(value).toLocaleString() : "—"; }
