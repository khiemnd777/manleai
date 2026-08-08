"use client";

import { useEffect, useRef, useState } from "react";
import { AlertTriangle, CheckCircle2, RefreshCcw } from "lucide-react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { RequestError } from "@/lib/api/client";
import {
  changePlatformSchedulingAuthority,
  latestSchedulingAuthoritySwitch,
  newSchedulingAuthorityActionKey,
  preparePlatformSchedulingAuthorityChange
} from "@/lib/api/scheduling-authority-switches";
import type { ChangeSchedulingAuthorityInput } from "@/lib/api/scheduling-authority-switches";
import { actionableCalendarBlockers, calendarBlockerPresentation } from "@/lib/scheduling/calendar-setup";
import type { ManleAICalendarAggregate, SchedulingAuthority, SchedulingAuthoritySwitchRun } from "@/types/api";

const authorityOptions: Array<{ value: SchedulingAuthority; label: string; description: string }> = [
  { value: "owner_manual", label: "Owner confirmation", description: "Capture pending requests for the salon to handle. No appointment is confirmed automatically." },
  { value: "manleai_calendar", label: "ManleAI Calendar", description: "Confirm new work only after verified internal availability and an atomic calendar commit." },
  { value: "external_provider", label: "Square Appointments", description: "Confirm new work only after Square returns the required durable booking evidence." }
];

const conflictCodes = new Set([
  "SCHEDULING_AUTHORITY_SWITCH_VERSION_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_READINESS_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_STATE_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_LIVE_EXECUTION"
]);

export function PlatformSchedulingAuthorityControl({
  salonID,
  currentAuthority,
  currentVersion,
  calendar,
  onReload
}: {
  salonID: string;
  currentAuthority: SchedulingAuthority;
  currentVersion: number;
  calendar: ManleAICalendarAggregate;
  onReload: () => Promise<void>;
}) {
  const [selected, setSelected] = useState<SchedulingAuthority>(currentAuthority);
  const [latest, setLatest] = useState<SchedulingAuthoritySwitchRun | null>(null);
  const [readiness, setReadiness] = useState<SchedulingAuthoritySwitchRun | null>(null);
  const [actionKey, setActionKey] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<"" | "readiness" | "change" | "reload">("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const activeRequest = useRef("");

  useEffect(() => {
    setSelected(currentAuthority);
    setReadiness(null);
    setActionKey("");
  }, [currentAuthority, currentVersion]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    latestSchedulingAuthoritySwitch(salonID, "platform")
      .then((response) => { if (active) setLatest(response.scheduling_authority_switch); })
      .catch((caught: unknown) => {
        if (active && (!(caught instanceof RequestError) || caught.status !== 404)) setError(errorMessage(caught, "Could not load authority history."));
      })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [salonID]);

  function rollbackRunID(target: SchedulingAuthority) {
    if (!latest || latest.status !== "committed") return "";
    return latest.target_scheduling_authority === currentAuthority && latest.source_scheduling_authority === target ? latest.id : "";
  }

  async function selectTarget(target: SchedulingAuthority) {
    setSelected(target);
    setReadiness(null);
    setError("");
    setSuccess("");
    if (target === currentAuthority || currentVersion <= 0) {
      setActionKey("");
      return;
    }
    const nextActionKey = newSchedulingAuthorityActionKey("change");
    const request = changeInput(target, nextActionKey, rollbackRunID(target));
    activeRequest.current = nextActionKey;
    setActionKey(nextActionKey);
    setBusy("readiness");
    try {
      const response = await preparePlatformSchedulingAuthorityChange(salonID, request);
      if (activeRequest.current === nextActionKey) setReadiness(response.data);
    } catch (caught) {
      if (activeRequest.current === nextActionKey) await handleConflict(caught, "Could not evaluate this scheduling authority.");
    } finally {
      if (activeRequest.current === nextActionKey) setBusy("");
    }
  }

  function changeInput(target: SchedulingAuthority, key: string, rollbackID: string): ChangeSchedulingAuthorityInput {
    return {
      target_scheduling_authority: target,
      expected_authority_version: currentVersion,
      action_key: key,
      ...(rollbackID ? { rollback_of_switch_run_id: rollbackID } : {})
    };
  }

  async function changeAuthority() {
    if (!readiness || readiness.status !== "preview_ready" || !readiness.readiness_snapshot.ready || !actionKey) return;
    setBusy("change");
    setError("");
    try {
      const response = await changePlatformSchedulingAuthority(salonID, changeInput(selected, actionKey, rollbackRunID(selected)));
      if (response.data.status !== "committed") {
        setReadiness(response.data);
        return;
      }
      setLatest(response.data);
      setReadiness(null);
      setSuccess(`New scheduling work now uses ${authorityLabel(response.data.target_scheduling_authority)}. Existing work keeps its original authority.`);
      await onReload();
    } catch (caught) {
      await handleConflict(caught, "Could not change scheduling authority.");
    } finally {
      setBusy("");
    }
  }

  async function handleConflict(caught: unknown, fallback: string) {
    if (caught instanceof RequestError && conflictCodes.has(caught.code)) {
      setReadiness(null);
      setActionKey("");
      setError(`${caught.message} Current settings were reloaded.`);
      await onReload();
      return;
    }
    setError(errorMessage(caught, fallback));
  }

  async function reload() {
    setBusy("reload");
    setError("");
    setReadiness(null);
    setActionKey("");
    try {
      await onReload();
      const response = await latestSchedulingAuthoritySwitch(salonID, "platform").catch((caught: unknown) => {
        if (caught instanceof RequestError && caught.status === 404) return null;
        throw caught;
      });
      setLatest(response?.scheduling_authority_switch ?? null);
    } catch (caught) {
      setError(errorMessage(caught, "Could not reload scheduling authority."));
    } finally {
      setBusy("");
    }
  }

  if (loading) return <Card><Skeleton className="h-6 w-56" /><Skeleton className="mt-4 h-24" /><Skeleton className="mt-4 h-10 w-36" /></Card>;

  const ready = readiness?.status === "preview_ready" && readiness.readiness_snapshot.ready && readiness.blockers.length === 0;
  const visibleBlockers = actionableCalendarBlockers(readiness?.blockers ?? []);
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div><CardTitle>Scheduling authority</CardTitle><CardDescription>Choose who confirms new scheduling work. Existing work keeps the authority that created it.</CardDescription></div>
        <div className="flex items-center gap-2"><Badge value={authorityLabel(currentAuthority)} /><Badge value={`version_${currentVersion}`} /></div>
      </div>
      {error ? <div className="mt-4"><Alert title="Scheduling authority unchanged" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Scheduling authority changed" message={success} /></div> : null}
      <div className="mt-5 grid gap-3 lg:grid-cols-3">
        {authorityOptions.map((option) => {
          const active = option.value === currentAuthority;
          const chosen = option.value === selected;
          return <button key={option.value} type="button" disabled={busy !== ""} onClick={() => void selectTarget(option.value)} className={`min-w-0 rounded-lg border p-4 text-left disabled:cursor-not-allowed disabled:opacity-60 ${chosen ? "border-brand bg-teal-50" : "border-line bg-white hover:bg-slate-50"}`}><div className="flex items-start justify-between gap-2"><span className="text-sm font-semibold text-ink">{option.label}</span>{active ? <Badge value="active" /> : null}</div><p className="mt-2 text-sm leading-6 text-muted">{option.description}</p></button>;
        })}
      </div>
      {busy === "readiness" ? <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">Checking readiness…</div> : null}
      {readiness ? <div className={`mt-4 rounded-md border p-4 ${ready ? "border-emerald-200 bg-emerald-50" : "border-amber-200 bg-amber-50"}`}><div className="flex items-start gap-3">{ready ? <CheckCircle2 className="mt-0.5 h-5 w-5 text-emerald-700" /> : <AlertTriangle className="mt-0.5 h-5 w-5 text-amber-700" />}<div><div className="text-sm font-semibold text-ink">{ready ? `Ready to switch to ${authorityLabel(selected)}` : `${authorityLabel(selected)} needs setup`}</div><p className="mt-1 text-sm text-muted">{ready ? "The backend will recheck this evidence before changing new-work authority." : "Resolve the items below, then select this authority again."}</p></div></div>{visibleBlockers.length > 0 ? <div className="mt-3 space-y-2">{visibleBlockers.map((blocker, index) => { const item = calendarBlockerPresentation(blocker, calendar, "platform", salonID); return <div key={`${blocker.code}-${blocker.scope ?? ""}-${blocker.entity_id ?? ""}-${index}`} className="flex flex-col justify-between gap-2 rounded-md border border-amber-200 bg-white p-3 sm:flex-row sm:items-center"><div><div className="text-sm font-semibold text-ink">{item.label}</div><div className="mt-1 text-xs leading-5 text-amber-900">{item.message}</div></div><Link href={item.href} className="flex-none text-sm font-semibold text-brand hover:underline">{item.action}</Link></div>; })}</div> : null}</div> : null}
      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><p className="text-xs leading-5 text-muted">{latest ? `Last change: ${authorityLabel(latest.source_scheduling_authority)} → ${authorityLabel(latest.target_scheduling_authority)} · ${formatDate(latest.updated_at)}` : "No previous authority change."}</p><div className="flex flex-col gap-2 sm:flex-row"><Button type="button" variant="secondary" onClick={() => void reload()} disabled={busy !== ""} className="w-full sm:w-auto"><RefreshCcw className="h-4 w-4" />{busy === "reload" ? "Reloading…" : "Reload"}</Button><Button type="button" onClick={() => void changeAuthority()} disabled={busy !== "" || !ready} className="w-full sm:w-auto">{busy === "change" ? "Changing…" : `Switch to ${authorityLabel(selected)}`}</Button></div></div>
    </Card>
  );
}

function authorityLabel(value: SchedulingAuthority) { return authorityOptions.find((option) => option.value === value)?.label ?? value; }
function formatDate(value: string) { const parsed = new Date(value); return Number.isNaN(parsed.getTime()) ? "time unavailable" : parsed.toLocaleString(); }
function errorMessage(error: unknown, fallback: string) { return error instanceof Error && error.message ? error.message : fallback; }
