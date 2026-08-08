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
import type { ManleAICalendarAggregate } from "@/types/api";

export function PlatformSchedulingSettings({ tenantID }: { tenantID: string }) {
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const response = await getManleAICalendar(tenantID, "platform");
      setCalendar(response.manleai_calendar);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load scheduling settings.");
    } finally {
      setLoading(false);
    }
  }, [tenantID]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading) {
    return <div className="space-y-4"><Skeleton className="h-48 w-full" /><Skeleton className="h-96 w-full" /></div>;
  }
  if (error || !calendar) {
    return <Alert title="Scheduling settings unavailable" message={error || "No calendar configuration was returned."} />;
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-bold text-ink">Scheduling authority</h2>
        <p className="mt-1 text-sm text-muted">
          Choose who may confirm new scheduling work. A provider connection never changes this setting.
        </p>
      </div>
      <PlatformSchedulingAuthorityControl
        salonID={tenantID}
        currentAuthority={calendar.scheduling_authority}
        currentVersion={calendar.authority_version}
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
