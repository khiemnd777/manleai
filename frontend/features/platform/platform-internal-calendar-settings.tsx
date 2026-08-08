"use client";

import { useCallback, useEffect, useState } from "react";
import { ArrowLeft, ArrowRight, RefreshCcw } from "lucide-react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { InternalCalendarSetup } from "@/features/dashboard/internal-calendar-setup";
import { getManleAICalendar } from "@/lib/api/internal-calendar";
import {
  actionableCalendarBlockers,
  calendarBlockerPresentation,
  calendarSetupLinks,
  calendarSetupProgress
} from "@/lib/scheduling/calendar-setup";
import type { ManleAICalendarAggregate } from "@/types/api";

export function PlatformInternalCalendarSettings({ tenantID }: { tenantID: string }) {
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setError("");
    try {
      const response = await getManleAICalendar(tenantID, "platform");
      setCalendar(response.manleai_calendar);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load ManleAI Calendar setup.");
    }
  }, [tenantID]);

  useEffect(() => { void load(); }, [load]);

  if (error) return <Alert title="ManleAI Calendar setup unavailable" message={error}><Button type="button" variant="secondary" className="mt-4" onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Retry setup</Button></Alert>;
  if (!calendar) return <div className="space-y-4"><Skeleton className="h-24" /><Skeleton className="h-96" /></div>;

  return (
    <div className="space-y-5">
      <Link href={`/platform/tenants/${tenantID}/scheduling`} className="inline-flex items-center gap-2 text-sm font-semibold text-brand"><ArrowLeft className="h-4 w-4" />Scheduling authority</Link>
      <div><h2 className="text-lg font-bold text-ink">ManleAI Calendar setup</h2><p className="mt-1 text-sm text-muted">Configure and activate the internal scheduling engine. This workflow does not change scheduling authority.</p></div>
      <PlatformCalendarSetupProgress tenantID={tenantID} calendar={calendar} />
      <InternalCalendarSetup salonID={tenantID} timezone={calendar.timezone} surface="platform" initialCalendar={calendar} onCalendarChange={setCalendar} />
    </div>
  );
}

function PlatformCalendarSetupProgress({ tenantID, calendar }: { tenantID: string; calendar: ManleAICalendarAggregate }) {
  const progress = calendarSetupProgress(calendar);
  const links = calendarSetupLinks("platform", tenantID);
  const blockers = actionableCalendarBlockers(calendar.readiness.blockers);
  const steps = [
    { label: "Policy & hours", detail: progress.policyReady && progress.hoursReady ? "Complete" : `${progress.policyReady ? "Policy saved" : "Policy required"} · ${progress.hoursReady ? "Hours saved" : "Hours required"}`, ready: progress.policyReady && progress.hoursReady, href: `${links.calendar}#${progress.policyReady ? "calendar-hours" : "calendar-policy"}` },
    { label: "Staff schedules", detail: progress.requiredStaffCount > 0 ? `${progress.scheduledStaffCount}/${progress.requiredStaffCount} assigned staff ready` : "Enable a service and assign staff first", ready: progress.staffReady, href: links.staff },
    { label: "Service policies", detail: `${progress.configuredServiceCount}/${progress.eligibleServiceCount} explicitly configured · ${progress.enabledServiceCount} enabled`, ready: progress.eligibleServiceCount > 0 && progress.configuredServiceCount === progress.eligibleServiceCount && progress.enabledServiceCount > 0, href: links.services },
    { label: "Activation", detail: progress.activationCurrent ? `Current version ${calendar.config_version}` : `Version ${calendar.config_version} needs activation`, ready: progress.activationCurrent, href: `${links.calendar}#calendar-activation` }
  ];

  return (
    <Card className="border-blue-200 bg-blue-50 shadow-none">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div><CardTitle>Production setup checklist</CardTitle><CardDescription className="text-blue-900">Complete each owner-scoped object, activate the final configuration version, then preview the authority switch.</CardDescription></div>
        <Badge value={calendar.readiness.configuration_ready && progress.activationCurrent ? "ready" : "needs setup"} />
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        {steps.map((step) => <Link key={step.label} href={step.href} className="rounded-md border border-line bg-white p-4 hover:bg-slate-50"><div className="flex items-start justify-between gap-2"><div className="text-sm font-semibold text-ink">{step.label}</div><Badge value={step.ready ? "ready" : "required"} /></div><div className="mt-2 text-xs leading-5 text-muted">{step.detail}</div></Link>)}
      </div>
      {blockers.length > 0 ? <div className="mt-4 rounded-md border border-line bg-white p-4"><div className="text-sm font-semibold text-ink">Needs action</div><div className="mt-3 space-y-2">{blockers.map((blocker, index) => { const item = calendarBlockerPresentation(blocker, calendar, "platform", tenantID); return <div key={`${blocker.code}-${blocker.entity_id ?? blocker.scope}-${index}`} className="flex flex-col justify-between gap-2 rounded-md border border-line p-3 sm:flex-row sm:items-center"><div><div className="text-sm font-medium text-ink">{item.label}</div><div className="mt-1 text-xs leading-5 text-muted">{item.message}</div></div><Link href={item.href} className="inline-flex h-9 flex-none items-center justify-center gap-2 rounded-md border border-line bg-white px-3 text-sm font-semibold text-ink hover:bg-slate-50">{item.action}<ArrowRight className="h-4 w-4" /></Link></div>; })}</div></div> : null}
    </Card>
  );
}
