"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Building2, CheckCircle2 } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { apiRequest } from "@/lib/api/client";
import type { Salon } from "@/types/api";

export function SalonProfileForm() {
  const router = useRouter();
  const [createOperationKey, setCreateOperationKey] = useState("");
  const [form, setForm] = useState({
    name: "",
    phone: "",
    address: "",
    city: "",
    state: "",
    zip_code: "",
    timezone: "America/Chicago",
    primary_language: "en",
    secondary_language: "vi",
    handoff_phone: ""
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  function setField(field: keyof typeof form, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
    setCreateOperationKey("");
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setSuccess("");
    setLoading(true);
    const operationKey = createOperationKey || newSalonCreateOperationKey();
    setCreateOperationKey(operationKey);
    try {
      const salon = await apiRequest<Salon>("/api/salons", {
        method: "POST",
        body: JSON.stringify({ ...form, scheduling_authority: "owner_manual", operation_key: operationKey })
      });
      setSuccess(`${salon.name} was created in Owner review mode. Platform Operations will configure and activate technical integrations separately.`);
      setTimeout(() => router.push("/dashboard"), 700);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create salon.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="mx-auto w-full max-w-4xl min-w-0 space-y-6">
      <div>
        <div className="text-sm font-semibold text-brand">Owner onboarding</div>
        <h1 className="mt-2 text-3xl font-bold text-ink">Create salon profile</h1>
        <p className="mt-2 text-sm leading-6 text-muted">
          Add the salon&apos;s business identity. Platform Operations handles provider and scheduling setup after creation.
        </p>
      </div>

      <div className="grid min-w-0 gap-4 md:grid-cols-3">
        {["Business Profile", "Owner Review Mode", "Platform Setup"].map((step, index) => (
          <Card key={step} className="min-w-0 p-4">
            <div className="flex items-center gap-3">
              <CheckCircle2 className={index === 0 ? "h-5 w-5 text-brand" : "h-5 w-5 text-slate-300"} />
              <div className="min-w-0 text-sm font-semibold text-ink">{step}</div>
            </div>
          </Card>
        ))}
      </div>

      {error ? <Alert title="Salon setup failed" message={error} /> : null}
      {success ? <Alert type="success" title="Salon setup" message={success} /> : null}

      <Card className="min-w-0">
          <div className="mb-6 flex items-start gap-3">
            <Building2 className="mt-1 h-5 w-5 text-brand" />
            <div>
              <CardTitle>Salon details</CardTitle>
              <CardDescription>
                Defaults are tuned for a US nail salon with English primary and Vietnamese secondary
                conversation support.
              </CardDescription>
            </div>
          </div>
          <div className="mb-6 rounded-lg border border-teal-200 bg-teal-50 p-4">
            <div className="text-sm font-semibold text-ink">Safe initial scheduling mode</div>
            <div className="mt-1 text-sm leading-6 text-muted">
              New appointment requests start in Owner review mode and are never auto-confirmed. Platform Operations will connect Square, Twilio, and OpenAI, then review and activate the intended scheduling authority from the Platform console.
            </div>
          </div>
          <form onSubmit={submit} className="grid min-w-0 gap-4 md:grid-cols-2">
            <Field label="Salon name" value={form.name} onChange={(value) => setField("name", value)} />
            <Field label="Business phone" value={form.phone} onChange={(value) => setField("phone", value)} />
            <Field label="Address" value={form.address} onChange={(value) => setField("address", value)} />
            <Field label="City" value={form.city} onChange={(value) => setField("city", value)} />
            <Field label="State" value={form.state} onChange={(value) => setField("state", value)} />
            <Field label="ZIP code" value={form.zip_code} onChange={(value) => setField("zip_code", value)} />
            <Field label="Timezone" value={form.timezone} onChange={(value) => setField("timezone", value)} />
            <Field
              label="Handoff phone"
              value={form.handoff_phone}
              onChange={(value) => setField("handoff_phone", value)}
            />
            <div className="md:col-span-2 flex justify-end gap-3 pt-2">
              <Button type="button" variant="secondary" onClick={() => router.push("/dashboard")} disabled={loading}>
                Back
              </Button>
              <Button type="submit" disabled={loading}>
                {loading ? "Creating..." : "Create salon"}
              </Button>
            </div>
          </form>
      </Card>
    </div>
  );
}

function newSalonCreateOperationKey() {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `onboarding-salon-create-${suffix}`;
}

function Field({
  label,
  value,
  onChange
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block min-w-0">
      <span className="text-sm font-semibold text-ink">{label}</span>
      <input
        className="mt-2 h-11 w-full rounded-md border border-line bg-white px-3 text-sm outline-none"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}
