"use client";

import { useCallback, useEffect, useState } from "react";
import { ArrowRight } from "lucide-react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { PlatformSchedulingAuthorityControl } from "@/features/platform/platform-scheduling-authority-control";
import { getManleAICalendar } from "@/lib/api/internal-calendar";
import { getPlatformSchedulingBehavior } from "@/lib/api/scheduling-behavior";
import type { ManleAICalendarAggregate, SchedulingBehavior } from "@/types/api";

export function PlatformSchedulingSettings({ tenantID }: { tenantID: string }) {
  const [behavior, setBehavior] = useState<SchedulingBehavior | null>(null);
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [canChangeAuthority, setCanChangeAuthority] = useState(false);
  const [canSetBookingMode, setCanSetBookingMode] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [behaviorResult, calendarResult] = await Promise.allSettled([
        getPlatformSchedulingBehavior(tenantID),
        getManleAICalendar(tenantID, "platform")
      ]);
      if (behaviorResult.status === "rejected") throw behaviorResult.reason;
      setBehavior(behaviorResult.value.data);
      setCanChangeAuthority(behaviorResult.value.meta.permissions.allowed_actions.includes("set_authority"));
      setCanSetBookingMode(behaviorResult.value.meta.permissions.allowed_actions.includes("set_booking_mode"));
      setCalendar(calendarResult.status === "fulfilled" ? calendarResult.value.manleai_calendar : null);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load scheduling settings.");
    } finally {
      setLoading(false);
    }
  }, [tenantID]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading && !behavior) {
    return <div className="space-y-4"><Skeleton className="h-48 w-full" /><Skeleton className="h-96 w-full" /></div>;
  }
  if (error || !behavior) {
    return <div className="space-y-3"><Alert title="Scheduling behavior unavailable" message={error || "No scheduling behavior was returned."} /><Button type="button" variant="secondary" onClick={() => void load()}>Retry</Button></div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-bold text-ink">Scheduling</h2>
        <p className="mt-1 text-sm text-muted">
          Manage the execution source and AI booking outcome for new scheduling work. Existing work keeps its original authority.
        </p>
      </div>
      <PlatformSchedulingAuthorityControl
        salonID={tenantID}
        behavior={behavior}
        calendar={calendar}
        canChangeAuthority={canChangeAuthority}
        canSetBookingMode={canSetBookingMode}
        onReload={load}
      />
      <Card>
        <CardTitle>ManleAI Calendar setup</CardTitle>
        <CardDescription>Hours, staff schedules, service policies, resources, exceptions, and activation are managed in a separate setup workflow.</CardDescription>
        <Link className="mt-4 inline-flex" href={`/platform/tenants/${tenantID}/scheduling/calendar`}><Button type="button" variant="secondary">Open calendar setup<ArrowRight className="h-4 w-4" /></Button></Link>
      </Card>
    </div>
  );
}
