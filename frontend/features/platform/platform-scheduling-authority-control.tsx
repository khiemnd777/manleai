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
import { newBookingModeActionKey, updatePlatformBookingMode } from "@/lib/api/scheduling-behavior";
import {
  changePlatformSchedulingAuthority,
  latestSchedulingAuthoritySwitch,
  newSchedulingAuthorityActionKey,
  preparePlatformSchedulingAuthorityChange
} from "@/lib/api/scheduling-authority-switches";
import type { ChangeSchedulingAuthorityInput } from "@/lib/api/scheduling-authority-switches";
import { actionableCalendarBlockers, calendarBlockerPresentation } from "@/lib/scheduling/calendar-setup";
import type {
  BookingMode,
  ManleAICalendarAggregate,
  SchedulingAuthority,
  SchedulingAuthorityReadinessBlocker,
  SchedulingAuthoritySwitchRun,
  SchedulingBehavior
} from "@/types/api";

const authorityOptions: Array<{ value: SchedulingAuthority; label: string; description: string }> = [
  { value: "owner_manual", label: "Owner confirmation", description: "Capture new scheduling work as owner-review requests. No appointment is confirmed automatically." },
  { value: "manleai_calendar", label: "ManleAI Calendar", description: "Use verified internal availability and atomic calendar commits when automatic confirmation is enabled." },
  { value: "external_provider", label: "Square Appointments", description: "Use Square availability and durable booking evidence when automatic confirmation is enabled." }
];

const bookingModeOptions: Array<{ value: BookingMode; label: string; description: string }> = [
  { value: "pending_approval", label: "Owner approval required", description: "Check availability when supported, then create an owner-review request. The selected time is not reserved." },
  { value: "confirmed_booking", label: "Confirm automatically", description: "Confirm only after the selected authority returns its required durable booking evidence." },
  { value: "disabled", label: "Disabled", description: "Do not check availability or create new scheduling work from AI conversations." }
];

const authorityConflictCodes = new Set([
  "SCHEDULING_AUTHORITY_SWITCH_VERSION_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_READINESS_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_STATE_CONFLICT",
  "SCHEDULING_AUTHORITY_SWITCH_LIVE_EXECUTION"
]);

const bookingModeConflictCodes = new Set([
  "SCHEDULING_BEHAVIOR_VERSION_CONFLICT",
  "SCHEDULING_BEHAVIOR_INCOMPATIBLE"
]);

type BusyAction = "" | "readiness" | "change" | "booking-mode" | "reload";

export function PlatformSchedulingAuthorityControl({
  salonID,
  behavior,
  calendar,
  canChangeAuthority,
  canSetBookingMode,
  onReload
}: {
  salonID: string;
  behavior: SchedulingBehavior;
  calendar: ManleAICalendarAggregate | null;
  canChangeAuthority: boolean;
  canSetBookingMode: boolean;
  onReload: () => Promise<void>;
}) {
  const currentAuthority = behavior.scheduling_authority;
  const currentVersion = behavior.authority_version;
  const [selected, setSelected] = useState<SchedulingAuthority>(currentAuthority);
  const [selectedBookingMode, setSelectedBookingMode] = useState<BookingMode>(behavior.booking_mode);
  const [latest, setLatest] = useState<SchedulingAuthoritySwitchRun | null>(null);
  const [readiness, setReadiness] = useState<SchedulingAuthoritySwitchRun | null>(null);
  const [actionKey, setActionKey] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<BusyAction>("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const activeRequest = useRef("");

  useEffect(() => {
    setSelected(currentAuthority);
    setReadiness(null);
    setActionKey("");
  }, [currentAuthority, currentVersion]);

  useEffect(() => {
    setSelectedBookingMode(behavior.booking_mode);
  }, [behavior.booking_mode, behavior.policy_version]);

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
      if (activeRequest.current === nextActionKey) await handleAuthorityConflict(caught, "Could not evaluate this scheduling authority.");
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
    setSuccess("");
    try {
      const response = await changePlatformSchedulingAuthority(salonID, changeInput(selected, actionKey, rollbackRunID(selected)));
      if (response.data.status !== "committed") {
        setReadiness(response.data);
        return;
      }
      setLatest(response.data);
      setReadiness(null);
      setSuccess(`New scheduling work now uses ${authorityLabel(response.data.target_scheduling_authority)}. The AI booking mode was not changed.`);
      await onReload();
    } catch (caught) {
      await handleAuthorityConflict(caught, "Could not change scheduling authority.");
    } finally {
      setBusy("");
    }
  }

  async function saveBookingMode() {
    if (!canSetBookingMode || selectedBookingMode === behavior.booking_mode) return;
    setBusy("booking-mode");
    setError("");
    setSuccess("");
    try {
      await updatePlatformBookingMode(salonID, selectedBookingMode, behavior.policy_version, newBookingModeActionKey());
      setSuccess(`AI booking mode changed to ${bookingModeLabel(selectedBookingMode)}. Scheduling authority was not changed.`);
      await onReload();
    } catch (caught) {
      if (caught instanceof RequestError && bookingModeConflictCodes.has(caught.code)) {
        setError(`${caught.message} Current scheduling behavior was reloaded.`);
        await onReload();
      } else {
        setError(errorMessage(caught, "Could not change AI booking mode."));
      }
    } finally {
      setBusy("");
    }
  }

  async function handleAuthorityConflict(caught: unknown, fallback: string) {
    if (caught instanceof RequestError && authorityConflictCodes.has(caught.code)) {
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
      setError(errorMessage(caught, "Could not reload scheduling behavior."));
    } finally {
      setBusy("");
    }
  }

  if (loading) return <Card><Skeleton className="h-6 w-56" /><Skeleton className="mt-4 h-24" /><Skeleton className="mt-4 h-10 w-36" /></Card>;

  const ready = readiness?.status === "preview_ready" && readiness.readiness_snapshot.ready && readiness.blockers.length === 0;
  const visibleBlockers = actionableCalendarBlockers(readiness?.blockers ?? []);
  const allowedModes = new Set(behavior.allowed_booking_modes);
  const bookingModeChanged = selectedBookingMode !== behavior.booking_mode;

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Scheduling behavior</CardTitle>
          <CardDescription>Scheduling authority chooses the execution source. AI booking mode chooses whether new work needs owner approval.</CardDescription>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Badge value={authorityLabel(currentAuthority)} />
          <Badge value={bookingModeLabel(behavior.booking_mode)} />
        </div>
      </div>

      {error ? <div className="mt-4"><Alert title="Scheduling behavior unchanged" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Scheduling behavior updated" message={success} /></div> : null}

      <section className="mt-6" aria-labelledby="scheduling-authority-heading">
        <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-end">
          <div>
            <h3 id="scheduling-authority-heading" className="text-sm font-semibold text-ink">Scheduling authority</h3>
            <p className="mt-1 text-sm text-muted">Choose the source of availability and booking execution. Existing work keeps its original authority.</p>
          </div>
          <span className="text-xs text-muted">Authority version {currentVersion}</span>
        </div>
        <div className="mt-4 grid gap-3 lg:grid-cols-3">
          {authorityOptions.map((option) => {
            const active = option.value === currentAuthority;
            const chosen = option.value === selected;
            return <button key={option.value} type="button" disabled={busy !== "" || !canChangeAuthority} onClick={() => void selectTarget(option.value)} className={`min-w-0 rounded-lg border p-4 text-left disabled:cursor-not-allowed disabled:opacity-60 ${chosen ? "border-brand bg-teal-50" : "border-line bg-white hover:bg-slate-50"}`}><div className="flex items-start justify-between gap-2"><span className="text-sm font-semibold text-ink">{option.label}</span>{active ? <Badge value="active" /> : null}</div><p className="mt-2 text-sm leading-6 text-muted">{option.description}</p></button>;
          })}
        </div>
        {busy === "readiness" ? <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">Checking readiness…</div> : null}
        {readiness ? <div className={`mt-4 rounded-md border p-4 ${ready ? "border-emerald-200 bg-emerald-50" : "border-amber-200 bg-amber-50"}`}><div className="flex items-start gap-3">{ready ? <CheckCircle2 className="mt-0.5 h-5 w-5 text-emerald-700" /> : <AlertTriangle className="mt-0.5 h-5 w-5 text-amber-700" />}<div><div className="text-sm font-semibold text-ink">{ready ? `Ready to switch to ${authorityLabel(selected)}` : `${authorityLabel(selected)} needs setup`}</div><p className="mt-1 text-sm text-muted">{ready ? "The backend will recheck this evidence before changing new-work authority." : "Resolve the items below, then select this authority again."}</p></div></div>{visibleBlockers.length > 0 ? <div className="mt-3 space-y-2">{visibleBlockers.map((blocker, index) => { const item = blockerPresentation(blocker, calendar, selected, salonID); return <div key={`${blocker.code}-${blocker.scope ?? ""}-${blocker.entity_id ?? ""}-${index}`} className="flex flex-col justify-between gap-2 rounded-md border border-amber-200 bg-white p-3 sm:flex-row sm:items-center"><div><div className="text-sm font-semibold text-ink">{item.label}</div><div className="mt-1 text-xs leading-5 text-amber-900">{item.message}</div></div><Link href={item.href} className="flex-none text-sm font-semibold text-brand hover:underline">{item.action}</Link></div>; })}</div> : null}</div> : null}
        <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-xs leading-5 text-muted">{authorityHistoryLabel(latest)}</p>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Button type="button" variant="secondary" onClick={() => void reload()} disabled={busy !== ""} className="w-full sm:w-auto"><RefreshCcw className="h-4 w-4" />{busy === "reload" ? "Reloading…" : "Reload"}</Button>
            <Button type="button" onClick={() => void changeAuthority()} disabled={busy !== "" || !canChangeAuthority || !ready} className="w-full sm:w-auto">{busy === "change" ? "Changing…" : `Switch to ${authorityLabel(selected)}`}</Button>
          </div>
        </div>
      </section>

      <section id="ai-booking-mode" className="mt-7 border-t border-line pt-6" aria-labelledby="ai-booking-mode-heading">
        <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-end">
          <div>
            <h3 id="ai-booking-mode-heading" className="text-sm font-semibold text-ink">AI booking mode</h3>
            <p className="mt-1 text-sm text-muted">Choose how AI conversations handle new scheduling work. This does not change scheduling authority.</p>
          </div>
          <span className="text-xs text-muted">Policy version {behavior.policy_version}</span>
        </div>
        <div className="mt-4 grid gap-3 lg:grid-cols-3">
          {bookingModeOptions.map((option) => {
            const allowed = allowedModes.has(option.value);
            const active = option.value === behavior.booking_mode;
            const chosen = option.value === selectedBookingMode;
            return <button key={option.value} type="button" disabled={busy !== "" || !canSetBookingMode || !allowed} onClick={() => { setSelectedBookingMode(option.value); setError(""); setSuccess(""); }} className={`min-w-0 rounded-lg border p-4 text-left disabled:cursor-not-allowed disabled:opacity-60 ${chosen ? "border-brand bg-teal-50" : "border-line bg-white hover:bg-slate-50"}`}><div className="flex items-start justify-between gap-2"><span className="text-sm font-semibold text-ink">{option.label}</span>{active ? <Badge value="active" /> : !allowed ? <Badge value="not available" /> : null}</div><p className="mt-2 text-sm leading-6 text-muted">{option.description}</p></button>;
          })}
        </div>
        {!canSetBookingMode ? <div className="mt-4"><Alert title="Read-only scheduling behavior" message="Your Platform access can view this setting but cannot change it." /></div> : null}
        <div className="mt-4 rounded-lg border border-blue-200 bg-blue-50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wide text-blue-800">Current effective behavior</div>
          <p className="mt-2 text-sm leading-6 text-blue-950">{effectiveBehaviorDescription(behavior)}</p>
        </div>
        <div className="mt-5 flex justify-end">
          <Button type="button" onClick={() => void saveBookingMode()} disabled={busy !== "" || !canSetBookingMode || !bookingModeChanged || !allowedModes.has(selectedBookingMode)} className="w-full sm:w-auto">
            {busy === "booking-mode" ? "Saving…" : "Save AI booking mode"}
          </Button>
        </div>
      </section>
    </Card>
  );
}

function blockerPresentation(blocker: SchedulingAuthorityReadinessBlocker, calendar: ManleAICalendarAggregate | null, selected: SchedulingAuthority, salonID: string) {
  if (calendar) return calendarBlockerPresentation(blocker, calendar, "platform", salonID);
  if (blocker.code === "BOOKING_MODE_COMPATIBLE") {
    return { label: "AI booking mode", message: blocker.message, href: "#ai-booking-mode", action: "Review booking mode" };
  }
  const base = `/platform/tenants/${encodeURIComponent(salonID)}`;
  const href = selected === "external_provider" ? `${base}/integrations` : `${base}/scheduling/calendar`;
  return { label: humanizeCode(blocker.code), message: blocker.message, href, action: "Open setup" };
}

function effectiveBehaviorDescription(behavior: SchedulingBehavior) {
  switch (behavior.effective_behavior) {
    case "owner_review":
      return `${authorityLabel(behavior.scheduling_authority)} supplies availability when supported. AI records an owner-review request; the selected time is not reserved or confirmed.`;
    case "automatic_internal_commit":
      return "AI uses ManleAI Calendar and confirms only after verified availability and an atomic internal commit returns durable appointment evidence.";
    case "automatic_external_booking":
      return "AI uses Square Appointments and confirms only after the provider returns the required durable booking evidence.";
    case "disabled":
      return "AI does not check availability or create new scheduling work. Existing persisted operations keep their original replay behavior.";
  }
}

function authorityHistoryLabel(latest: SchedulingAuthoritySwitchRun | null) {
  if (!latest) return "No previous authority change.";
  if (latest.status === "committed") {
    return `Last change: ${authorityLabel(latest.source_scheduling_authority)} → ${authorityLabel(latest.target_scheduling_authority)} · ${formatDate(latest.committed_at ?? latest.updated_at)}`;
  }
  const status = latest.status === "preview_ready" ? "ready" : latest.status === "preview_blocked" ? "blocked" : "failed";
  return `Latest readiness check: ${authorityLabel(latest.target_scheduling_authority)} · ${status} · ${formatDate(latest.updated_at)}`;
}

function authorityLabel(value: SchedulingAuthority) { return authorityOptions.find((option) => option.value === value)?.label ?? value; }
function bookingModeLabel(value: BookingMode) { return bookingModeOptions.find((option) => option.value === value)?.label ?? value; }
function humanizeCode(value: string) { return value.toLocaleLowerCase().replace(/_/g, " ").replace(/^./, (letter) => letter.toUpperCase()); }
function formatDate(value: string) { const parsed = new Date(value); return Number.isNaN(parsed.getTime()) ? "time unavailable" : parsed.toLocaleString(); }
function errorMessage(error: unknown, fallback: string) { return error instanceof Error && error.message ? error.message : fallback; }
