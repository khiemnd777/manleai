"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Activity, ExternalLink, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { getOperationsHealth } from "@/lib/api/operations-health";
import type {
  OperationsHealthJob,
  OperationsHealthQueue,
  OperationsHealthResponse,
  OperationsHealthStatus
} from "@/types/api";

export function OperationsHealth({ salonID }: { salonID: string }) {
  const [status, setStatus] = useState<OperationsHealthResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      setStatus(await getOperationsHealth(salonID));
    } catch (loadError) {
      setStatus(null);
      setError(loadError instanceof Error ? loadError.message : "Could not load operations health.");
    } finally {
      setLoading(false);
    }
  }, [salonID]);

  useEffect(() => {
    void load();
  }, [load]);

  if (loading && !status) {
    return (
      <Card aria-label="Loading operations health">
        <div className="flex items-start gap-3">
          <Skeleton className="h-10 w-10 flex-none" />
          <div className="w-full space-y-3">
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-4 w-full max-w-2xl" />
            <Skeleton className="h-36 w-full" />
          </div>
        </div>
      </Card>
    );
  }

  if (error || !status) {
    return (
      <Card>
        <Alert
          title="Operations health unavailable"
          message={error || "Worker heartbeats and tenant queue metrics could not be loaded."}
        />
        <Button type="button" variant="secondary" className="mt-4" onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" />
          Retry operations health
        </Button>
      </Card>
    );
  }

  return (
    <Card className={cardTone(status.status)}>
      <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-start">
        <div className="flex min-w-0 gap-3">
          <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-white text-brand ring-1 ring-line">
            <Activity className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <CardTitle>Operations health</CardTitle>
              <HealthBadge status={status.status} />
            </div>
            <CardDescription>
              Persisted worker heartbeats and this salon&apos;s safe backlog totals. Missing or stale evidence fails closed.
            </CardDescription>
            <div className="mt-1 text-xs text-muted">Evaluated {formatDateTime(status.evaluated_at)}</div>
          </div>
        </div>
        <Button type="button" variant="secondary" onClick={() => void load()} disabled={loading}>
          <RefreshCcw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {status.status !== "healthy" && status.status !== "running" ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
          This status is not proof of a provider outage. Open the linked owner workflow and verify current configuration before taking action.
        </div>
      ) : null}

      <section className="mt-5" aria-labelledby="worker-job-health-title">
        <div id="worker-job-health-title" className="text-sm font-semibold text-ink">Recurring worker jobs</div>
        <div className="mt-3 overflow-x-auto rounded-md border border-line bg-white">
          <table className="min-w-[760px] w-full text-left text-sm">
            <thead className="bg-slate-50 text-xs uppercase tracking-wide text-muted">
              <tr>
                <th className="px-3 py-3 font-semibold">Job</th>
                <th className="px-3 py-3 font-semibold">Status</th>
                <th className="px-3 py-3 font-semibold">Last success</th>
                <th className="px-3 py-3 font-semibold">Heartbeat</th>
                <th className="px-3 py-3 text-right font-semibold">Processed</th>
                <th className="px-3 py-3 font-semibold"><span className="sr-only">Open</span></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {status.jobs.map((job) => <JobRow key={job.key} job={job} />)}
            </tbody>
          </table>
        </div>
      </section>

      <section className="mt-5" aria-labelledby="salon-queue-health-title">
        <div id="salon-queue-health-title" className="text-sm font-semibold text-ink">Salon queues</div>
        <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {status.queues.map((queue) => <QueueCard key={queue.key} queue={queue} />)}
        </div>
      </section>
    </Card>
  );
}

function JobRow({ job }: { job: OperationsHealthJob }) {
  return (
    <tr>
      <td className="px-3 py-3">
        <div className="font-medium text-ink">{job.label}</div>
        {job.error_code ? <div className="mt-1 font-mono text-xs text-muted">{job.error_code}</div> : null}
      </td>
      <td className="px-3 py-3"><HealthBadge status={job.status} /></td>
      <td className="px-3 py-3 text-muted">{formatOptionalDate(job.last_success_at)}</td>
      <td className="px-3 py-3 text-muted">{formatOptionalDate(job.last_heartbeat_at)}</td>
      <td className="px-3 py-3 text-right font-medium text-ink">{formatCount(job.last_processed_count)}</td>
      <td className="px-3 py-3 text-right"><SafeLink links={job.links} /></td>
    </tr>
  );
}

function QueueCard({ queue }: { queue: OperationsHealthQueue }) {
  return (
    <div className="rounded-md border border-line bg-white p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="font-medium text-ink">{queue.label}</div>
        <HealthBadge status={queue.status} />
      </div>
      <dl className="mt-3 grid grid-cols-3 gap-2 text-xs">
        <Metric label="Backlog" value={queue.backlog_count.toLocaleString()} />
        <Metric label="Oldest" value={formatCompactDate(queue.oldest_at)} />
        <Metric label="Dead letter" value={queue.dead_letter_count.toLocaleString()} />
      </dl>
      {queue.error_code ? <div className="mt-3 font-mono text-xs text-muted">{queue.error_code}</div> : null}
      <div className="mt-3"><SafeLink links={queue.links} /></div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted">{label}</dt>
      <dd className="mt-1 font-semibold text-ink">{value}</dd>
    </div>
  );
}

function SafeLink({ links }: { links: Array<{ label: string; href: string }> }) {
  const link = links.find((item) => item.href.startsWith("/dashboard/"));
  if (!link) return null;
  return (
    <Link href={link.href} className="inline-flex items-center gap-1 text-xs font-semibold text-brand hover:text-teal-800">
      {link.label}
      <ExternalLink className="h-3.5 w-3.5" />
    </Link>
  );
}

function HealthBadge({ status }: { status: OperationsHealthStatus }) {
  return <Badge value={status} className={badgeTone(status)} />;
}

function badgeTone(status: OperationsHealthStatus) {
  if (status === "healthy") return "bg-emerald-50 text-emerald-700 ring-emerald-200";
  if (status === "running") return "bg-blue-50 text-blue-700 ring-blue-200";
  if (status === "degraded") return "bg-amber-50 text-amber-700 ring-amber-200";
  if (status === "stale") return "bg-red-50 text-red-700 ring-red-200";
  return "bg-slate-100 text-slate-700 ring-slate-200";
}

function cardTone(status: OperationsHealthStatus) {
  if (status === "healthy" || status === "running") return "border-emerald-200 bg-emerald-50/40";
  if (status === "degraded") return "border-amber-200 bg-amber-50/40";
  if (status === "stale") return "border-red-200 bg-red-50/30";
  return "border-slate-300 bg-slate-50";
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
}

function formatOptionalDate(value?: string) { return value ? formatDateTime(value) : "Not recorded"; }
function formatCompactDate(value?: string) {
  if (!value) return "None";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(date);
}
function formatCount(value?: number) { return typeof value === "number" ? value.toLocaleString() : "—"; }
