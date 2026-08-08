"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Bot, Power, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { getPlatformAIRuntime, updatePlatformAIRuntime } from "@/lib/api/ai-runtime-control";
import type { AIRuntimeResponse } from "@/lib/api/ai-runtime-control";
import { newBusinessActionKey } from "@/lib/api/business";

export function PlatformAIRuntimeControl({ tenantID }: { tenantID: string }) {
  const [runtime, setRuntime] = useState<AIRuntimeResponse | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const action = useRef<{ enabled: boolean; key: string } | null>(null);

  const load = useCallback(async () => {
    setError("");
    try {
      setRuntime(await getPlatformAIRuntime(tenantID));
    } catch (failure) {
      setError(errorMessage(failure, "Could not load the AI Receptionist runtime."));
    }
  }, [tenantID]);

  useEffect(() => { void load(); }, [load]);

  async function setEnabled(enabled: boolean) {
    if (!runtime) return;
    if (!action.current || action.current.enabled !== enabled) {
      action.current = { enabled, key: newBusinessActionKey(enabled ? "enable-ai-runtime" : "pause-ai-runtime") };
    }
    setBusy(true);
    setError("");
    try {
      const updated = await updatePlatformAIRuntime(tenantID, enabled, runtime.meta.resource_version, action.current.key);
      action.current = null;
      setRuntime(updated);
    } catch (failure) {
      setError(errorMessage(failure, "Could not update the AI Receptionist runtime."));
    } finally {
      setBusy(false);
    }
  }

  if (!runtime && !error) {
    return <Card><CardTitle>AI Receptionist runtime</CardTitle><CardDescription>Loading the salon-wide runtime state.</CardDescription><Skeleton className="mt-5 h-24" /></Card>;
  }

  const enabled = Boolean(runtime?.data.enabled);
  const canWrite = Boolean(runtime?.meta.permissions.allowed_actions.includes("set_enabled"));
  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-lg font-bold text-ink">AI Receptionist runtime</h2>
        <p className="mt-1 text-sm text-muted">Start or pause AI handling for this salon. Scheduling actions continue to follow the salon&apos;s selected scheduling authority.</p>
      </div>
      {error ? <Alert title="Runtime unavailable" message={error} /> : null}
      <Card>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex gap-3">
            <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand"><Bot className="h-5 w-5" /></div>
            <div><CardTitle>Call handling</CardTitle><CardDescription>{enabled ? "AI Receptionist is available to handle new calls." : "AI Receptionist is paused for new calls."}</CardDescription></div>
          </div>
          <Badge value={enabled ? "active" : "paused"} />
        </div>
        <div className="mt-5 rounded-lg border border-line bg-slate-50 p-4 text-sm text-muted">
          This switch does not connect Square, change scheduling authority, or confirm an appointment. Each scheduling operation still uses the authority and durable evidence required by the scheduling boundary.
        </div>
        <div className="mt-5 flex flex-wrap gap-3">
          <Button type="button" variant={enabled ? "danger" : "primary"} disabled={busy || !runtime || !canWrite} onClick={() => void setEnabled(!enabled)}>
            <Power className="h-4 w-4" />{busy ? "Saving…" : enabled ? "Pause AI Receptionist" : "Start AI Receptionist"}
          </Button>
          <Button type="button" variant="secondary" disabled={busy} onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Reload</Button>
        </div>
        {!canWrite && runtime ? <p className="mt-3 text-sm text-muted">This account can view runtime state but cannot change it.</p> : null}
      </Card>
    </div>
  );
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}

