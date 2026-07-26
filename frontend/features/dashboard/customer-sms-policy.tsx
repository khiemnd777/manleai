"use client";

import { useCallback, useEffect, useState } from "react";
import { MessageSquareText, RefreshCcw, Save } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  getCustomerSMSPolicy,
  updateCustomerSMSPolicy,
  type CustomerSMSPolicy
} from "@/lib/api/customer-notifications";

type PolicyForm = { enabled: boolean; quietStart: string; quietEnd: string };

export function CustomerSMSPolicyCard({ salonID }: { salonID: string }) {
  const [policy, setPolicy] = useState<CustomerSMSPolicy | null>(null);
  const [form, setForm] = useState<PolicyForm>({ enabled: false, quietStart: "21:00", quietEnd: "08:00" });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const next = await getCustomerSMSPolicy(salonID);
      setPolicy(next);
      setForm({
        enabled: next.enabled,
        quietStart: next.quiet_start || "21:00",
        quietEnd: next.quiet_end || "08:00"
      });
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "Could not load customer SMS policy.");
    } finally {
      setLoading(false);
    }
  }, [salonID]);

  useEffect(() => { void load(); }, [load]);

  async function save() {
    if (!policy || saving) return;
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const next = await updateCustomerSMSPolicy(salonID, {
        enabled: form.enabled,
        quietStart: form.quietStart,
        quietEnd: form.quietEnd,
        expectedVersion: policy.version
      });
      setPolicy(next);
      setForm({ enabled: next.enabled, quietStart: next.quiet_start || form.quietStart, quietEnd: next.quiet_end || form.quietEnd });
      setSuccess(next.enabled
        ? "Customer SMS is enabled. Each destination still needs explicit consent before a message can be queued."
        : "Customer SMS is disabled. Existing consent remains audited, but new delivery is suppressed.");
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "Could not save customer SMS policy.");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="flex gap-3">
          <MessageSquareText className="mt-0.5 h-5 w-5 flex-none text-brand" />
          <div>
            <CardTitle>Customer appointment SMS</CardTitle>
            <CardDescription>
              Send request and appointment updates only after explicit customer consent. This policy is disabled by default and uses the salon timezone for quiet hours.
            </CardDescription>
          </div>
        </div>
        <Badge value={policy?.enabled ? (policy.ready ? "active" : "blocked") : "disabled"} />
      </div>

      {loading ? <div className="mt-5 space-y-3"><Skeleton className="h-10" /><Skeleton className="h-20" /></div> : null}
      {error ? <div className="mt-5"><Alert title="Customer SMS policy unavailable" message={error} /></div> : null}
      {!loading && !policy && error ? (
        <Button type="button" variant="secondary" className="mt-4 w-full sm:w-auto" onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" /> Retry customer SMS policy
        </Button>
      ) : null}
      {success ? <div className="mt-5"><Alert type="success" title="Customer SMS policy saved" message={success} /></div> : null}

      {!loading && policy ? (
        <div className="mt-5 space-y-5">
          <label className="flex items-start gap-3 rounded-md border border-line bg-slate-50 p-4">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4"
              checked={form.enabled}
              disabled={saving}
              onChange={(event) => setForm((current) => ({ ...current, enabled: event.target.checked }))}
            />
            <span>
              <span className="block text-sm font-semibold text-ink">Enable customer SMS after explicit consent</span>
              <span className="mt-1 block text-sm leading-6 text-muted">A caller phone number alone is never consent. STOP immediately blocks later sends; START applies only from the signed Twilio opt-out callback.</span>
            </span>
          </label>

          <div className="grid gap-4 sm:grid-cols-2">
            <TimeField label="Quiet hours start" value={form.quietStart} disabled={saving} onChange={(quietStart) => setForm((current) => ({ ...current, quietStart }))} />
            <TimeField label="Quiet hours end" value={form.quietEnd} disabled={saving} onChange={(quietEnd) => setForm((current) => ({ ...current, quietEnd }))} />
          </div>
          <div className="rounded-md border border-line bg-white px-4 py-3 text-sm leading-6 text-muted">
            Salon timezone: <span className="font-medium text-ink">{policy.timezone || "Not configured"}</span>. Messages held during quiet hours stay queued until the next valid local send time.
          </div>

          <div className="flex flex-col gap-3 sm:flex-row sm:justify-end">
            <Button type="button" variant="secondary" disabled={saving} onClick={() => void load()}>
              <RefreshCcw className="h-4 w-4" /> Refresh
            </Button>
            <Button type="button" disabled={saving || form.quietStart === form.quietEnd} onClick={() => void save()}>
              <Save className="h-4 w-4" /> {saving ? "Saving…" : "Save customer SMS policy"}
            </Button>
          </div>
        </div>
      ) : null}
    </Card>
  );
}

function TimeField({ label, value, disabled, onChange }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <input type="time" value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
    </label>
  );
}
