"use client";

import { useCallback, useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { InternalCalendarSetup } from "@/features/dashboard/internal-calendar-setup";
import { getManleAICalendar } from "@/lib/api/internal-calendar";
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

  if (error) return <Alert title="ManleAI Calendar setup unavailable" message={error} />;
  if (!calendar) return <div className="space-y-4"><Skeleton className="h-24" /><Skeleton className="h-96" /></div>;

  return (
    <div className="space-y-5">
      <Link href={`/platform/tenants/${tenantID}/scheduling`} className="inline-flex items-center gap-2 text-sm font-semibold text-brand"><ArrowLeft className="h-4 w-4" />Scheduling authority</Link>
      <div><h2 className="text-lg font-bold text-ink">ManleAI Calendar setup</h2><p className="mt-1 text-sm text-muted">Configure and activate the internal scheduling engine. This workflow does not change scheduling authority.</p></div>
      <InternalCalendarSetup salonID={tenantID} timezone={calendar.timezone} surface="platform" />
    </div>
  );
}

