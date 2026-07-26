"use client";

import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, CheckCircle2, RefreshCcw, X } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { RequestError } from "@/lib/api/client";
import {
  commitSchedulingAuthoritySwitch,
  latestSchedulingAuthoritySwitch,
  newSchedulingAuthorityActionKey,
  previewSchedulingAuthoritySwitch
} from "@/lib/api/scheduling-authority-switches";
import type { SchedulingAuthority, SchedulingAuthoritySwitchRun } from "@/types/api";

const authorityOptions: Array<{ value: SchedulingAuthority; label: string; description: string }> = [
  { value: "owner_manual", label: "Owner confirmation", description: "Record pending requests for owner review. No appointment is confirmed automatically." },
  { value: "manleai_calendar", label: "ManleAI Calendar", description: "Use verified internal availability and atomic ManleAI appointment commits when ready." },
  { value: "external_provider", label: "Square Appointments", description: "Use the configured Square Appointments connection and confirm only after Square returns the required booking evidence." }
];

const conflictCodes = new Set([
  "SCHEDULING_AUTHORITY_SWITCH_VERSION_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_READINESS_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_STATE_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_LIVE_EXECUTION"
]);

export function SchedulingAuthoritySwitch({
  salonID,
  currentAuthority,
  currentVersion,
  onReload
}: {
  salonID: string;
  currentAuthority: SchedulingAuthority;
  currentVersion: number;
  onReload: () => Promise<void>;
}) {
  const [selected, setSelected] = useState<SchedulingAuthority>(currentAuthority);
  const [latest, setLatest] = useState<SchedulingAuthoritySwitchRun | null>(null);
  const [preview, setPreview] = useState<SchedulingAuthoritySwitchRun | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<"" | "preview" | "commit" | "reload">("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [previewKey, setPreviewKey] = useState("");
  const [commitKey, setCommitKey] = useState("");

  useEffect(() => {
    setSelected(currentAuthority);
    setPreview(null);
    setPreviewKey("");
    setCommitKey("");
  }, [currentAuthority, currentVersion]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    latestSchedulingAuthoritySwitch(salonID)
      .then((response) => {
        if (active) setLatest(response.scheduling_authority_switch);
      })
      .catch((caught: unknown) => {
        if (active && (!(caught instanceof RequestError) || caught.status !== 404)) {
          setError(errorMessage(caught, "Could not load the latest authority switch."));
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => { active = false; };
  }, [salonID]);

  const rollbackOf = useMemo(() => {
    if (!latest || latest.status !== "committed") return "";
    return latest.target_scheduling_authority === currentAuthority && latest.source_scheduling_authority === selected ? latest.id : "";
  }, [currentAuthority, latest, selected]);

  async function createPreview() {
    if (selected === currentAuthority || currentVersion <= 0) return;
    const operationKey = previewKey || newSchedulingAuthorityActionKey("preview");
    setPreviewKey(operationKey);
    setBusy("preview");
    setError("");
    setSuccess("");
    try {
      const response = await previewSchedulingAuthoritySwitch(salonID, {
        operation_key: operationKey,
        source_scheduling_authority: currentAuthority,
        target_scheduling_authority: selected,
        expected_source_authority_version: currentVersion,
        ...(rollbackOf ? { rollback_of_switch_run_id: rollbackOf } : {})
      });
      setPreview(response.scheduling_authority_switch);
      setLatest(response.scheduling_authority_switch);
      setCommitKey(newSchedulingAuthorityActionKey("commit"));
    } catch (caught) {
      await handleConflict(caught, "Could not preview this authority switch.");
    } finally {
      setBusy("");
    }
  }

  async function commitPreview() {
    if (!preview || preview.status !== "preview_ready" || !preview.readiness_snapshot.ready || preview.blockers.length > 0) return;
    const actionKey = commitKey || newSchedulingAuthorityActionKey("commit");
    setCommitKey(actionKey);
    setBusy("commit");
    setError("");
    try {
      const response = await commitSchedulingAuthoritySwitch(salonID, preview.id, actionKey);
      const committed = response.scheduling_authority_switch;
      if (committed.status !== "committed") {
        setError("The backend did not return committed authority-switch evidence. Reload before taking another action.");
        return;
      }
      setLatest(committed);
      setPreview(null);
      setSuccess(`Scheduling authority changed to ${authorityLabel(committed.target_scheduling_authority)}. A reverse switch requires a fresh preview.`);
      await onReload();
    } catch (caught) {
      await handleConflict(caught, "Could not commit this authority switch.");
    } finally {
      setBusy("");
    }
  }

  async function handleConflict(caught: unknown, fallback: string) {
    if (caught instanceof RequestError && conflictCodes.has(caught.code)) {
      setPreview(null);
      setPreviewKey("");
      setCommitKey("");
      setError(`${caught.message} Current settings are being reloaded; create a fresh preview before committing.`);
      await onReload();
      return;
    }
    setError(errorMessage(caught, fallback));
  }

  async function reload() {
    setBusy("reload");
    setError("");
    setPreview(null);
    setPreviewKey("");
    setCommitKey("");
    try {
      await onReload();
      const response = await latestSchedulingAuthoritySwitch(salonID).catch((caught: unknown) => {
        if (caught instanceof RequestError && caught.status === 404) return null;
        throw caught;
      });
      setLatest(response?.scheduling_authority_switch ?? null);
    } catch (caught) {
      setError(errorMessage(caught, "Could not reload authority state."));
    } finally {
      setBusy("");
    }
  }

  if (loading) {
    return <Card><Skeleton className="h-6 w-56" /><Skeleton className="mt-4 h-24" /><Skeleton className="mt-4 h-10 w-36" /></Card>;
  }

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Scheduling authority</CardTitle>
          <CardDescription>Choose who may confirm new scheduling work. Provider setup in Integrations never changes this setting.</CardDescription>
        </div>
        <div className="flex items-center gap-2"><Badge value={authorityLabel(currentAuthority)} /><Badge value={`version_${currentVersion}`} /></div>
      </div>

      {error ? <div className="mt-4"><Alert title="Authority switch needs review" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Authority switch committed" message={success} /></div> : null}

      <div className="mt-5 grid gap-3 lg:grid-cols-3">
        {authorityOptions.map((option) => {
          const active = option.value === currentAuthority;
          const chosen = option.value === selected;
          return (
            <button
              key={option.value}
              type="button"
              disabled={busy !== ""}
              onClick={() => { setSelected(option.value); setPreview(null); setPreviewKey(""); setCommitKey(""); setError(""); }}
              className={`min-w-0 rounded-lg border p-4 text-left disabled:cursor-not-allowed disabled:opacity-60 ${chosen ? "border-brand bg-teal-50" : "border-line bg-white hover:bg-slate-50"}`}
            >
              <div className="flex items-start justify-between gap-2"><span className="text-sm font-semibold text-ink">{option.label}</span>{active ? <Badge value="active" /> : null}</div>
              <p className="mt-2 text-sm leading-6 text-muted">{option.description}</p>
            </button>
          );
        })}
      </div>

      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-xs leading-5 text-muted">
          {selected === currentAuthority ? "Select a different authority to preview readiness." : rollbackOf ? "This reverse switch will reference the prior committed switch and still requires a fresh readiness review." : "Preview is read-only. Nothing changes until an explicit commit succeeds."}
        </p>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button type="button" variant="secondary" onClick={() => void reload()} disabled={busy !== ""} className="w-full sm:w-auto"><RefreshCcw className="h-4 w-4" />{busy === "reload" ? "Reloading..." : "Reload"}</Button>
          <Button type="button" onClick={() => void createPreview()} disabled={busy !== "" || selected === currentAuthority || currentVersion <= 0} className="w-full sm:w-auto">{busy === "preview" ? "Previewing..." : "Preview switch"}</Button>
        </div>
      </div>

      {latest ? <div className="mt-4 text-xs text-muted">Latest switch: {historicalAuthorityLabel(latest.source_scheduling_authority)} → {historicalAuthorityLabel(latest.target_scheduling_authority)} · {latest.status.replaceAll("_", " ")} · {formatDate(latest.updated_at)}</div> : <div className="mt-4 text-xs text-muted">No previous switch run.</div>}

      {preview ? (
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-slate-950/40 sm:items-center sm:p-6" role="dialog" aria-modal="true" aria-labelledby="authority-review-title">
          <div className="max-h-[92vh] w-full overflow-y-auto rounded-t-lg bg-white p-5 shadow-soft sm:max-w-2xl sm:rounded-lg">
            <div className="flex items-start justify-between gap-4">
              <div><h2 id="authority-review-title" className="text-lg font-semibold text-ink">Review authority switch</h2><p className="mt-1 text-sm text-muted">{authorityLabel(preview.source_scheduling_authority)} → {authorityLabel(preview.target_scheduling_authority)}</p></div>
              <button type="button" onClick={() => setPreview(null)} disabled={busy === "commit"} aria-label="Close switch review" className="rounded-md p-2 text-muted hover:bg-slate-100"><X className="h-5 w-5" /></button>
            </div>

            <div className="mt-5 grid gap-3 sm:grid-cols-3">
              <ReviewMetric label="Authority version" value={String(preview.expected_source_authority_version)} />
              <ReviewMetric label="Eligible services" value={String(preview.readiness_snapshot.eligible_service_count ?? preview.readiness_snapshot.service_count ?? 0)} />
              <ReviewMetric label="Readiness" value={preview.readiness_snapshot.ready ? "Ready" : "Blocked"} />
            </div>

            <div className="mt-5 space-y-2">
              <div className="text-sm font-semibold text-ink">Readiness checks</div>
              {preview.readiness_snapshot.checks.length > 0 ? preview.readiness_snapshot.checks.map((check) => (
                <div key={`${check.code}-${check.scope ?? ""}-${check.entity_id ?? ""}`} className="flex items-start gap-3 rounded-md border border-line p-3">
                  {check.ready ? <CheckCircle2 className="mt-0.5 h-4 w-4 flex-none text-emerald-700" /> : <AlertTriangle className="mt-0.5 h-4 w-4 flex-none text-amber-700" />}
                  <div className="min-w-0"><div className="break-words text-sm font-medium text-ink">{readableCode(check.code)}</div>{check.scope ? <div className="mt-1 text-xs text-muted">Scope: {check.scope}</div> : null}</div>
                </div>
              )) : <div className="rounded-md border border-dashed border-line p-4 text-sm text-muted">The backend returned no readiness checks. Commit remains disabled unless readiness is explicitly true.</div>}
            </div>

            {preview.blockers.length > 0 ? <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 p-4"><div className="text-sm font-semibold text-amber-900">Resolve before committing</div><ul className="mt-2 space-y-2 text-sm leading-6 text-amber-900">{preview.blockers.map((blocker) => <li key={`${blocker.code}-${blocker.scope ?? ""}-${blocker.entity_id ?? ""}`}>• {blocker.message}</li>)}</ul></div> : null}

            <div className="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
              <Button type="button" variant="secondary" onClick={() => setPreview(null)} disabled={busy === "commit"} className="w-full sm:w-auto">Close</Button>
              <Button type="button" onClick={() => void commitPreview()} disabled={busy !== "" || preview.status !== "preview_ready" || !preview.readiness_snapshot.ready || preview.blockers.length > 0 || preview.readiness_snapshot.checks.length === 0} className="w-full sm:w-auto">{busy === "commit" ? "Committing..." : "Commit authority switch"}</Button>
            </div>
          </div>
        </div>
      ) : null}
    </Card>
  );
}

function ReviewMetric({ label, value }: { label: string; value: string }) { return <div className="rounded-md border border-line p-3"><div className="text-xs font-semibold uppercase text-muted">{label}</div><div className="mt-2 text-sm font-semibold text-ink">{value}</div></div>; }
function authorityLabel(value: SchedulingAuthority) { return authorityOptions.find((option) => option.value === value)?.label ?? value; }
function historicalAuthorityLabel(value: SchedulingAuthority) { return value === "external_provider" ? "External provider" : authorityLabel(value); }
function readableCode(value: string) { return value.toLowerCase().split("_").map((part) => part ? part[0].toUpperCase() + part.slice(1) : part).join(" "); }
function formatDate(value: string) { const parsed = new Date(value); return Number.isNaN(parsed.getTime()) ? "time unavailable" : parsed.toLocaleString(); }
function errorMessage(error: unknown, fallback: string) { return error instanceof Error && error.message ? error.message : fallback; }
