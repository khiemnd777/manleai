"use client";

import { useCallback, useEffect, useState } from "react";
import { RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest, RequestError } from "@/lib/api/client";

type AuditEvent = { id:string;actor_user_id?:string;salon_id?:string;target_user_id?:string;event_type:string;object_type:string;object_id:string;details:Record<string,unknown>;created_at:string };
type AuditResponse = { events:AuditEvent[];limit:number;offset:number;has_more:boolean };

export function PlatformAuditLog({ tenantID }: { tenantID: string }) {
  const [result, setResult] = useState<AuditResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [blocked, setBlocked] = useState(false);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true); setError(""); setBlocked(false);
    try { setResult(await apiRequest<AuditResponse>(`/api/platform/access/audit?salon_id=${encodeURIComponent(tenantID)}&limit=100`)); }
    catch (failure) { if (failure instanceof RequestError && failure.status === 403) setBlocked(true); else setError(failure instanceof Error ? failure.message : "Could not load audit events."); }
    finally { setLoading(false); }
  }, [tenantID]);
  useEffect(() => { void load(); }, [load]);
  if (loading) return <div className="space-y-3">{Array.from({length:4}).map((_,index)=><Skeleton key={index} className="h-24"/>)}</div>;
  if (blocked) return <Alert title="Audit access denied" message="This Platform account needs audit.read for this exact salon."/>;
  if (error) return <Alert title="Audit unavailable" message={error}/>;
  return <div className="space-y-4"><div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"><div><h2 className="text-lg font-bold text-ink">Tenant audit trail</h2><p className="mt-1 text-sm text-muted">Access, Business, and Technical events preserve the actual actor. Secret values and raw provider payloads are not stored here.</p></div><Button type="button" variant="secondary" onClick={() => void load()}><RefreshCcw className="h-4 w-4"/>Refresh</Button></div>{result?.events.length?<div className="space-y-3">{result.events.map(event=><Card key={event.id}><div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between"><div><CardTitle>{event.event_type}</CardTitle><CardDescription>{event.object_type} · {event.object_id}</CardDescription></div><Badge value={event.object_type}/></div><dl className="mt-4 grid gap-3 text-xs sm:grid-cols-3"><Item label="Actor" value={event.actor_user_id||"system"}/><Item label="Created" value={new Date(event.created_at).toLocaleString()}/><Item label="Target user" value={event.target_user_id||"—"}/></dl>{Object.keys(event.details||{}).length?<pre className="mt-4 overflow-x-auto rounded-lg bg-slate-50 p-3 text-xs text-slate-700">{JSON.stringify(event.details,null,2)}</pre>:null}</Card>)}</div>:<Alert title="No tenant events" message="No access, Business, or Technical audit event has been recorded for this salon yet."/>}</div>;
}

function Item({label,value}:{label:string;value:string}){return <div><dt className="font-bold uppercase tracking-wide text-muted">{label}</dt><dd className="mt-1 break-all text-ink">{value}</dd></div>}
