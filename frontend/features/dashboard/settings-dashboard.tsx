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
import { useTenantSalon } from "@/components/layout/tenant-salon-context";
import { ImportIssueList, ImportScopePreview, ImportSummaryTable, listOrNone } from "@/features/configuration-transfer/import-preview";
import { InternalCalendarSetup } from "@/features/dashboard/internal-calendar-setup";
import { CustomerSMSPolicyCard } from "@/features/dashboard/customer-sms-policy";
import { OperationsHealth } from "@/features/dashboard/operations-health";
import { SchedulingAuthoritySwitch } from "@/features/dashboard/scheduling-authority-switch";
import { apiRequest, RequestError } from "@/lib/api/client";
import {
  applyConfigurationImport,
  downloadConfigurationExport,
  newConfigurationImportRequestID,
  previewConfigurationImport,
  readConfigurationBundle
} from "@/lib/api/configuration-transfer";
import { landingBaseUrl } from "@/lib/config/env";
import { serviceEligibleForAuthority } from "@/lib/api/scheduling-evidence";
import type {
  BusinessHourPeriod,
  BookingMode,
  ConfigurationBundle,
  ConfigurationImportResponse,
  POSService,
  PublicCatalogSettings,
  Salon,
  SalonSettings,
  SchedulingAuthority
} from "@/types/api";

type ExternalSchedulingReadiness = {
  scheduling_authority: SchedulingAuthority;
  ready_for_external_new_work: boolean;
  service_count: number;
  staff_count: number;
  business_hour_period_count: number;
  booking_write_blocked: boolean;
};

type BusinessHoursResponse = {
  periods: BusinessHourPeriod[];
};

type ServicesResponse = {
  services: POSService[];
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
  bookingMode: BookingMode;
  recordingEnabled: boolean;
  recordingConsentMessage: string;
  smsConfirmationEnabled: boolean;
  smsReminderEnabled: boolean;
  reminderHoursBefore: string;
  handoffEnabled: boolean;
  consultationEnabled: boolean;
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

const bookingModeOptions: Array<{ value: BookingMode; label: string }> = [
  { value: "pending_approval", label: "Owner approval required" },
  { value: "confirmed_booking", label: "Confirm automatically" },
  { value: "disabled", label: "Scheduling disabled" }
];

export function SettingsDashboard() {
  const tenant = useTenantSalon();
  const [salon, setSalon] = useState<Salon | null>(null);
  const [settings, setSettings] = useState<SalonSettings | null>(null);
  const [externalReadiness, setExternalReadiness] = useState<ExternalSchedulingReadiness | null>(null);
  const [externalReadinessError, setExternalReadinessError] = useState("");
  const [periods, setPeriods] = useState<BusinessHourPeriod[]>([]);
  const [services, setServices] = useState<POSService[]>([]);
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
      const activeSalon = tenant.activeSalonID
        ? await apiRequest<Salon>(`/api/salons/${tenant.activeSalonID}`)
        : null;
      setSalon(activeSalon);
      if (!activeSalon) {
        setSettings(null);
        setExternalReadiness(null);
        setExternalReadinessError("");
        setPeriods([]);
        setServices([]);
        setSalonForm(emptySalonForm());
        setSettingsForm(emptySettingsForm());
        setPublicCatalog(null);
        setPublicCatalogForm(emptyPublicCatalogForm());
        clearImportState();
        return;
      }

      const [readinessResult, settingsResponse, businessHoursResponse, publicCatalogResponse, servicesResponse] = await Promise.all([
        apiRequest<ExternalSchedulingReadiness>(`/api/salons/${activeSalon.id}/business/external-scheduling-readiness`)
          .then((value) => ({ value, error: "" }))
          .catch((readinessError: unknown) => ({ value: null, error: errorMessage(readinessError, "Could not load external scheduling readiness.") })),
        apiRequest<SalonSettings>(`/api/salons/${activeSalon.id}/settings`),
        apiRequest<BusinessHoursResponse>(`/api/salons/${activeSalon.id}/business-hours`),
        apiRequest<PublicCatalogSettings>(`/api/salons/${activeSalon.id}/public-catalog`),
        apiRequest<ServicesResponse>(`/api/salons/${activeSalon.id}/services`)
      ]);

      setExternalReadiness(readinessResult.value);
      setExternalReadinessError(readinessResult.error);
      setSettings(settingsResponse);
      setPeriods(businessHoursResponse.periods ?? []);
      setServices(servicesResponse.services ?? []);
      setPublicCatalog(publicCatalogResponse);
      setSalonForm(salonToForm(activeSalon));
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
    if (!tenant.loading) void load();
  }, [tenant.activeSalonID, tenant.loading]);

  const aiEnabled = Boolean(salon?.ai_enabled);
  const activeProvider = salon?.active_pos_provider || "square";
  const consultationEligibleServices = useMemo(
    () => services.filter((service) => serviceEligibleForAuthority(service, salon?.scheduling_authority, activeProvider)),
    [activeProvider, salon?.scheduling_authority, services]
  );
  const consultationReadyCount = useMemo(
    () =>
      consultationEligibleServices.filter(
        (service) =>
          service.consultation_profile?.status === "ready" &&
          service.consultation_profile.recommended_outcomes.length > 0 &&
          service.consultation_profile.compatible_current_systems.length > 0
      ).length,
    [consultationEligibleServices]
  );
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
    if (settingsForm.consultationEnabled && consultationReadyCount === 0) {
      setError("Complete and mark at least one eligible service consultation profile as ready before enabling AI service consultation.");
      return;
    }
    if (settingsForm.bookingMode === "confirmed_booking" && settings?.scheduling_authority === "owner_manual") {
      setError("Owner-managed scheduling can record requests for review, but it cannot confirm appointments automatically.");
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
          booking_mode: settingsForm.bookingMode,
          recording_enabled: settingsForm.recordingEnabled,
          recording_consent_message: settingsForm.recordingConsentMessage,
          sms_confirmation_enabled: settingsForm.smsConfirmationEnabled,
          sms_reminder_enabled: settingsForm.smsReminderEnabled,
          reminder_hours_before: reminderHours,
          handoff_enabled: settingsForm.handoffEnabled,
          consultation_enabled: settingsForm.consultationEnabled
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
          public_catalog_enabled: publicCatalogForm.enabled,
          expected_scheduling_authority_version: publicCatalog?.scheduling_authority_version
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
      if (err instanceof RequestError && (err.code === "SCHEDULING_AUTHORITY_CHANGED" || err.code === "PUBLIC_CATALOG_NOT_READY")) {
        try {
          const current = await apiRequest<PublicCatalogSettings>(`/api/salons/${salon.id}/public-catalog`);
          setPublicCatalog(current);
          setPublicCatalogForm(publicCatalogToForm(current));
        } catch {
          // Keep the typed conflict as the primary actionable error.
        }
      }
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

  if (loading || tenant.loading) {
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
            Configure salon profile, ManleAI Calendar, AI receptionist behavior, and optional provider settings.
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
      {externalReadinessError ? <Alert title="External scheduling readiness unavailable" message={`${externalReadinessError} Internal calendar settings remain available.`} /> : null}

      {settings && Number.isInteger(settings.scheduling_authority_version) && settings.scheduling_authority_version > 0 ? (
        <SchedulingAuthoritySwitch
          salonID={salon.id}
          currentAuthority={settings.scheduling_authority}
          currentVersion={settings.scheduling_authority_version}
          onReload={() => load({ silent: true })}
        />
      ) : (
        <Card>
          <CardTitle>Scheduling authority unavailable</CardTitle>
          <CardDescription>The settings response did not include current authority state. Reload before selecting or switching authority.</CardDescription>
        </Card>
      )}

      <InternalCalendarSetup salonID={salon.id} timezone={salon.timezone} />

      <CustomerSMSPolicyCard salonID={salon.id} />

      {settings?.scheduling_authority === "external_provider" ? (
        <ReadinessGate aiEnabled={aiEnabled} activeProvider={activeProvider} readiness={externalReadiness} />
      ) : null}

      <div className="grid gap-4 md:grid-cols-3">
        <StatusMetric label="Active provider" value={activeProvider === "square" ? "Square Appointments" : activeProvider} badge="booking" />
        <StatusMetric label="Business hours" value={hasBusinessHourPeriods ? "Synced" : "Missing"} badge={hasBusinessHourPeriods ? "ready" : "blocked"} />
        <StatusMetric label="Last saved" value={latestUpdate ? formatDateTime(latestUpdate) : "Not available"} badge={latestUpdate ? "synced" : "not_configured"} />
      </div>

      <OperationsHealth salonID={salon.id} />

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
        schedulingAuthority={settings?.scheduling_authority ?? "owner_manual"}
        aiEnabled={aiEnabled}
        consultationReadyCount={consultationReadyCount}
        consultationEligibleCount={consultationEligibleServices.length}
        busy={busy === "save-settings"}
        onChange={setSettingsForm}
        onSave={() => void saveSettings()}
      />

      <BusinessHoursCard
        periods={importedProviderPeriods}
        sourceLabel={activeProviderLabel}
        hasSyncedSquarePeriods={hasBusinessHourPeriods}
        lastSyncedAt={latestBusinessHourSync}
      />
    </div>
  );
}

function ReadinessGate({
  aiEnabled,
  activeProvider,
  readiness
}: {
  aiEnabled: boolean;
  activeProvider: string;
  readiness: ExternalSchedulingReadiness | null;
}) {
  const readyForExternalNewWork = Boolean(readiness?.ready_for_external_new_work);
  const readyDescription = readyForExternalNewWork
    ? "External scheduling is ready for new work. The receptionist still confirms only after the selected provider returns the required durable booking evidence."
    : "External scheduling is not ready for new work. Provider setup and diagnostics remain in Platform Technical settings.";

  return (
    <Card className={readyForExternalNewWork ? "border-emerald-200 bg-emerald-50 shadow-none" : "border-amber-200 bg-amber-50 shadow-none"}>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="flex gap-3">
          {readyForExternalNewWork ? (
            <ShieldCheck className="mt-0.5 h-5 w-5 flex-none text-emerald-700" />
          ) : (
            <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
          )}
          <div>
            <CardTitle>AI booking is controlled from Square setup</CardTitle>
            <CardDescription className={readyForExternalNewWork ? "text-emerald-800" : "text-amber-900"}>
              {readyDescription}
            </CardDescription>
            <div className="mt-2 text-xs leading-5 text-muted">
              Provider: {activeProvider === "square" ? "Square Appointments" : activeProvider}.{" "}
              {readiness ? `${readiness.service_count} services · ${readiness.staff_count} staff · ${readiness.business_hour_period_count} hour periods.` : "Readiness evidence is unavailable."}
            </div>
          </div>
        </div>
        <Badge value={aiEnabled ? "active" : "disabled"} className="self-start" />
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
              Export portable salon intent or import a scoped data pack without changing scheduling authority.
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
          <div className="text-sm font-semibold text-ink">Full export includes</div>
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

        <div className="rounded-md border border-line p-4">
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <div className="text-sm font-semibold text-ink">Import configuration</div>
              <div className="mt-1 text-sm leading-6 text-muted">
                Preview validates schema, destination authority compatibility, conflicts, and repeated-import behavior before any write.
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
              <ImportScopePreview preview={preview} />
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
  const serviceCount = settings?.eligible_service_count ?? settings?.bookable_service_count ?? 0;
  const staffCount = settings?.eligible_staff_count ?? settings?.bookable_staff_count ?? 0;
  const hoursCount = settings?.published_hours_count ?? 0;
  const publicPath = settings?.public_path || (form.publicSlug ? `/s/${form.publicSlug}` : "");
  const publicUrl = publicPath ? `${landingBaseUrl}${publicPath}` : "";
  const backendBlockers = settings?.readiness_blockers ?? [];
  const nonSlugBlockers = backendBlockers.filter((blocker) => blocker.code !== "PUBLIC_SLUG_REQUIRED");
  const canPublishWithForm = form.publicSlug.trim().length >= 3 && nonSlugBlockers.length === 0;
  const visibleBlockers = form.publicSlug.trim().length >= 3
    ? nonSlugBlockers
    : [
        { code: "PUBLIC_SLUG_REQUIRED", scope: "public_page", message: "Add a public page slug before publishing." },
        ...nonSlugBlockers
      ];

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="flex gap-3">
          <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand">
            <Globe2 className="h-5 w-5" />
          </div>
          <div>
            <CardTitle>Public salon page</CardTitle>
            <CardDescription>
              Publish services, staff, and authority-safe hours. Customers call the salon to request an appointment.
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
          <div className="text-xs font-semibold uppercase text-muted">{settings?.readiness_label || "Public page readiness"}</div>
          <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
            <MiniMetric label="Services" value={String(serviceCount)} />
            <MiniMetric label="Staff" value={String(staffCount)} />
            <MiniMetric label="Hour periods" value={String(hoursCount)} />
          </div>
          <div className="mt-3 text-xs leading-5 text-muted">
            {settings ? `${publicAuthorityLabel(settings.scheduling_authority)} · authority version ${settings.scheduling_authority_version}` : "Loading scheduling method..."}
          </div>
        </div>
      </div>

      {form.enabled && !canPublishWithForm ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900" role="alert">
          <div className="font-semibold">Resolve before publishing</div>
          <ul className="mt-2 list-disc space-y-1 pl-5">
            {visibleBlockers.map((blocker) => <li key={`${blocker.scope}:${blocker.code}`}>{blocker.message}</li>)}
          </ul>
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

function publicAuthorityLabel(authority: PublicCatalogSettings["scheduling_authority"]) {
  switch (authority) {
    case "owner_manual":
      return "Owner-managed requests";
    case "manleai_calendar":
      return "ManleAI Calendar";
    case "external_provider":
      return "Connected scheduling";
  }
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
  schedulingAuthority,
  aiEnabled,
  consultationReadyCount,
  consultationEligibleCount,
  busy,
  onChange,
  onSave
}: {
  form: SettingsFormState;
  schedulingAuthority: SchedulingAuthority;
  aiEnabled: boolean;
  consultationReadyCount: number;
  consultationEligibleCount: number;
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
        <Field label="AI scheduling behavior">
          <select
            className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-slate-400"
            value={form.bookingMode}
            onChange={(event) => onChange({ ...form, bookingMode: event.target.value as BookingMode })}
            disabled={busy}
          >
            {bookingModeOptions.map((option) => (
              <option
                key={option.value}
                value={option.value}
                disabled={option.value === "confirmed_booking" && schedulingAuthority === "owner_manual"}
              >
                {option.label}
              </option>
            ))}
          </select>
          <div className="mt-3 rounded-md border border-line bg-slate-50 px-3 py-2 text-xs leading-5 text-muted">
            {bookingModeDescription(form.bookingMode, schedulingAuthority)}
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
        <CheckboxRow
          label="AI service consultation enabled"
          checked={form.consultationEnabled}
          disabled={busy}
          onChange={(checked) => onChange({ ...form, consultationEnabled: checked })}
        />
      </div>

      <div className="mt-5 rounded-md border border-line bg-slate-50 p-4 text-sm">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <div className="font-semibold text-ink">Consultation profile coverage</div>
            <div className="mt-1 text-xs leading-5 text-muted">
              {consultationReadyCount} of {consultationEligibleCount} AI-bookable services have an owner-approved ready profile.
            </div>
          </div>
          <Link className="text-sm font-semibold text-brand hover:underline" href="/dashboard/services">
            Manage service profiles
          </Link>
        </div>
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
  lastSyncedAt
}: {
  periods: BusinessHourPeriod[];
  sourceLabel: string;
  hasSyncedSquarePeriods: boolean;
  lastSyncedAt: string;
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
            Provider sync is managed in Platform Technical settings. Last sync: {lastSyncedAt ? formatDateTime(lastSyncedAt) : "not synced"}.
          </div>
        </div>
        <Badge value={hasSyncedSquarePeriods ? "ready" : "blocked"} className="self-start" />
      </div>

      {!hasSyncedSquarePeriods ? (
        <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
          No Square business hour periods are synced yet. Ask an authorized Platform administrator to review the provider setup and sync in Technical settings.
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
    bookingMode: "pending_approval",
    recordingEnabled: true,
    recordingConsentMessage: "",
    smsConfirmationEnabled: true,
    smsReminderEnabled: true,
    reminderHoursBefore: "24",
    handoffEnabled: true,
    consultationEnabled: false
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
    bookingMode: normalizeBookingMode(settings.booking_mode),
    recordingEnabled: settings.recording_enabled,
    recordingConsentMessage: settings.recording_consent_message || "",
    smsConfirmationEnabled: settings.sms_confirmation_enabled,
    smsReminderEnabled: settings.sms_reminder_enabled,
    reminderHoursBefore: String(settings.reminder_hours_before || 24),
    handoffEnabled: settings.handoff_enabled,
    consultationEnabled: settings.consultation_enabled
  };
}

function tonePreview(value: string) {
  return aiToneOptions.find((option) => option.value === value)?.preview ?? aiToneOptions[0].preview;
}

function normalizeBookingMode(value: string): BookingMode {
  if (value === "confirmed_booking" || value === "disabled") return value;
  return "pending_approval";
}

function bookingModeDescription(mode: BookingMode, authority: SchedulingAuthority) {
  if (mode === "disabled") {
    return "The AI receptionist will not check availability, create a request, or change an appointment; it will hand the caller to the owner workflow.";
  }
  if (mode === "pending_approval") {
    if (authority === "owner_manual") {
      return "The AI receptionist records the requested time for owner review. It does not reserve or confirm that time.";
    }
    return `The AI receptionist checks ${publicAuthorityLabel(authority)} for openings, then records the selected time for owner review. The time is not reserved or confirmed.`;
  }
  return `The AI receptionist confirms only after ${publicAuthorityLabel(authority)} completes the booking and returns durable appointment evidence.`;
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

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}
