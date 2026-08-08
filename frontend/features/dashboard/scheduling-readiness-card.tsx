import Link from "next/link";
import { AlertTriangle, CalendarClock, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  actionableCalendarBlockers,
  calendarBlockerPresentation,
  calendarSetupLinks,
  type CalendarSetupSurface
} from "@/lib/scheduling/calendar-setup";
import type { ManleAICalendarAggregate, SchedulingAuthority } from "@/types/api";

type SchedulingReadinessCardProps = {
  calendar: ManleAICalendarAggregate | null;
  loading?: boolean;
  error?: string;
  onRetry?: () => void;
  showSetupLinks?: boolean;
  setupSurface?: CalendarSetupSurface;
  salonID?: string;
};

export function SchedulingReadinessCard({
  calendar,
  loading = false,
  error = "",
  onRetry,
  showSetupLinks = true,
  setupSurface = "tenant",
  salonID = ""
}: SchedulingReadinessCardProps) {
  if (loading) {
    return (
      <Card aria-label="Loading scheduling readiness">
        <div className="flex items-start gap-3">
          <Skeleton className="h-10 w-10 flex-none" />
          <div className="w-full space-y-3">
            <Skeleton className="h-5 w-56" />
            <Skeleton className="h-4 w-full max-w-2xl" />
            <div className="grid gap-3 sm:grid-cols-3">
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </div>
          </div>
        </div>
      </Card>
    );
  }

  if (error || !calendar) {
    return (
      <Card>
        <Alert
          title="Scheduling readiness unavailable"
          message={error || "The current scheduling authority and its readiness could not be loaded."}
        />
        {onRetry ? (
          <Button type="button" variant="secondary" className="mt-4" onClick={onRetry}>
            <RefreshCcw className="h-4 w-4" />
            Retry readiness
          </Button>
        ) : null}
      </Card>
    );
  }

  const readiness = calendar.readiness;
  const blockers = actionableCalendarBlockers(readiness.blockers);
  const setupLinks = calendarSetupLinks(setupSurface, salonID);
  const capabilities = readiness.capabilities;
  const partialExecution = Boolean(capabilities && Object.values(capabilities).some(Boolean));
  const selectedInternal = calendar.scheduling_authority === "manleai_calendar";
  const selectedOwner = calendar.scheduling_authority === "owner_manual";
  const tone = selectedInternal && readiness.execution_ready
    ? "border-emerald-200 bg-emerald-50 shadow-none"
    : selectedInternal
      ? "border-amber-200 bg-amber-50 shadow-none"
      : "border-blue-200 bg-blue-50 shadow-none";

  return (
    <Card className={tone}>
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
        <div className="flex min-w-0 gap-3">
          {selectedInternal && !readiness.execution_ready ? (
            <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
          ) : (
            <CalendarClock className="mt-0.5 h-5 w-5 flex-none text-brand" />
          )}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>Scheduling authority readiness</CardTitle>
              <Badge value={authorityLabel(calendar.scheduling_authority)} />
            </div>
            <CardDescription className={selectedInternal ? "text-amber-900" : "text-blue-900"}>
              {readinessDescription(calendar.scheduling_authority, readiness.execution_ready, capabilities?.staff_only_create === true)}
            </CardDescription>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {selectedInternal ? (
            <>
              <Badge value={readiness.configuration_ready ? "configured" : "required"} />
              <Badge value={readiness.execution_ready ? "ready" : partialExecution ? "partial" : "blocked"} />
            </>
          ) : (
            <Badge value={selectedOwner ? "pending" : "selected"} />
          )}
        </div>
      </div>

      <div className="mt-4 grid gap-3 sm:grid-cols-3">
        <ReadinessValue label="Selected authority" value={authorityLabel(calendar.scheduling_authority)} />
        {selectedInternal ? (
          <>
            <ReadinessValue
              label="Configuration"
              value={readiness.configuration_ready ? "Ready" : "Needs attention"}
            />
            <ReadinessValue
              label="Full scheduling execution"
              value={readiness.execution_ready ? "Ready" : partialExecution ? "Partial" : "Disabled"}
            />
          </>
        ) : selectedOwner ? (
          <>
            <ReadinessValue label="New work" value="Pending owner review" />
            <ReadinessValue label="Automatic confirmation" value="Never" />
          </>
        ) : (
          <>
            <ReadinessValue label="New work" value="Square Appointments selected" />
            <ReadinessValue label="Confirmation evidence" value="Square booking ID" />
          </>
        )}
      </div>

      {selectedInternal ? (
        <div className="mt-4 rounded-md border border-line bg-white p-4">
          <div className="text-sm font-semibold text-ink">Backend capability matrix</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            Controls are enabled only from these operation-specific backend flags. Full execution can remain blocked while the staff-only slice is available.
          </div>
          <div className="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            <CapabilityValue label="Staff-only availability" enabled={capabilities?.staff_only_availability === true} />
            <CapabilityValue label="Staff-only create" enabled={capabilities?.staff_only_create === true} />
            <CapabilityValue label="Party create" enabled={capabilities?.party_create === true} />
            <CapabilityValue label="Pooled capacity" enabled={capabilities?.pooled_capacity === true} />
            <CapabilityValue label="Reschedule" enabled={capabilities?.reschedule === true} />
            <CapabilityValue label="Cancel" enabled={capabilities?.cancel === true} />
          </div>
        </div>
      ) : null}

      <div className="mt-3 text-xs leading-5 text-muted">
        Authority version {readiness.authority_version}
        {selectedInternal ? ` · Calendar configuration version ${readiness.config_version}` : ""}
      </div>

      {selectedInternal && blockers.length > 0 ? (
        <div className="mt-4 rounded-md border border-line bg-white p-4">
          <div className="text-sm font-semibold text-ink">Backend readiness blockers</div>
          <div className="mt-3 space-y-2">
            {blockers.map((blocker, index) => {
              const item = calendarBlockerPresentation(blocker, calendar, setupSurface, salonID);
              return <div key={`${blocker.code}-${blocker.entity_id ?? blocker.scope}-${index}`} className="flex flex-col justify-between gap-2 rounded-md border border-line p-3 sm:flex-row sm:items-center"><div className="text-sm leading-6 text-muted"><span className="font-medium text-ink">{item.label}: </span>{item.message}<span className="ml-2 text-xs">{blocker.dimension} · {blocker.scope}</span></div><Link href={item.href} className="flex-none text-sm font-semibold text-brand hover:underline">{item.action}</Link></div>;
            })}
          </div>
        </div>
      ) : null}

      {selectedInternal && showSetupLinks ? (
        <div className="mt-4 flex flex-col gap-2 sm:flex-row sm:flex-wrap">
          <SetupLink href={setupLinks.calendar} label="Calendar settings" />
          <SetupLink href={setupLinks.staff} label="Staff schedules" />
          <SetupLink href={setupLinks.services} label="Service policies" />
        </div>
      ) : null}
    </Card>
  );
}

function ReadinessValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-line bg-white p-3">
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 text-sm font-semibold text-ink">{value}</div>
    </div>
  );
}

function CapabilityValue({ label, enabled }: { label: string; enabled: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border border-line bg-slate-50 p-3">
      <div>
        <div className="text-sm font-medium text-ink">{label}</div>
        {!enabled ? <div className="mt-1 text-xs text-muted">Needs compatible configuration</div> : null}
      </div>
      <Badge value={enabled ? "ready" : "blocked"} />
    </div>
  );
}

function SetupLink({ href, label }: { href: string; label: string }) {
  return (
    <Link
      href={href}
      className="inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
    >
      {label}
    </Link>
  );
}

function authorityLabel(authority: SchedulingAuthority) {
  if (authority === "manleai_calendar") return "ManleAI Calendar";
  if (authority === "owner_manual") return "Owner request";
  return "Square Appointments";
}

function readinessDescription(authority: SchedulingAuthority, executionReady: boolean, staffOnlyCreateReady: boolean) {
  if (authority === "owner_manual") {
    return "New scheduling work is recorded for owner review and is not automatically confirmed.";
  }
  if (authority === "external_provider") {
    return "New scheduling work uses Square Appointments. Internal calendar setup remains separate.";
  }
  if (executionReady) {
    return "ManleAI Calendar configuration and scheduling execution are ready.";
  }
  if (staffOnlyCreateReady) {
    return "Staff-only availability and atomic appointment creation are ready. Party, pooled-capacity, reschedule, and cancellation remain gated by their backend capabilities.";
  }
  return "ManleAI Calendar is selected, but internal availability and appointment writes remain disabled until execution readiness passes.";
}
