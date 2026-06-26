import { apiRequest } from "@/lib/api/client";
import type { ConfigurationBundle, ConfigurationImportResponse, Salon } from "@/types/api";

export async function downloadConfigurationExport(salon: Salon) {
  const data = await apiRequest<ConfigurationBundle>(`/api/salons/${salon.id}/configuration-export`);
  const filename = configurationExportFilename(data, salon.name);
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.URL.revokeObjectURL(url);
}

export async function readConfigurationBundle(file: File) {
  const raw = await file.text();
  return JSON.parse(raw) as ConfigurationBundle;
}

export async function previewConfigurationImport(salon: Salon, configuration: ConfigurationBundle) {
  return apiRequest<ConfigurationImportResponse>(`/api/salons/${salon.id}/configuration-import/preview`, {
    method: "POST",
    body: JSON.stringify({ configuration })
  });
}

export async function applyConfigurationImport(salon: Salon, configuration: ConfigurationBundle, requestId: string) {
  return apiRequest<ConfigurationImportResponse>(`/api/salons/${salon.id}/configuration-import`, {
    method: "POST",
    body: JSON.stringify({ request_id: requestId, configuration })
  });
}

export async function previewOnboardingConfigurationImport(configuration: ConfigurationBundle) {
  return apiRequest<ConfigurationImportResponse>("/api/onboarding/configuration-import/preview", {
    method: "POST",
    body: JSON.stringify({ configuration })
  });
}

export async function applyOnboardingConfigurationImport(configuration: ConfigurationBundle, requestId: string) {
  return apiRequest<ConfigurationImportResponse>("/api/onboarding/configuration-import", {
    method: "POST",
    body: JSON.stringify({ request_id: requestId, configuration })
  });
}

export function newConfigurationImportRequestID() {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `import-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function configurationExportFilename(data: ConfigurationBundle, fallbackName: string) {
  const salonName = data.salon_profile?.name || fallbackName || "salon";
  const baseName = slugify(salonName) || "salon";
  const exportedDate = data.exported_at ? data.exported_at.slice(0, 10) : new Date().toISOString().slice(0, 10);
  return `${baseName}-configuration-${exportedDate}.json`;
}

function slugify(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
