"use client";

import { useEffect, useState } from "react";
import { RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { sectionLabel } from "@/features/configuration-transfer/import-preview";
import { RequestError } from "@/lib/api/client";
import { listPlatformTransferRuns, type PlatformTransferResponse } from "@/lib/api/platform-configuration-transfer";

export function PlatformConfigurationTransferHistory({ tenantID }: { tenantID: string }) {
  const [runs, setRuns] = useState<PlatformTransferResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [forbidden, setForbidden] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    setLoading(true);
    setError("");
    setForbidden(false);
    try {
      const response = await listPlatformTransferRuns(tenantID);
      setRuns(response.runs);
    } catch (failure) {
      if (failure instanceof RequestError && failure.status === 403) {
        setForbidden(true);
        setRuns([]);
      } else {
        setError(failure instanceof Error ? failure.message : "Could not load configuration transfer history.");
      }
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { void load(); }, [tenantID]);

  return (
    <div className="space-y-5">
      <div><h2 className="text-xl font-bold text-ink">Configuration transfers</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-muted">Review immutable transfer-run evidence. Copy and apply controls remain under Platform Controls.</p></div>
      {error ? <Alert title="Configuration transfer history unavailable" message={error} /> : null}
      {forbidden ? <Alert type="warning" title="Audit access required" message="This Platform account needs audit.read to review configuration transfer history." /> : null}
      <Card>
        <div className="flex items-start justify-between gap-3"><div><CardTitle>Transfer runs</CardTitle><CardDescription>Safe run metadata only. Full configuration payloads and secret values are not stored.</CardDescription></div><Button type="button" variant="secondary" onClick={() => void load()} disabled={loading}><RefreshCcw className="h-4 w-4" />{loading ? "Loading…" : "Refresh"}</Button></div>
        {!forbidden && !loading && runs.length ? <div className="mt-4 space-y-3">{runs.map((run) => <div key={run.run_id} className="rounded-md border border-line p-4"><div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between"><div><div className="text-sm font-semibold text-ink">{run.source_type === "tenant" ? "Tenant-to-tenant" : "JSON upload"}</div><div className="mt-1 text-xs text-muted">{new Date(run.created_at).toLocaleString()} · {run.included_sections.map(sectionLabel).join(", ")}</div></div><Badge value={run.status} /></div></div>)}</div> : null}
        {!forbidden && !loading && runs.length === 0 ? <div className="mt-4 rounded-md border border-dashed border-line p-5 text-sm text-muted">No transfer run has been recorded for this salon.</div> : null}
      </Card>
    </div>
  );
}
