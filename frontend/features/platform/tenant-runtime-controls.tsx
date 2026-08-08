"use client";

import { useCallback, useEffect, useState } from "react";
import { Activity, Gauge, Save } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest, RequestError } from "@/lib/api/client";

type RuntimeLimits = {
  salon_id: string;
  expensive_requests_per_minute: number;
  scheduling_writes_per_minute: number;
  provider_writes_per_minute: number;
  voice_starts_per_minute: number;
  worker_claims_per_batch: number;
  version: number;
  updated_at: string;
};

type RuntimeMetric = {
  metric: string;
  used: number;
  rejected: number;
  current_per_minute_limit: number;
};

type RuntimeProfile = {
  limits: RuntimeLimits;
  window_minutes: number;
  usage: RuntimeMetric[];
  evaluated_at: string;
};

type LimitForm = Omit<RuntimeLimits, "salon_id" | "version" | "updated_at">;

export function TenantRuntimeControls({ tenantID }: { tenantID: string }) {
  const [profile, setProfile] = useState<RuntimeProfile | null>(null);
  const [form, setForm] = useState<LimitForm | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [blocked, setBlocked] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [actionKey, setActionKey] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = (await apiRequest<{data:RuntimeProfile}>(`/api/v2/platform/tenants/${encodeURIComponent(tenantID)}/operations/runtime-limits?window_minutes=60`)).data;
      setProfile(result);
      setForm(formFromLimits(result.limits));
      setBlocked(false);
      setActionKey("");
    } catch (failure) {
      if (failure instanceof RequestError && failure.status === 403) setBlocked(true);
      else setError(failure instanceof Error ? failure.message : "Could not load tenant runtime controls.");
    } finally {
      setLoading(false);
    }
  }, [tenantID]);

  useEffect(() => { void load(); }, [load]);

  async function save() {
    if (!profile || !form) return;
    setSaving(true);
    setError("");
    setSuccess("");
    const stableActionKey = actionKey || newActionKey();
    setActionKey(stableActionKey);
    try {
      const limits = (await apiRequest<{data:RuntimeLimits}>(`/api/v2/platform/tenants/${encodeURIComponent(tenantID)}/operations/runtime-limits`, {
        method: "PUT",
        body: JSON.stringify({ ...form, action_key: stableActionKey, expected_version: profile.limits.version })
      })).data;
      setProfile((current) => current ? { ...current, limits } : current);
      setForm(formFromLimits(limits));
      setActionKey("");
      setSuccess("Tenant runtime limits were saved with an immutable Platform actor audit event.");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not save tenant runtime limits.");
    } finally {
      setSaving(false);
    }
  }

  if (loading) return <Skeleton className="h-72" />;
  if (blocked) return <Alert title="Runtime controls access denied" message="This Platform account needs operations.read for this exact salon." />;
  if (!profile || !form) return error ? <Alert title="Runtime controls unavailable" message={error} /> : null;

  return (
    <Card>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex items-center gap-2"><Gauge className="h-5 w-5 text-brand" /><CardTitle>Tenant resource controls</CardTitle></div>
          <CardDescription>Per-salon quotas and fair worker claims protect the shared VPS from noisy-neighbor load. Counts contain no customer or provider payloads.</CardDescription>
        </div>
        <Badge value={`v${profile.limits.version}`} />
      </div>

      {error ? <div className="mt-4"><Alert title="Runtime limits not saved" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Runtime limits updated" message={success} /></div> : null}

      <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {profile.usage.map((metric) => (
          <div key={metric.metric} className="rounded-lg border border-line bg-panel p-4">
            <div className="flex items-center justify-between gap-2"><span className="text-xs font-bold uppercase tracking-wide text-muted">{metricLabel(metric.metric)}</span><Activity className="h-4 w-4 text-brand" /></div>
            <div className="mt-3 text-2xl font-bold text-ink">{metric.used}</div>
            <div className="mt-1 text-xs text-muted">used in {profile.window_minutes}m · {metric.rejected} rejected</div>
            <div className="mt-2 text-xs font-semibold text-ink">Limit {metric.current_per_minute_limit}/minute</div>
          </div>
        ))}
      </div>

      <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-5">
        <LimitField label="Expensive requests / min" value={form.expensive_requests_per_minute} onChange={(value) => update("expensive_requests_per_minute", value)} />
        <LimitField label="Scheduling writes / min" value={form.scheduling_writes_per_minute} onChange={(value) => update("scheduling_writes_per_minute", value)} />
        <LimitField label="Provider writes / min" value={form.provider_writes_per_minute} onChange={(value) => update("provider_writes_per_minute", value)} />
        <LimitField label="Voice starts / min" value={form.voice_starts_per_minute} onChange={(value) => update("voice_starts_per_minute", value)} />
        <LimitField label="Worker claims / batch" value={form.worker_claims_per_batch} max={50} onChange={(value) => update("worker_claims_per_batch", value)} />
      </div>

      <div className="mt-5 flex flex-col gap-3 border-t border-line pt-4 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-xs text-muted">Updated {new Date(profile.limits.updated_at).toLocaleString()}. Changes require operations.write and exact version.</p>
        <div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => void load()} disabled={saving}>Refresh</Button><Button type="button" onClick={() => void save()} disabled={saving}><Save className="h-4 w-4" />{saving ? "Saving..." : "Save limits"}</Button></div>
      </div>
    </Card>
  );

  function update(field: keyof LimitForm, value: number) {
    setForm((current) => current ? { ...current, [field]: value } : current);
    setActionKey("");
  }
}

function LimitField({ label, value, max = 6000, onChange }: { label: string; value: number; max?: number; onChange: (value: number) => void }) {
  return <label className="block"><span className="text-xs font-semibold text-muted">{label}</span><input type="number" min={1} max={max} value={value} onChange={(event) => onChange(Number(event.target.value))} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm font-semibold text-ink outline-none" /></label>;
}

function formFromLimits(limits: RuntimeLimits): LimitForm {
  return {
    expensive_requests_per_minute: limits.expensive_requests_per_minute,
    scheduling_writes_per_minute: limits.scheduling_writes_per_minute,
    provider_writes_per_minute: limits.provider_writes_per_minute,
    voice_starts_per_minute: limits.voice_starts_per_minute,
    worker_claims_per_batch: limits.worker_claims_per_batch
  };
}

function metricLabel(metric: string) {
  return ({
    expensive_request: "Expensive reads",
    scheduling_write: "Scheduling writes",
    provider_write: "Provider writes",
    voice_start: "Voice starts"
  } as Record<string, string>)[metric] ?? metric;
}

function newActionKey() {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `tenant-runtime-limits-${suffix}`;
}
