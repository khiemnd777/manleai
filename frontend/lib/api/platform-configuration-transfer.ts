import { apiRequest } from "./client";
import type {
  ConfigurationBundle,
  ConfigurationImportIssue,
  ConfigurationImportSectionSummary,
  SchedulingAuthority
} from "../../types/api";

export type PlatformTransferSourceType = "tenant" | "json_upload";

export const platformTransferSectionOrder = [
  "salon_profile",
  "ai_receptionist",
  "public_booking_page",
  "local_business_hours",
  "service_categories",
  "service_aliases",
  "service_consultation_profiles",
  "knowledge_base",
  "integrations"
] as const;

const platformV8SectionOrder = platformTransferSectionOrder.filter((section) => section !== "local_business_hours");
const platformLegacyV7ContentSections = [
  "service_categories",
  "service_aliases",
  "service_consultation_profiles",
  "knowledge_base"
] as const;

export type PlatformConfigurationFile = {
  configuration: ConfigurationBundle;
  included_sections: string[];
  legacy_v7_adapted: boolean;
};

export type PlatformTransferRequest = {
  source_type: PlatformTransferSourceType;
  source_tenant_id?: string;
  included_sections: string[];
  configuration?: ConfigurationBundle;
};

export type PlatformTransferResponse = {
  run_id: string;
  target_tenant_id: string;
  source_type: PlatformTransferSourceType;
  source_tenant_id?: string;
  schema_version: string;
  included_sections: string[];
  status: "previewed" | "applied" | string;
  can_apply: boolean;
  replayed?: boolean;
  target_scheduling_authority: SchedulingAuthority;
  target_scheduling_authority_version: number;
  source_active_pos_provider?: string;
  target_active_pos_provider?: string;
  summary: ConfigurationImportSectionSummary[];
  warnings: ConfigurationImportIssue[];
  conflicts: ConfigurationImportIssue[];
  excluded_data: string[];
  requires_secret_reentry: string[];
  created_at: string;
  applied_at?: string;
};

export function platformTransferBase(tenantID: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(tenantID)}/configuration-transfers`;
}

export function previewPlatformTransfer(tenantID: string, request: PlatformTransferRequest) {
  return platformTransferRequest<PlatformTransferResponse>(`${platformTransferBase(tenantID)}/previews`, {
    method: "POST",
    body: JSON.stringify(request)
  });
}

export function applyPlatformTransfer(
  tenantID: string,
  request: PlatformTransferRequest,
  previewID: string,
  actionKey: string
) {
  return platformTransferRequest<PlatformTransferResponse>(`${platformTransferBase(tenantID)}/applications`, {
    method: "POST",
    body: JSON.stringify({ ...request, preview_id: previewID, action_key: actionKey })
  });
}

export function listPlatformTransferRuns(tenantID: string, limit = 25) {
  return platformTransferRequest<{ runs: PlatformTransferResponse[] }>(
    `${platformTransferBase(tenantID)}/runs?limit=${limit}`
  );
}

export async function exportPlatformConfiguration(tenantID: string, sections: string[], salonName: string) {
  const path = `${platformTransferBase(tenantID)}/export?sections=${encodeURIComponent(sections.join(","))}`;
  const bundle = await platformTransferRequest<ConfigurationBundle>(path);
  const filename = `${slugify(bundle.salon_profile?.name || salonName || "salon") || "salon"}-platform-configuration-${datePart(bundle.exported_at)}.json`;
  const blob = new Blob([JSON.stringify(bundle)], { type: "application/json" });
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.URL.revokeObjectURL(url);
}

async function platformTransferRequest<T>(path: string, init: RequestInit = {}) {
  return (await apiRequest<{ data: T }>(path, init)).data;
}

export async function readPlatformConfiguration(file: File): Promise<PlatformConfigurationFile> {
  if (file.size > 3 * 1024 * 1024) throw new Error("Configuration JSON must be 3 MB or smaller.");
  const parsed = JSON.parse(await file.text()) as ConfigurationBundle;
  if (!parsed || typeof parsed !== "object" || typeof parsed.schema_version !== "string") {
    throw new Error("The selected file is not a ManleAI configuration bundle.");
  }
  return inspectPlatformConfiguration(parsed);
}

export function inspectPlatformConfiguration(configuration: ConfigurationBundle): PlatformConfigurationFile {
  const schemaVersion = configuration.schema_version.trim();
  let allowedSections: readonly string[];
  let declaredSections = Array.isArray(configuration.included_sections) ? configuration.included_sections : [];
  let legacyV7Adapted = false;

  switch (schemaVersion) {
    case "manleai.salon_configuration.v10":
    case "manleai.salon_configuration.v9":
      allowedSections = platformTransferSectionOrder;
      if (declaredSections.length === 0) {
        throw new Error("Platform v10/v9 configuration files must declare included_sections.");
      }
      break;
    case "manleai.salon_configuration.v8":
      allowedSections = platformV8SectionOrder;
      if (declaredSections.length === 0) declaredSections = [...platformV8SectionOrder];
      break;
    case "manleai.salon_configuration.v7":
      allowedSections = platformLegacyV7ContentSections;
      legacyV7Adapted = true;
      if (declaredSections.length === 0) {
        throw new Error("Legacy v7 support requires an explicit scoped content pack.");
      }
      break;
    default:
      throw new Error("Platform Transfer supports v10, v9, v8, and scoped content-only v7 configuration files.");
  }

  const allowed = new Set(allowedSections);
  const requested = new Set<string>();
  for (const rawSection of declaredSections) {
    const section = typeof rawSection === "string" ? rawSection.trim() : "";
    if (!section || !allowed.has(section) || requested.has(section)) {
      if (legacyV7Adapted) {
        throw new Error("Legacy v7 packs may contain only service categories, service aliases, consultation profiles, and knowledge base sections.");
      }
      throw new Error("The configuration file contains an unsupported or duplicate included section.");
    }
    requested.add(section);
  }

  return {
    configuration,
    included_sections: allowedSections.filter((section) => requested.has(section)),
    legacy_v7_adapted: legacyV7Adapted
  };
}

export function newPlatformTransferActionKey() {
  const suffix = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `platform-transfer-${suffix}`;
}

export function platformTransferRequestSignature(request: PlatformTransferRequest) {
  return JSON.stringify({
    source_type: request.source_type,
    source_tenant_id: request.source_tenant_id || "",
    included_sections: [...request.included_sections],
    configuration: request.configuration ?? null
  });
}

function slugify(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

function datePart(value: string) {
  return value ? value.slice(0, 10) : new Date().toISOString().slice(0, 10);
}
