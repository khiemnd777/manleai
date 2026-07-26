"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Building2, CheckCircle2, FileJson, Upload } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { ImportIssueList, ImportScopePreview, ImportSummaryTable, listOrNone } from "@/features/configuration-transfer/import-preview";
import { apiRequest } from "@/lib/api/client";
import {
  applyOnboardingConfigurationImport,
  newConfigurationImportRequestID,
  previewOnboardingConfigurationImport,
  readConfigurationBundle
} from "@/lib/api/configuration-transfer";
import type { ConfigurationBundle, ConfigurationImportResponse, Salon, SchedulingAuthority } from "@/types/api";

type OnboardingMode = "manual" | "import";

export function SalonProfileForm() {
  const router = useRouter();
  const [mode, setMode] = useState<OnboardingMode>("manual");
  const [schedulingAuthority, setSchedulingAuthority] = useState<SchedulingAuthority>("owner_manual");
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
  const [importBusy, setImportBusy] = useState<"" | "preview" | "apply">("");
  const [importBundle, setImportBundle] = useState<ConfigurationBundle | null>(null);
  const [importPreview, setImportPreview] = useState<ConfigurationImportResponse | null>(null);
  const [importFileName, setImportFileName] = useState("");
  const [importRequestID, setImportRequestID] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const busy = loading || importBusy !== "";

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
        body: JSON.stringify({ ...form, scheduling_authority: schedulingAuthority, operation_key: operationKey })
      });
      if (salon.scheduling_authority === schedulingAuthority) {
        setSuccess(`${salon.name} was created with ${authorityLabel(schedulingAuthority)}.`);
        setTimeout(() => router.push(schedulingAuthority === "external_provider" ? "/dashboard/integrations" : "/dashboard/settings"), 600);
        return;
      }
      setSuccess(`${salon.name} was created, but the create response did not confirm ${authorityLabel(schedulingAuthority)}. The salon was preserved; review scheduling authority in Settings before enabling scheduling.`);
      setTimeout(() => router.push("/dashboard/settings"), 1100);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create salon.");
    } finally {
      setLoading(false);
    }
  }

  async function previewImportFile(file: File | null) {
    if (!file) return;
    setError("");
    setSuccess("");
    setImportBusy("preview");
    try {
      const bundle = await readConfigurationBundle(file);
      const preview = await previewOnboardingConfigurationImport(bundle);
      setImportBundle(bundle);
      setImportPreview(preview);
      setImportFileName(file.name);
      setImportRequestID(newConfigurationImportRequestID());
      setSuccess(preview.can_apply ? "Configuration import preview is ready." : "Configuration import preview has conflicts.");
    } catch (err) {
      clearImportState();
      setError(err instanceof Error ? err.message : "Could not preview configuration import.");
    } finally {
      setImportBusy("");
    }
  }

  async function applyImportPreview() {
    if (!importBundle || !importPreview?.can_apply) return;
    setError("");
    setSuccess("");
    setImportBusy("apply");
    const requestID = importRequestID || newConfigurationImportRequestID();
    try {
      const applied = await applyOnboardingConfigurationImport(importBundle, requestID);
      setImportPreview(applied);
      setImportRequestID(applied.request_id);
      setSuccess("Configuration imported with Owner confirmation as the safe default. Review scheduling authority and re-enter provider secrets in Settings.");
      setTimeout(() => router.push("/dashboard/settings"), 700);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not apply configuration import.");
    } finally {
      setImportBusy("");
    }
  }

  function clearImportState() {
    setImportBundle(null);
    setImportPreview(null);
    setImportFileName("");
    setImportRequestID("");
  }

  return (
    <div className="mx-auto w-full max-w-4xl min-w-0 space-y-6">
      <div>
        <div className="text-sm font-semibold text-brand">Owner onboarding</div>
        <h1 className="mt-2 text-3xl font-bold text-ink">Create salon profile</h1>
        <p className="mt-2 text-sm leading-6 text-muted">
          Create a new salon manually or import a ManleAI configuration JSON bundle.
        </p>
      </div>

      <div className="grid min-w-0 gap-4 md:grid-cols-3">
        {["Salon Profile", "Scheduling Authority", "Setup & Readiness"].map((step, index) => (
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

      <div className="grid min-w-0 gap-3 md:grid-cols-2">
        <ModeButton
          active={mode === "manual"}
          title="Create manually"
          description="Enter salon details and explicitly choose the initial scheduling authority."
          onClick={() => setMode("manual")}
        />
        <ModeButton
          active={mode === "import"}
          title="Import JSON"
          description="Preview and apply a configuration export with Owner confirmation as the safe authority default."
          onClick={() => setMode("import")}
        />
      </div>

      {mode === "manual" ? (
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
          <div className="mb-6">
            <div className="text-sm font-semibold text-ink">Initial scheduling authority</div>
            <div className="mt-1 text-sm leading-6 text-muted">Owner confirmation is the safe default. ManleAI Calendar and Square Appointments still require readiness setup after creation.</div>
            <div className="mt-3 grid gap-3 lg:grid-cols-3">
              {onboardingAuthorities.map((authority) => (
                <button
                  key={authority.value}
                  type="button"
                  disabled={busy}
                  onClick={() => { setSchedulingAuthority(authority.value); setCreateOperationKey(""); }}
                  className={`rounded-lg border p-4 text-left disabled:cursor-not-allowed disabled:opacity-60 ${schedulingAuthority === authority.value ? "border-brand bg-teal-50" : "border-line bg-white hover:bg-slate-50"}`}
                >
                  <div className="flex items-start justify-between gap-2"><span className="text-sm font-semibold text-ink">{authority.label}</span>{schedulingAuthority === authority.value ? <Badge value="selected" /> : null}</div>
                  <div className="mt-2 text-sm leading-6 text-muted">{authority.description}</div>
                </button>
              ))}
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
              <Button type="button" variant="secondary" onClick={() => router.push("/dashboard")} disabled={busy}>
                Back
              </Button>
              <Button type="submit" disabled={busy}>
                {loading ? "Creating..." : "Create salon"}
              </Button>
            </div>
          </form>
        </Card>
      ) : (
        <ImportConfigurationPanel
          busy={importBusy}
          disabled={busy}
          fileName={importFileName}
          preview={importPreview}
          onApply={() => void applyImportPreview()}
          onClear={clearImportState}
          onPreviewFile={(file) => void previewImportFile(file)}
        />
      )}
    </div>
  );
}

const onboardingAuthorities: Array<{ value: SchedulingAuthority; label: string; description: string }> = [
  { value: "owner_manual", label: "Owner confirmation", description: "New requests wait for owner review and are never auto-confirmed." },
  { value: "manleai_calendar", label: "ManleAI Calendar", description: "Set up and activate internal hours, staff, services, and capacity next." },
  { value: "external_provider", label: "Square Appointments", description: "Configure and verify a Square Appointments connection next." }
];

function authorityLabel(value: SchedulingAuthority) {
  return onboardingAuthorities.find((authority) => authority.value === value)?.label ?? value;
}

function newSalonCreateOperationKey() {
  const suffix = typeof crypto !== "undefined" && "randomUUID" in crypto
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `onboarding-salon-create-${suffix}`;
}

function ModeButton({
  active,
  title,
  description,
  onClick
}: {
  active: boolean;
  title: string;
  description: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`w-full min-w-0 rounded-lg border p-4 text-left shadow-soft ${
        active ? "border-brand bg-white" : "border-line bg-panel hover:bg-slate-50"
      }`}
    >
      <div className="flex min-w-0 items-center justify-between gap-3">
        <div className="min-w-0 text-sm font-semibold text-ink">{title}</div>
        <Badge value={active ? "active" : "available"} className="flex-none" />
      </div>
      <div className="mt-2 min-w-0 break-words text-sm leading-6 text-muted">{description}</div>
    </button>
  );
}

function ImportConfigurationPanel({
  busy,
  disabled,
  fileName,
  preview,
  onApply,
  onClear,
  onPreviewFile
}: {
  busy: "" | "preview" | "apply";
  disabled: boolean;
  fileName: string;
  preview: ConfigurationImportResponse | null;
  onApply: () => void;
  onClear: () => void;
  onPreviewFile: (file: File | null) => void;
}) {
  const previewBusy = busy === "preview";
  const applyBusy = busy === "apply";
  return (
    <Card className="min-w-0">
      <div className="flex flex-col justify-between gap-4 md:flex-row md:items-start">
        <div className="flex gap-3">
          <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand">
            <FileJson className="h-5 w-5" />
          </div>
          <div>
            <CardTitle>Import salon configuration</CardTitle>
            <CardDescription>
              Import salon profile, AI receptionist, public booking page, integrations, service understanding, and AI Training knowledge base.
            </CardDescription>
          </div>
        </div>
        <label
          className={`inline-flex h-10 cursor-pointer items-center justify-center gap-2 rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50 ${
            disabled ? "pointer-events-none opacity-50" : ""
          }`}
        >
          <Upload className="h-4 w-4" />
          {previewBusy ? "Previewing..." : "Choose JSON"}
          <input
            type="file"
            accept="application/json,.json"
            className="hidden"
            disabled={disabled}
            onChange={(event) => {
              const file = event.target.files?.[0] ?? null;
              event.target.value = "";
              onPreviewFile(file);
            }}
          />
        </label>
      </div>

      <div className="mt-5 grid min-w-0 gap-4 lg:grid-cols-[0.8fr_1.2fr]">
        <div className="min-w-0 rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Full bundle includes</div>
          <div className="mt-3 flex flex-wrap gap-2">
            {["Salon profile", "AI receptionist", "Public booking page", "Integrations", "Service categories", "Service aliases", "Consultation profiles", "Knowledge base"].map((item) => (
              <Badge key={item} value={item.toLowerCase().replaceAll(" ", "_")} />
            ))}
          </div>
          <div className="mt-5 text-sm font-semibold text-ink">Excluded</div>
          <div className="mt-2 text-sm leading-6 text-muted">
            Scheduling authority and switch history, services, staff, customers, appointments, pending scheduling requests, internal execution records, call data, POS connection state, provider tokens, API keys, and client secrets.
          </div>
        </div>

        <div className="min-w-0 rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Preview before import</div>
          <div className="mt-1 text-sm leading-6 text-muted">
            Preview validates schema, destination authority compatibility, conflicts, and repeated-import behavior before any write.
          </div>
          {fileName ? <div className="mt-3 break-all text-xs text-muted">Selected file: {fileName}</div> : null}

          {preview ? (
            <div className="mt-5 space-y-4">
              <ImportScopePreview preview={preview} />
              <ImportSummaryTable summary={preview.summary} />
              <ImportIssueList title="Conflicts" issues={preview.conflicts} tone="danger" />
              <ImportIssueList title="Warnings" issues={preview.warnings} tone="warning" />
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="text-xs leading-5 text-muted">
                  Schema {preview.schema_version}. Secrets must be re-entered for:{" "}
                  {listOrNone(preview.requires_secret_reentry)}.
                </div>
                <div className="flex gap-2">
                  <Button type="button" variant="secondary" onClick={onClear} disabled={disabled}>
                    Clear
                  </Button>
                  <Button type="button" onClick={onApply} disabled={disabled || !preview.can_apply}>
                    <Upload className="h-4 w-4" />
                    {applyBusy ? "Applying..." : "Apply import"}
                  </Button>
                </div>
              </div>
            </div>
          ) : (
            <div className="mt-5 rounded-md border border-dashed border-line p-5 text-sm leading-6 text-muted">
              Choose a ManleAI configuration JSON file to preview the salon setup. Preview does not create a salon.
            </div>
          )}
        </div>
      </div>
    </Card>
  );
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
