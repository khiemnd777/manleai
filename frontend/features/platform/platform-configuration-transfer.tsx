"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowRight, Download, FileJson, RefreshCcw, Search, ShieldCheck, Upload } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { ImportIssueList, ImportSummaryTable, listOrNone, sectionLabel } from "@/features/configuration-transfer/import-preview";
import { listBusinessSalons, type BusinessSalonSummary } from "@/lib/api/business";
import { RequestError } from "@/lib/api/client";
import {
  applyPlatformTransfer,
  exportPlatformConfiguration,
  listPlatformTransferRuns,
  newPlatformTransferActionKey,
  previewPlatformTransfer,
  readPlatformConfiguration,
  type PlatformTransferRequest,
  type PlatformTransferResponse,
  type PlatformTransferSourceType
} from "@/lib/api/platform-configuration-transfer";
import type { ConfigurationBundle } from "@/types/api";

const sectionOptions = [
  { key: "salon_profile", label: "Salon profile", description: "Name, contact, location, timezone, and handoff phone." },
  { key: "ai_receptionist", label: "AI receptionist", description: "Greeting, voice, booking policy, consent, reminders, and AI enablement." },
  { key: "public_booking_page", label: "Public booking page", description: "Public slug and publish intent, subject to destination readiness." },
  { key: "local_business_hours", label: "Local business hours", description: "Only local_override periods; provider-imported hours never transfer." },
  { key: "service_categories", label: "Service categories", description: "Category structure and category aliases." },
  { key: "service_aliases", label: "Service aliases", description: "Portable phrases mapped onto matching destination services." },
  { key: "service_consultation_profiles", label: "Consultation profiles", description: "Profiles mapped onto matching destination services." },
  { key: "knowledge_base", label: "Knowledge base", description: "FAQs and policies without calls, customers, or operational history." },
  { key: "integrations", label: "Provider non-secret settings", description: "Only providers persisted for the source are eligible. Credentials stay on the destination." }
] as const;

const safeSections = sectionOptions.filter((item) => item.key !== "integrations").map((item) => item.key);
const knowledgeSections = ["service_categories", "service_aliases", "service_consultation_profiles", "knowledge_base"];

export function PlatformConfigurationTransfer({ tenantID }: { tenantID: string }) {
  const [salons, setSalons] = useState<BusinessSalonSummary[]>([]);
  const [sourceType, setSourceType] = useState<PlatformTransferSourceType>("tenant");
  const [sourceTenantID, setSourceTenantID] = useState("");
  const [sourceQuery, setSourceQuery] = useState("");
  const [configuration, setConfiguration] = useState<ConfigurationBundle | null>(null);
  const [fileName, setFileName] = useState("");
  const [fileSections, setFileSections] = useState<string[]>([]);
  const [legacyV7Adapted, setLegacyV7Adapted] = useState(false);
  const [sections, setSections] = useState<string[]>(safeSections);
  const [preview, setPreview] = useState<PlatformTransferResponse | null>(null);
  const [runs, setRuns] = useState<PlatformTransferResponse[]>([]);
  const [runsBlocked, setRunsBlocked] = useState(false);
  const [actionKey, setActionKey] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const target = salons.find((salon) => salon.id === tenantID);
  const sourceCandidates = useMemo(() => {
    const needle = sourceQuery.trim().toLowerCase();
    return salons.filter((salon) => salon.id !== tenantID && (!needle || [salon.name, salon.city, salon.state].some((value) => value?.toLowerCase().includes(needle))));
  }, [salons, sourceQuery, tenantID]);

  const request = useMemo<PlatformTransferRequest>(() => ({
    source_type: sourceType,
    source_tenant_id: sourceType === "tenant" ? sourceTenantID : undefined,
    included_sections: sections,
    configuration: sourceType === "json_upload" ? configuration ?? undefined : undefined
  }), [configuration, sections, sourceTenantID, sourceType]);
  const availableSections = sourceType === "json_upload" && configuration ? fileSections : sectionOptions.map((item) => item.key);

  async function load() {
    setLoading(true);
    setError("");
    try {
      const [directory, runResult] = await Promise.allSettled([
        listBusinessSalons("platform"),
        listPlatformTransferRuns(tenantID)
      ]);
      if (directory.status === "fulfilled") setSalons(directory.value.salons);
      else throw directory.reason;
      if (runResult.status === "fulfilled") {
        setRuns(runResult.value.runs);
        setRunsBlocked(false);
      } else if (runResult.reason instanceof RequestError && runResult.reason.status === 403) {
        setRunsBlocked(true);
      } else {
        setError(errorMessage(runResult.reason, "Could not load recent transfer runs."));
      }
    } catch (failure) {
      setError(errorMessage(failure, "Could not load Platform Transfer."));
    } finally { setLoading(false); }
  }

  useEffect(() => { void load(); }, [tenantID]);

  function invalidatePreview() {
    setPreview(null);
    setActionKey("");
    setSuccess("");
    setError("");
  }

  function changeSourceType(next: PlatformTransferSourceType) {
    setSourceType(next);
    setSections(next === "json_upload" && configuration ? fileSections : safeSections);
    invalidatePreview();
  }

  function choosePreset(next: "safe" | "knowledge" | "all") {
    const preset = next === "safe" ? safeSections : next === "knowledge" ? knowledgeSections : sectionOptions.map((item) => item.key);
    setSections(preset.filter((section) => availableSections.includes(section)));
    invalidatePreview();
  }

  function toggleSection(key: string) {
    if (!availableSections.includes(key)) return;
    setSections((current) => current.includes(key) ? current.filter((item) => item !== key) : sectionOptions.filter((item) => current.includes(item.key) || item.key === key).map((item) => item.key));
    invalidatePreview();
  }

  async function chooseJSON(file: File | null) {
    if (!file) return;
    invalidatePreview();
    setBusy("file");
    try {
      const result = await readPlatformConfiguration(file);
      setConfiguration(result.configuration);
      setFileSections(result.included_sections);
      setLegacyV7Adapted(result.legacy_v7_adapted);
      setSections(result.included_sections);
      setFileName(file.name);
    } catch (failure) {
      setConfiguration(null);
      setFileName("");
      setFileSections([]);
      setLegacyV7Adapted(false);
      setError(errorMessage(failure, "Could not read configuration JSON."));
    } finally {
      setBusy("");
    }
  }

  async function runPreview() {
    setBusy("preview"); setError(""); setSuccess("");
    try {
      const result = await previewPlatformTransfer(tenantID, request);
      setPreview(result);
      setActionKey(newPlatformTransferActionKey());
    } catch (failure) {
      setError(errorMessage(failure, "Could not preview this transfer."));
    } finally { setBusy(""); }
  }

  async function apply() {
    if (!preview || !actionKey) return;
    setBusy("apply"); setError(""); setSuccess("");
    try {
      const result = await applyPlatformTransfer(tenantID, request, preview.run_id, actionKey);
      setPreview(result);
      setSuccess(result.replayed ? "The previously applied result was returned safely." : "Configuration transfer applied atomically. Scheduling authority, provider connection state, and secrets were preserved.");
      try {
        const recent = await listPlatformTransferRuns(tenantID);
        setRuns(recent.runs);
        setRunsBlocked(false);
      } catch (historyFailure) {
        if (historyFailure instanceof RequestError && historyFailure.status === 403) setRunsBlocked(true);
      }
    } catch (failure) {
      setError(errorMessage(failure, "Could not apply this transfer."));
      if (failure instanceof RequestError && failure.code === "CONFIGURATION_TRANSFER_STALE") {
        setPreview((current) => current ? { ...current, can_apply: false } : current);
        setActionKey("");
      }
    } finally { setBusy(""); }
  }

  async function exportJSON() {
    if (!target || sections.length === 0) return;
    setBusy("export"); setError("");
    try {
      await exportPlatformConfiguration(tenantID, sections, target.name);
    } catch (failure) {
      setError(errorMessage(failure, "Could not export configuration JSON."));
    } finally { setBusy(""); }
  }

  const sourceReady = sourceType === "tenant" ? Boolean(sourceTenantID) : Boolean(configuration);
  const canPreview = sourceReady && sections.length > 0 && !busy;

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div><h2 className="text-xl font-bold text-ink">Platform Transfer</h2><p className="mt-1 max-w-3xl text-sm leading-6 text-muted">Copy reviewed, portable configuration into this salon. The destination is fixed by this tenant page; every selected source read and destination write is authorized separately.</p></div>
        <Button type="button" variant="secondary" disabled={Boolean(busy) || sections.length === 0 || !target} onClick={() => void exportJSON()}><Download className="h-4 w-4" />{busy === "export" ? "Exporting…" : "Export selected JSON"}</Button>
      </div>

      {error ? <Alert title="Platform Transfer needs attention" message={error} /> : null}
      {success ? <Alert type="success" title="Transfer complete" message={success} /> : null}
      {loading ? <Card className="text-sm text-muted">Loading authorized salons and recent transfer evidence…</Card> : null}

      <Card>
        <div className="flex items-start gap-3"><div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand"><FileJson className="h-5 w-5" /></div><div><CardTitle>1. Choose a source</CardTitle><CardDescription>Use another authorized tenant, a v10 or v9 export, a compatibility v8 JSON file, or a scoped content-only v7 pack.</CardDescription></div></div>
        <div className="mt-5 grid gap-3 sm:grid-cols-2">
          <SourceChoice active={sourceType === "tenant"} disabled={Boolean(busy)} title="Another tenant" description="Reads current source data and rechecks its fingerprint at apply." onClick={() => changeSourceType("tenant")} />
          <SourceChoice active={sourceType === "json_upload"} disabled={Boolean(busy)} title="JSON upload" description="Supports v10, v9, v8, and safely adapted scoped v7 content packs." onClick={() => changeSourceType("json_upload")} />
        </div>
        {sourceType === "tenant" ? <div className="mt-5 grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)]"><label><span className="text-sm font-semibold text-ink">Find source salon</span><div className="mt-2 flex h-10 items-center gap-2 rounded-md border border-line px-3"><Search className="h-4 w-4 text-muted" /><input className="w-full bg-transparent text-sm outline-none disabled:opacity-50" disabled={Boolean(busy)} value={sourceQuery} onChange={(event) => setSourceQuery(event.target.value)} placeholder="Name, city, or state" /></div></label><label><span className="text-sm font-semibold text-ink">Source tenant</span><select className="field mt-2 h-10" disabled={Boolean(busy)} value={sourceTenantID} onChange={(event) => { setSourceTenantID(event.target.value); invalidatePreview(); }}><option value="">Select an authorized salon</option>{sourceCandidates.map((salon) => <option key={salon.id} value={salon.id}>{salon.name} · {[salon.city, salon.state].filter(Boolean).join(", ") || salon.timezone}</option>)}</select></label></div> : <div className="mt-5"><label className={`inline-flex h-10 cursor-pointer items-center justify-center gap-2 rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50 ${busy ? "pointer-events-none opacity-50" : ""}`}><Upload className="h-4 w-4" />{busy === "file" ? "Reading…" : "Choose JSON"}<input type="file" accept="application/json,.json" className="hidden" disabled={Boolean(busy)} onChange={(event) => { const file=event.target.files?.[0] ?? null; event.target.value=""; void chooseJSON(file); }} /></label>{fileName ? <div className="mt-2 break-all text-xs text-muted">Selected: {fileName} · source schema {configuration?.schema_version}{legacyV7Adapted ? " · reviewed as canonical v8" : ""}</div> : null}{legacyV7Adapted ? <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900"><strong>Legacy v7 content pack detected.</strong> Platform will canonicalize it to v8 before fingerprinting and review. Runtime, provider, scheduling, and operational sections are not accepted.</div> : null}</div>}
      </Card>

      <Card>
        <CardTitle>2. Select portable sections</CardTitle>
        <CardDescription>Safe setup excludes provider settings. Add them only when their non-secret values should follow the source.</CardDescription>
        <div className="mt-4 flex flex-wrap gap-2"><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => choosePreset("safe")}>Safe setup</Button><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => choosePreset("knowledge")}>Training pack</Button><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => choosePreset("all")}>All portable</Button></div>
        <div className="mt-4 grid gap-3 md:grid-cols-2">{sectionOptions.map((item) => { const unavailable = !availableSections.includes(item.key); return <label key={item.key} className={`flex gap-3 rounded-md border p-3 ${busy || unavailable ? "cursor-not-allowed opacity-60" : "cursor-pointer"} ${sections.includes(item.key) ? "border-teal-300 bg-teal-50/50" : "border-line"}`}><input type="checkbox" className="mt-1 h-4 w-4 accent-teal-700" disabled={Boolean(busy) || unavailable} checked={sections.includes(item.key)} onChange={() => toggleSection(item.key)} /><span><span className="block text-sm font-semibold text-ink">{item.label}</span><span className="mt-1 block text-xs leading-5 text-muted">{item.description}{unavailable && sourceType === "json_upload" && configuration ? " Not included in the selected file." : ""}</span></span></label>; })}</div>
        <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm leading-6 text-amber-900"><strong>Always excluded:</strong> scheduling authority and switch history, active-provider changes, provider connections and tokens, secrets, services, staff, customers, appointments, scheduling requests, calls, recordings, and operational history.</div>
      </Card>

      <Card>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between"><div><CardTitle>3. Preview and apply</CardTitle><CardDescription>Preview writes no salon configuration; it records safe review evidence. Apply succeeds only against the exact source fingerprint, destination versions, and scheduling-authority version reviewed here.</CardDescription></div><Button type="button" disabled={!canPreview} onClick={() => void runPreview()}><ShieldCheck className="h-4 w-4" />{busy === "preview" ? "Previewing…" : "Preview changes"}</Button></div>
        {!preview ? <div className="mt-5 rounded-md border border-dashed border-line p-6 text-sm text-muted">Choose a source and at least one section, then preview. No destination configuration is changed during preview.</div> : <div className="mt-5 space-y-4"><div className="flex flex-col gap-3 rounded-md border border-line bg-slate-50 p-4 sm:flex-row sm:items-start sm:justify-between"><div><div className="text-sm font-semibold text-ink">Destination authority remains {authorityLabel(preview.target_scheduling_authority)}</div><div className="mt-1 text-xs leading-5 text-muted">Authority version {preview.target_scheduling_authority_version} · source adapter {preview.source_active_pos_provider || "not reported"} · destination adapter {preview.target_active_pos_provider || "not configured"}</div></div><Badge value={preview.can_apply ? preview.status : "conflict"} /></div><ImportSummaryTable summary={preview.summary} /><ImportIssueList title="Conflicts" issues={preview.conflicts} tone="danger" /><ImportIssueList title="Warnings" issues={preview.warnings} tone="warning" /><details className="rounded-md border border-line p-3 text-xs leading-5 text-muted"><summary className="cursor-pointer font-semibold text-ink">Excluded data ({preview.excluded_data.length})</summary><div className="mt-2 break-words">{preview.excluded_data.join(", ")}</div></details><div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div className="text-xs leading-5 text-muted">Schema {preview.schema_version}. Re-enter secrets for: {listOrNone(preview.requires_secret_reentry)}.</div><Button type="button" disabled={Boolean(busy) || !preview.can_apply || preview.status === "applied"} onClick={() => void apply()}><ArrowRight className="h-4 w-4" />{busy === "apply" ? "Applying atomically…" : preview.status === "applied" ? "Applied" : "Apply reviewed transfer"}</Button></div></div>}
      </Card>

      <Card>
        <div className="flex items-start justify-between gap-3"><div><CardTitle>Recent transfer runs</CardTitle><CardDescription>Safe run metadata only. Full configuration payloads and secret values are not stored.</CardDescription></div><Button type="button" variant="secondary" onClick={() => void load()} disabled={Boolean(busy)}><RefreshCcw className="h-4 w-4" />Refresh</Button></div>
        {runsBlocked ? <div className="mt-4"><Alert type="warning" title="Run history requires audit access" message="This Platform account can operate authorized sections, but audit.read is required to list recent transfer runs." /></div> : runs.length ? <div className="mt-4 space-y-3">{runs.map((run) => <div key={run.run_id} className="rounded-md border border-line p-4"><div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between"><div><div className="text-sm font-semibold text-ink">{run.source_type === "tenant" ? "Tenant-to-tenant" : "JSON upload"}</div><div className="mt-1 text-xs text-muted">{new Date(run.created_at).toLocaleString()} · {run.included_sections.map(sectionLabel).join(", ")}</div></div><Badge value={run.status} /></div></div>)}</div> : <div className="mt-4 rounded-md border border-dashed border-line p-5 text-sm text-muted">No transfer run has been recorded for this salon.</div>}
      </Card>
    </div>
  );
}

function SourceChoice({ active, disabled, title, description, onClick }: { active: boolean; disabled: boolean; title: string; description: string; onClick: () => void }) {
  return <button type="button" disabled={disabled} onClick={onClick} className={`rounded-md border p-4 text-left disabled:cursor-not-allowed disabled:opacity-60 ${active ? "border-teal-400 bg-teal-50" : "border-line hover:bg-slate-50"}`}><span className="block text-sm font-semibold text-ink">{title}</span><span className="mt-1 block text-xs leading-5 text-muted">{description}</span></button>;
}

function authorityLabel(value: string) {
  if (value === "owner_manual") return "Owner confirmation";
  if (value === "manleai_calendar") return "ManleAI Calendar";
  if (value === "external_provider") return "external provider";
  return value;
}

function errorMessage(failure: unknown, fallback: string) {
  return failure instanceof Error ? failure.message : fallback;
}
