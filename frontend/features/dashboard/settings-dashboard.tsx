"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import Link from "next/link";
import { AlertTriangle, Download, ExternalLink, FileJson, Globe2, RefreshCcw, Save, ShieldCheck, Upload } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { ImportIssueList, ImportSummaryTable, listOrNone } from "@/features/configuration-transfer/import-preview";
import { apiRequest } from "@/lib/api/client";
import {
  applyConfigurationImport,
  downloadConfigurationExport,
  newConfigurationImportRequestID,
  previewConfigurationImport,
  readConfigurationBundle
} from "@/lib/api/configuration-transfer";
import { landingBaseUrl } from "@/lib/config/env";
import type {
  BusinessHourPeriod,
  ConfigurationBundle,
  ConfigurationImportResponse,
  POSConnection,
  PublicCatalogSettings,
  Salon,
  SalonSettings,
  SquareReadiness,
  SyncLog
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type BusinessHoursResponse = {
  periods: BusinessHourPeriod[];
};

type SalonFormState = {
  name: string;
  phone: string;
  address: string;
  city: string;
  state: string;
  zipCode: string;
  timezone: string;
  primaryLanguage: string;
  secondaryLanguage: string;
  handoffPhone: string;
};

type SettingsFormState = {
  aiGreeting: string;
  aiTone: string;
  recordingEnabled: boolean;
  recordingConsentMessage: string;
  smsConfirmationEnabled: boolean;
  smsReminderEnabled: boolean;
  reminderHoursBefore: string;
  handoffEnabled: boolean;
};

type PublicCatalogFormState = {
  publicSlug: string;
  enabled: boolean;
};

const dayLabels = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

const aiToneOptions = [
  {
    value: "professional_warm",
    label: "Professional warm",
    preview: "Thanks for calling Lotus Nails Studio. How can I help today?"
  },
  {
    value: "natural_human",
    label: "Natural human",
    preview: "Thanks for calling Lotus Nails Studio. How can I help you today?"
  },
  {
    value: "friendly_young",
    label: "Friendly young",
    preview: "Hi, thanks for calling Lotus Nails Studio. How can I help today?"
  },
  {
    value: "concise_calm",
    label: "Concise calm",
    preview: "Thanks for calling Lotus Nails Studio. How can I help?"
  }
];

export function SettingsDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [settings, setSettings] = useState<SalonSettings | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [periods, setPeriods] = useState<BusinessHourPeriod[]>([]);
  const [salonForm, setSalonForm] = useState<SalonFormState>(emptySalonForm());
  const [settingsForm, setSettingsForm] = useState<SettingsFormState>(emptySettingsForm());
  const [publicCatalog, setPublicCatalog] = useState<PublicCatalogSettings | null>(null);
  const [publicCatalogForm, setPublicCatalogForm] = useState<PublicCatalogFormState>(emptyPublicCatalogForm());
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [importBundle, setImportBundle] = useState<ConfigurationBundle | null>(null);
  const [importPreview, setImportPreview] = useState<ConfigurationImportResponse | null>(null);
  const [importFileName, setImportFileName] = useState("");
  const [importRequestID, setImportRequestID] = useState("");

  async function load({ silent = false }: { silent?: boolean } = {}) {
    setError("");
    if (!silent) {
      setLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setSettings(null);
        setStatus(null);
        setPeriods([]);
        setSalonForm(emptySalonForm());
        setSettingsForm(emptySettingsForm());
        setPublicCatalog(null);
        setPublicCatalogForm(emptyPublicCatalogForm());
        clearImportState();
        return;
      }

      const [statusResponse, settingsResponse, businessHoursResponse, publicCatalogResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<SalonSettings>(`/api/salons/${firstSalon.id}/settings`),
        apiRequest<BusinessHoursResponse>(`/api/salons/${firstSalon.id}/business-hours`),
        apiRequest<PublicCatalogSettings>(`/api/salons/${firstSalon.id}/public-catalog`)
      ]);

      setStatus(statusResponse);
      setSettings(settingsResponse);
      setPeriods(businessHoursResponse.periods ?? []);
      setPublicCatalog(publicCatalogResponse);
      setSalonForm(salonToForm(firstSalon));
      setSettingsForm(settingsToForm(settingsResponse));
      setPublicCatalogForm(publicCatalogToForm(publicCatalogResponse));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load settings.");
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);
  const activeProvider = salon?.active_pos_provider || "square";
  const activeProviderLabel = activeProvider === "square" ? "Square" : activeProvider;
  const importedProviderPeriods = useMemo(() => periods.filter((period) => isImportedProviderPeriod(period, activeProvider)), [activeProvider, periods]);
  const hasBusinessHourPeriods = importedProviderPeriods.length > 0;
  const latestBusinessHourSync = latestUpdatedAt(...importedProviderPeriods.map((item) => item.last_synced_at || item.updated_at || ""));
  const latestUpdate = latestUpdatedAt(salon?.updated_at, settings?.updated_at, latestBusinessHourSync, publicCatalog?.updated_at);

  async function saveSalonProfile() {
    if (!salon) return;
    if (!salonForm.name.trim() || !salonForm.phone.trim()) {
      setError("Salon name and phone are required.");
      return;
    }

    setBusy("save-salon");
    setError("");
    setSuccess("");
    try {
      const updated = await apiRequest<Salon>(`/api/salons/${salon.id}`, {
        method: "PUT",
        body: JSON.stringify({
          name: salonForm.name,
          phone: salonForm.phone,
          address: salonForm.address,
          city: salonForm.city,
          state: salonForm.state,
          zip_code: salonForm.zipCode,
          timezone: salonForm.timezone,
          primary_language: salonForm.primaryLanguage,
          secondary_language: salonForm.secondaryLanguage,
          handoff_phone: salonForm.handoffPhone,
          ai_enabled: salon.ai_enabled
        })
      });
      setSalon(updated);
      setSalonForm(salonToForm(updated));
      setSuccess("Salon profile saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save salon profile.");
    } finally {
      setBusy("");
    }
  }

  async function saveSettings() {
    if (!salon) return;
    const reminderHours = Number.parseInt(settingsForm.reminderHoursBefore, 10);
    if (!settingsForm.aiGreeting.trim()) {
      setError("AI greeting is required.");
      return;
    }
    if (!settingsForm.recordingConsentMessage.trim()) {
      setError("Recording consent message is required.");
      return;
    }
    if (!Number.isFinite(reminderHours) || reminderHours <= 0) {
      setError("Reminder hours before must be greater than zero.");
      return;
    }

    setBusy("save-settings");
    setError("");
    setSuccess("");
    try {
      const updated = await apiRequest<SalonSettings>(`/api/salons/${salon.id}/settings`, {
        method: "PUT",
        body: JSON.stringify({
          ai_greeting: settingsForm.aiGreeting,
          ai_voice: settings?.ai_voice || "professional_female",
          ai_tone: settingsForm.aiTone,
          booking_mode: settings?.booking_mode || "pending_approval",
          recording_enabled: settingsForm.recordingEnabled,
          recording_consent_message: settingsForm.recordingConsentMessage,
          sms_confirmation_enabled: settingsForm.smsConfirmationEnabled,
          sms_reminder_enabled: settingsForm.smsReminderEnabled,
          reminder_hours_before: reminderHours,
          handoff_enabled: settingsForm.handoffEnabled
        })
      });
      setSettings(updated);
      setSettingsForm(settingsToForm(updated));
      setSuccess("AI receptionist settings saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save AI receptionist settings.");
    } finally {
      setBusy("");
    }
  }

  async function syncSquareRecords() {
    if (!salon) return;
    setBusy("sync-square");
    setError("");
    setSuccess("");
    try {
      await apiRequest<{ ok: boolean }>("/api/integrations/square/sync", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      setSuccess("Square sync completed.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not sync Square records.");
    } finally {
      setBusy("");
    }
  }

  async function savePublicCatalog() {
    if (!salon) return;
    setBusy("save-public-catalog");
    setError("");
    setSuccess("");
    try {
      const updated = await apiRequest<PublicCatalogSettings>(`/api/salons/${salon.id}/public-catalog`, {
        method: "PUT",
        body: JSON.stringify({
          public_slug: publicCatalogForm.publicSlug,
          public_catalog_enabled: publicCatalogForm.enabled
        })
      });
      setPublicCatalog(updated);
      setPublicCatalogForm(publicCatalogToForm(updated));
      setSalon((current) =>
        current
          ? {
              ...current,
              public_slug: updated.public_slug,
              public_catalog_enabled: updated.public_catalog_enabled,
              updated_at: updated.updated_at
            }
          : current
      );
      setSuccess(updated.public_catalog_enabled ? "Public salon page published." : "Public salon page settings saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save public page settings.");
    } finally {
      setBusy("");
    }
  }

  async function exportConfiguration() {
    if (!salon) return;
    setBusy("export-config");
    setError("");
    setSuccess("");
    try {
      await downloadConfigurationExport(salon);
      setSuccess("Configuration export downloaded.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not export configuration.");
    } finally {
      setBusy("");
    }
  }

  async function previewImportFile(file: File | null) {
    if (!salon || !file) return;
    setBusy("preview-config-import");
    setError("");
    setSuccess("");
    try {
      const bundle = await readConfigurationBundle(file);
      const preview = await previewConfigurationImport(salon, bundle);
      setImportBundle(bundle);
      setImportPreview(preview);
      setImportFileName(file.name);
      setImportRequestID(newConfigurationImportRequestID());
      setSuccess(preview.can_apply ? "Configuration import preview is ready." : "Configuration import preview has conflicts.");
    } catch (err) {
      clearImportState();
      setError(err instanceof Error ? err.message : "Could not preview configuration import.");
    } finally {
      setBusy("");
    }
  }

  async function applyImportPreview() {
    if (!salon || !importBundle || !importPreview?.can_apply) return;
    const requestID = importRequestID || newConfigurationImportRequestID();
    setBusy("apply-config-import");
    setError("");
    setSuccess("");
    try {
      const applied = await applyConfigurationImport(salon, importBundle, requestID);
      setImportPreview(applied);
      setImportRequestID(applied.request_id);
      setSuccess("Configuration import applied.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not apply configuration import.");
    } finally {
      setBusy("");
    }
  }

  function clearImportState() {
    setImportBundle(null);
    setImportPreview(null);
    setImportFileName("");
    setImportRequestID("");
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <div className="grid gap-4 md:grid-cols-3">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <Skeleton className="h-80" />
        <Skeleton className="h-96" />
      </div>
    );
  }

  if (error && !salon) {
    return (
      <div className="space-y-4">
        <Alert title="Settings unavailable" message={error} />
        <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
          <RefreshCcw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>Settings and business hours are scoped by salon, so the owner profile must exist first.</CardDescription>
        <div className="mt-5">
          <Link
            href="/onboarding"
            className="inline-flex h-10 items-center justify-center rounded-md bg-brand px-4 text-sm font-semibold text-white hover:bg-teal-800"
          >
            Create salon profile
          </Link>
        </div>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Settings</h1>
          <p className="mt-1 text-sm text-muted">
            Configure salon profile, AI receptionist behavior, handoff, SMS, and synced Square hours.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Settings update failed" message={error} /> : null}
      {success ? <Alert type="success" title="Settings updated" message={success} /> : null}

      <ReadinessGate aiEnabled={aiEnabled} activeProvider={activeProvider} status={status} />

      <div className="grid gap-4 md:grid-cols-3">
        <StatusMetric label="Active provider" value={activeProvider === "square" ? "Square Appointments" : activeProvider} badge="booking" />
        <StatusMetric label="Business hours" value={hasBusinessHourPeriods ? "Synced" : "Missing"} badge={hasBusinessHourPeriods ? "ready" : "blocked"} />
        <StatusMetric label="Last saved" value={latestUpdate ? formatDateTime(latestUpdate) : "Not available"} badge={latestUpdate ? "synced" : "not_configured"} />
      </div>

      <ConfigurationTransferCard
        busy={busy}
        fileName={importFileName}
        preview={importPreview}
        onApply={() => void applyImportPreview()}
        onClear={clearImportState}
        onExport={() => void exportConfiguration()}
        onPreviewFile={(file) => void previewImportFile(file)}
      />

      <PublicCatalogCard
        settings={publicCatalog}
        form={publicCatalogForm}
        busy={busy === "save-public-catalog"}
        onChange={setPublicCatalogForm}
        onSave={() => void savePublicCatalog()}
      />

      <SalonProfileForm form={salonForm} busy={busy === "save-salon"} onChange={setSalonForm} onSave={() => void saveSalonProfile()} />

      <AISettingsForm
        form={settingsForm}
        aiEnabled={aiEnabled}
        busy={busy === "save-settings"}
        onChange={setSettingsForm}
        onSave={() => void saveSettings()}
      />

      <BusinessHoursCard
        periods={importedProviderPeriods}
        sourceLabel={activeProviderLabel}
        hasSyncedSquarePeriods={hasBusinessHourPeriods}
        busy={busy === "sync-square"}
        lastSyncedAt={latestBusinessHourSync}
        onSync={() => void syncSquareRecords()}
      />
    </div>
  );
}

function ReadinessGate({
  aiEnabled,
  activeProvider,
  status
}: {
  aiEnabled: boolean;
  activeProvider: string;
  status: StatusResponse | null;
}) {
  const squareConnected = Boolean(status?.connection?.id) && status?.connection?.status !== "not_connected";
  const lastSync = status?.connection?.last_sync_at;
  const readyDescription = aiEnabled
    ? "AI booking is enabled through Square setup. The receptionist can only confirm appointments after Square Appointments returns a booking ID."
    : status?.readiness?.checks?.find((item) => !item.complete)?.message ||
      "Enable AI booking from Square setup after Square Appointments is connected, synced, tested, and cancelled successfully.";

  return (
    <Card className={aiEnabled ? "border-emerald-200 bg-emerald-50 shadow-none" : "border-amber-200 bg-amber-50 shadow-none"}>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="flex gap-3">
          {aiEnabled ? (
            <ShieldCheck className="mt-0.5 h-5 w-5 flex-none text-emerald-700" />
          ) : (
            <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
          )}
          <div>
            <CardTitle>AI booking is controlled from Square setup</CardTitle>
            <CardDescription className={aiEnabled ? "text-emerald-800" : "text-amber-900"}>
              {readyDescription}
            </CardDescription>
            <div className="mt-2 text-xs leading-5 text-muted">
              Provider: {activeProvider === "square" ? "Square Appointments" : activeProvider}.{" "}
              {squareConnected ? `Last sync: ${formatOptionalDate(lastSync)}.` : "Square Appointments is not fully connected."}
            </div>
          </div>
        </div>
        <Link
          href="/dashboard/integrations"
          className="inline-flex h-10 flex-none items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
        >
          Open Square setup
        </Link>
      </div>
    </Card>
  );
}

function StatusMetric({ label, value, badge }: { label: string; value: string; badge: string }) {
  return (
    <Card>
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-xs font-semibold uppercase text-muted">{label}</div>
          <div className="mt-2 text-base font-semibold text-ink">{value}</div>
        </div>
        <Badge value={badge} />
      </div>
    </Card>
  );
}

function ConfigurationTransferCard({
  busy,
  fileName,
  preview,
  onApply,
  onClear,
  onExport,
  onPreviewFile
}: {
  busy: string;
  fileName: string;
  preview: ConfigurationImportResponse | null;
  onApply: () => void;
  onClear: () => void;
  onExport: () => void;
  onPreviewFile: (file: File | null) => void;
}) {
  const exportBusy = busy === "export-config";
  const previewBusy = busy === "preview-config-import";
  const applyBusy = busy === "apply-config-import";
  const disabled = busy !== "";

  return (
    <Card>
      <div className="flex flex-col justify-between gap-4 md:flex-row md:items-start">
        <div className="flex gap-3">
          <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand">
            <FileJson className="h-5 w-5" />
          </div>
          <div>
            <CardTitle>Configuration transfer</CardTitle>
            <CardDescription>
              Export or import salon profile, AI receptionist, public booking page, integrations, and AI Training knowledge base in one JSON bundle.
            </CardDescription>
          </div>
        </div>
        <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
          <Badge value={preview?.can_apply === false ? "conflict" : "ready"} />
          <Button type="button" onClick={onExport} disabled={disabled}>
            <Download className="h-4 w-4" />
            {exportBusy ? "Exporting..." : "Export JSON"}
          </Button>
        </div>
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-[0.8fr_1.2fr]">
        <div className="rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Included</div>
          <div className="mt-3 flex flex-wrap gap-2">
            {["Salon profile", "AI receptionist", "Public booking page", "Integrations", "Service categories", "Service aliases", "Knowledge base"].map((item) => (
              <Badge key={item} value={item.toLowerCase().replaceAll(" ", "_")} />
            ))}
          </div>
          <div className="mt-5 text-sm font-semibold text-ink">Excluded</div>
          <div className="mt-2 text-sm leading-6 text-muted">
            Services, staff, customers, appointments, fallback requests, call data, POS connection state, business-hour sync periods, POS tokens, API keys, and client secrets.
          </div>
        </div>

        <div className="rounded-md border border-line p-4">
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <div className="text-sm font-semibold text-ink">Import configuration</div>
              <div className="mt-1 text-sm leading-6 text-muted">
                Preview validates schema, conflicts, skipped fields, and repeated-import behavior before any write.
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

          {fileName ? <div className="mt-3 break-all text-xs text-muted">Selected file: {fileName}</div> : null}

          {preview ? (
            <div className="mt-5 space-y-4">
              <ImportSummaryTable summary={preview.summary} />
              <ImportIssueList title="Conflicts" issues={preview.conflicts} tone="danger" />
              <ImportIssueList title="Warnings" issues={preview.warnings} tone="warning" />
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="text-xs leading-5 text-muted">
                  Schema {preview.schema_version}. Secrets must be re-entered for: {listOrNone(preview.requires_secret_reentry)}.
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
              Choose a ManleAI configuration JSON file to preview changes. Preview does not write to the salon.
            </div>
          )}
        </div>
      </div>
    </Card>
  );
}

function PublicCatalogCard({
  settings,
  form,
  busy,
  onChange,
  onSave
}: {
  settings: PublicCatalogSettings | null;
  form: PublicCatalogFormState;
  busy: boolean;
  onChange: (next: PublicCatalogFormState) => void;
  onSave: () => void;
}) {
  const serviceCount = settings?.bookable_service_count ?? 0;
  const staffCount = settings?.bookable_staff_count ?? 0;
  const publicPath = settings?.public_path || (form.publicSlug ? `/s/${form.publicSlug}` : "");
  const publicUrl = publicPath ? `${landingBaseUrl}${publicPath}` : "";
  const canPublishWithForm = form.publicSlug.trim().length >= 3 && serviceCount > 0 && staffCount > 0;
  const blockedReason =
    settings?.blocked_reason ||
    (!form.publicSlug.trim()
      ? "Add a public page slug before publishing."
      : serviceCount === 0
        ? "At least one synced AI-bookable service is required."
        : staffCount === 0
          ? "At least one synced AI-bookable staff member is required."
          : "");

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="flex gap-3">
          <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand">
            <Globe2 className="h-5 w-5" />
          </div>
          <div>
            <CardTitle>Public booking page</CardTitle>
            <CardDescription>
              Publish a customer-facing page for services, staff, hours, and phone booking requests.
            </CardDescription>
          </div>
        </div>
        <Badge value={settings?.public_catalog_enabled ? "published" : "disabled"} className="self-start" />
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-[1.3fr_0.7fr]">
        <Field label="Public slug">
          <div className="flex flex-col gap-2 sm:flex-row">
            <TextInput
              value={form.publicSlug}
              disabled={busy}
              onChange={(value) => onChange({ ...form, publicSlug: value })}
            />
            {publicUrl ? (
              <a
                href={publicUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex h-10 flex-none items-center justify-center gap-2 rounded-md border border-line px-4 text-sm font-semibold text-ink hover:bg-slate-50"
              >
                <ExternalLink className="h-4 w-4" />
                Open
              </a>
            ) : null}
          </div>
          <div className="mt-2 break-all text-xs leading-5 text-muted">
            {publicUrl || `${landingBaseUrl}/s/your-salon-slug`}
          </div>
        </Field>
        <div className="rounded-md border border-line p-3">
          <div className="text-xs font-semibold uppercase text-muted">Public-ready catalog</div>
          <div className="mt-3 grid grid-cols-2 gap-3">
            <MiniMetric label="Services" value={String(serviceCount)} />
            <MiniMetric label="Staff" value={String(staffCount)} />
          </div>
        </div>
      </div>

      {form.enabled && !canPublishWithForm ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          {blockedReason}
        </div>
      ) : null}

      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <CheckboxRow
          label="Publish public page"
          checked={form.enabled}
          disabled={busy}
          onChange={(checked) => onChange({ ...form, enabled: checked })}
        />
        <Button type="button" onClick={onSave} disabled={busy || (form.enabled && !canPublishWithForm)}>
          <Save className="h-4 w-4" />
          {busy ? "Saving..." : "Save public page"}
        </Button>
      </div>
    </Card>
  );
}

function MiniMetric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-lg font-bold text-ink">{value}</div>
      <div className="text-xs text-muted">{label}</div>
    </div>
  );
}

function SalonProfileForm({
  form,
  busy,
  onChange,
  onSave
}: {
  form: SalonFormState;
  busy: boolean;
  onChange: (next: SalonFormState) => void;
  onSave: () => void;
}) {
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Salon profile</CardTitle>
          <CardDescription>Owner-facing salon details used by receptionist handoff and dashboard workflows.</CardDescription>
        </div>
        <Button type="button" onClick={onSave} disabled={busy}>
          <Save className="h-4 w-4" />
          {busy ? "Saving..." : "Save salon profile"}
        </Button>
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-2">
        <Field label="Salon name">
          <TextInput value={form.name} disabled={busy} onChange={(value) => onChange({ ...form, name: value })} />
        </Field>
        <Field label="Phone">
          <TextInput value={form.phone} disabled={busy} onChange={(value) => onChange({ ...form, phone: value })} />
        </Field>
        <Field label="Address">
          <TextInput value={form.address} disabled={busy} onChange={(value) => onChange({ ...form, address: value })} />
        </Field>
        <Field label="City">
          <TextInput value={form.city} disabled={busy} onChange={(value) => onChange({ ...form, city: value })} />
        </Field>
        <Field label="State">
          <TextInput value={form.state} disabled={busy} onChange={(value) => onChange({ ...form, state: value })} />
        </Field>
        <Field label="ZIP code">
          <TextInput value={form.zipCode} disabled={busy} onChange={(value) => onChange({ ...form, zipCode: value })} />
        </Field>
        <Field label="Timezone">
          <TextInput value={form.timezone} disabled={busy} onChange={(value) => onChange({ ...form, timezone: value })} />
        </Field>
        <Field label="Owner handoff phone">
          <TextInput value={form.handoffPhone} disabled={busy} onChange={(value) => onChange({ ...form, handoffPhone: value })} />
        </Field>
        <Field label="Primary language">
          <TextInput value={form.primaryLanguage} disabled={busy} onChange={(value) => onChange({ ...form, primaryLanguage: value })} />
        </Field>
        <Field label="Secondary language">
          <TextInput value={form.secondaryLanguage} disabled={busy} onChange={(value) => onChange({ ...form, secondaryLanguage: value })} />
        </Field>
      </div>
    </Card>
  );
}

function AISettingsForm({
  form,
  aiEnabled,
  busy,
  onChange,
  onSave
}: {
  form: SettingsFormState;
  aiEnabled: boolean;
  busy: boolean;
  onChange: (next: SettingsFormState) => void;
  onSave: () => void;
}) {
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>AI receptionist</CardTitle>
          <CardDescription>Call greeting, speaking style, recording consent, handoff, and SMS behavior.</CardDescription>
        </div>
        <Badge value={aiEnabled ? "active" : "ai_disabled"} className="self-start" />
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-2">
        <Field label="AI greeting">
          <textarea
            className="min-h-28 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
            value={form.aiGreeting}
            onChange={(event) => onChange({ ...form, aiGreeting: event.target.value })}
            disabled={busy}
          />
        </Field>
        <Field label="Recording consent message">
          <textarea
            className="min-h-28 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
            value={form.recordingConsentMessage}
            onChange={(event) => onChange({ ...form, recordingConsentMessage: event.target.value })}
            disabled={busy}
          />
        </Field>
        <Field label="Speaking style">
          <select
            className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
            value={form.aiTone}
            onChange={(event) => onChange({ ...form, aiTone: event.target.value })}
            disabled={busy}
          >
            {aiToneOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          <div className="mt-3 rounded-md border border-line bg-slate-50 px-3 py-2 text-xs leading-5 text-muted">
            <span className="font-semibold text-ink">Style preview:</span> {tonePreview(form.aiTone)}
          </div>
        </Field>
        <Field label="Reminder hours before">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
            type="number"
            min="1"
            value={form.reminderHoursBefore}
            onChange={(event) => onChange({ ...form, reminderHoursBefore: event.target.value })}
            disabled={busy}
          />
        </Field>
      </div>

      <div className="mt-5 grid gap-3 md:grid-cols-2">
        <CheckboxRow label="Recording enabled" checked={form.recordingEnabled} disabled={busy} onChange={(checked) => onChange({ ...form, recordingEnabled: checked })} />
        <CheckboxRow
          label="SMS confirmation enabled"
          checked={form.smsConfirmationEnabled}
          disabled={busy}
          onChange={(checked) => onChange({ ...form, smsConfirmationEnabled: checked })}
        />
        <CheckboxRow
          label="SMS reminder enabled"
          checked={form.smsReminderEnabled}
          disabled={busy}
          onChange={(checked) => onChange({ ...form, smsReminderEnabled: checked })}
        />
        <CheckboxRow label="Owner handoff enabled" checked={form.handoffEnabled} disabled={busy} onChange={(checked) => onChange({ ...form, handoffEnabled: checked })} />
      </div>

      <div className="mt-5 flex justify-end">
        <Button type="button" onClick={onSave} disabled={busy}>
          <Save className="h-4 w-4" />
          {busy ? "Saving..." : "Save AI settings"}
        </Button>
      </div>
    </Card>
  );
}

function BusinessHoursCard({
  periods,
  sourceLabel,
  hasSyncedSquarePeriods,
  busy,
  lastSyncedAt,
  onSync
}: {
  periods: BusinessHourPeriod[];
  sourceLabel: string;
  hasSyncedSquarePeriods: boolean;
  busy: boolean;
  lastSyncedAt: string;
  onSync: () => void;
}) {
  const periodsByDay = groupPeriodsByDay(periods);

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Business hours</CardTitle>
          <CardDescription>
            Synced from Square Appointments. Availability checks reject requested times outside these periods.
          </CardDescription>
          <div className="mt-2 text-xs leading-5 text-muted">
            Edit operating hours in Square, then sync here. Last sync: {lastSyncedAt ? formatDateTime(lastSyncedAt) : "not synced"}.
          </div>
        </div>
        <Badge value={hasSyncedSquarePeriods ? "ready" : "blocked"} className="self-start" />
      </div>

      {!hasSyncedSquarePeriods ? (
        <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          No Square business hour periods are synced yet. Run Sync after selecting the correct Square location.
        </div>
      ) : null}

      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line md:block">
        <table className="w-full min-w-[760px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Day</th>
              <th className="px-4 py-3">Open periods</th>
              <th className="px-4 py-3">Source</th>
              <th className="px-4 py-3">Last sync</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line bg-white">
            {dayLabels.map((day, dayOfWeek) => {
              const dayPeriods = periodsByDay.get(dayOfWeek) ?? [];
              return (
                <tr key={day}>
                  <td className="px-4 py-3 font-medium text-ink">{day}</td>
                  <td className="px-4 py-3 text-ink">
                    {dayPeriods.length ? formatPeriodList(dayPeriods) : <span className="text-muted">Closed or not synced</span>}
                  </td>
                  <td className="px-4 py-3 text-muted">{formatPeriodSource(dayPeriods, sourceLabel)}</td>
                  <td className="px-4 py-3 text-muted">{formatDayLastSynced(dayPeriods)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <div className="mt-5 space-y-3 md:hidden">
        {dayLabels.map((day, dayOfWeek) => {
          const dayPeriods = periodsByDay.get(dayOfWeek) ?? [];
          return (
            <div key={day} className="rounded-md border border-line p-4">
              <div className="flex items-center justify-between gap-3">
                <div className="text-sm font-semibold text-ink">{day}</div>
                <Badge value={dayPeriods.length ? "open" : "closed"} />
              </div>
              <div className="mt-3 text-sm text-ink">
                {dayPeriods.length ? formatPeriodList(dayPeriods) : "Closed or not synced"}
              </div>
              <div className="mt-2 text-xs leading-5 text-muted">
                {formatPeriodSource(dayPeriods, sourceLabel)} - {formatDayLastSynced(dayPeriods)}
              </div>
            </div>
          );
        })}
      </div>

      <div className="mt-5 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <Link
          href="/dashboard/integrations"
          className="inline-flex h-10 items-center justify-center rounded-md border border-line px-4 text-sm font-semibold text-ink hover:bg-slate-50"
        >
          Open Square setup
        </Link>
        <Button type="button" onClick={onSync} disabled={busy}>
          <RefreshCcw className="h-4 w-4" />
          {busy ? "Syncing..." : "Sync"}
        </Button>
      </div>
    </Card>
  );
}

function groupPeriodsByDay(periods: BusinessHourPeriod[]) {
  const grouped = new Map<number, BusinessHourPeriod[]>();
  for (const period of periods) {
    const next = grouped.get(period.day_of_week) ?? [];
    next.push(period);
    grouped.set(
      period.day_of_week,
      next.sort((a, b) => a.start_local_time.localeCompare(b.start_local_time))
    );
  }
  return grouped;
}

function formatPeriodList(periods: BusinessHourPeriod[]) {
  return periods.map((period) => `${toDisplayTime(period.start_local_time)}-${toDisplayTime(period.end_local_time)}`).join(", ");
}

function formatPeriodSource(periods: BusinessHourPeriod[], sourceLabel: string) {
  if (periods.length === 0) return "Not synced";
  return sourceLabel;
}

function isImportedProviderPeriod(period: BusinessHourPeriod, provider: string) {
  return period.source === "imported" && period.provider === provider;
}

function formatDayLastSynced(periods: BusinessHourPeriod[]) {
  const value = latestUpdatedAt(...periods.map((period) => period.last_synced_at || period.updated_at || ""));
  return value ? formatDateTime(value) : "Not synced";
}

function toDisplayTime(value?: string) {
  if (!value) return "";
  return value.slice(0, 5);
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block text-sm font-medium text-ink">
      <span>{label}</span>
      <div className="mt-2">{children}</div>
    </label>
  );
}

function TextInput({
  value,
  disabled,
  onChange
}: {
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <input
      className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
      value={value}
      onChange={(event) => onChange(event.target.value)}
      disabled={disabled}
    />
  );
}

function CheckboxRow({
  label,
  checked,
  disabled,
  onChange
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm font-medium text-ink">
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} disabled={disabled} />
      {label}
    </label>
  );
}

function emptySalonForm(): SalonFormState {
  return {
    name: "",
    phone: "",
    address: "",
    city: "",
    state: "",
    zipCode: "",
    timezone: "America/Chicago",
    primaryLanguage: "en",
    secondaryLanguage: "vi",
    handoffPhone: ""
  };
}

function salonToForm(salon: Salon): SalonFormState {
  return {
    name: salon.name || "",
    phone: salon.phone || "",
    address: salon.address || "",
    city: salon.city || "",
    state: salon.state || "",
    zipCode: salon.zip_code || "",
    timezone: salon.timezone || "America/Chicago",
    primaryLanguage: salon.primary_language || "en",
    secondaryLanguage: salon.secondary_language || "vi",
    handoffPhone: salon.handoff_phone || ""
  };
}

function emptySettingsForm(): SettingsFormState {
  return {
    aiGreeting: "",
    aiTone: "professional_warm",
    recordingEnabled: true,
    recordingConsentMessage: "",
    smsConfirmationEnabled: true,
    smsReminderEnabled: true,
    reminderHoursBefore: "24",
    handoffEnabled: true
  };
}

function emptyPublicCatalogForm(): PublicCatalogFormState {
  return {
    publicSlug: "",
    enabled: false
  };
}

function settingsToForm(settings: SalonSettings): SettingsFormState {
  return {
    aiGreeting: settings.ai_greeting || "",
    aiTone: settings.ai_tone || "professional_warm",
    recordingEnabled: settings.recording_enabled,
    recordingConsentMessage: settings.recording_consent_message || "",
    smsConfirmationEnabled: settings.sms_confirmation_enabled,
    smsReminderEnabled: settings.sms_reminder_enabled,
    reminderHoursBefore: String(settings.reminder_hours_before || 24),
    handoffEnabled: settings.handoff_enabled
  };
}

function tonePreview(value: string) {
  return aiToneOptions.find((option) => option.value === value)?.preview ?? aiToneOptions[0].preview;
}

function publicCatalogToForm(settings: PublicCatalogSettings): PublicCatalogFormState {
  return {
    publicSlug: settings.public_slug || "",
    enabled: settings.public_catalog_enabled
  };
}

function latestUpdatedAt(...values: Array<string | undefined>) {
  const timestamps = values
    .filter(Boolean)
    .map((value) => new Date(value || "").getTime())
    .filter((value) => Number.isFinite(value));
  if (timestamps.length === 0) return "";
  return new Date(Math.max(...timestamps)).toISOString();
}

function formatDateTime(value: string) {
  return new Date(value).toLocaleString();
}

function formatOptionalDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "not synced";
}
