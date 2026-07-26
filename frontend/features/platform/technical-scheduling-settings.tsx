"use client";

import { useCallback, useEffect, useState } from "react";
import { Alert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { InternalCalendarSetup } from "@/features/dashboard/internal-calendar-setup";
import { SchedulingAuthoritySwitch } from "@/features/dashboard/scheduling-authority-switch";
import { getManleAICalendar } from "@/lib/api/internal-calendar";
import type { ManleAICalendarAggregate } from "@/types/api";

export function TechnicalSchedulingSettings({ tenantID }: { tenantID: string }) {
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
      setError(failure instanceof Error ? failure.message : "Could not load scheduling technical settings.");
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
    return <Alert title="Scheduling technical settings unavailable" message={error || "No calendar configuration was returned."} />;
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-bold text-ink">Scheduling control plane</h2>
        <p className="mt-1 text-sm text-muted">
          Platform Admin/Ops owns authority switching and internal-calendar configuration. A provider connection never changes scheduling authority implicitly.
        </p>
      </div>
      <SchedulingAuthoritySwitch
        salonID={tenantID}
        currentAuthority={calendar.scheduling_authority}
        currentVersion={calendar.authority_version}
        onReload={load}
        surface="platform"
      />
      <InternalCalendarSetup salonID={tenantID} timezone={calendar.timezone} surface="platform" />
    </div>
  );
}
