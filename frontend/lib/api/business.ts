import { apiRequest, apiRequestWithResponse } from "./client";

export type BusinessSurface = {
  kind: "tenant" | "platform";
  salonID: string;
};

export type DataClassification = "live" | "sample_test";

export type BusinessSalonSummary = {
  id: string;
  name: string;
  city?: string;
  state?: string;
  timezone: string;
  data_classification: DataClassification;
  public_slug?: string;
  public_catalog_enabled: boolean;
  ai_enabled: boolean;
  business_access: "tenant_owner" | "tenant_business_manager" | "global" | "assigned" | string;
  scheduling_authority: "owner_manual" | "manleai_calendar" | "external_provider";
  scheduling_authority_version: number;
  active_pos_provider: string;
};

export type BusinessSalonProfile = {
  id: string;
  name: string;
  phone: string;
  address?: string;
  city?: string;
  state?: string;
  zip_code?: string;
  timezone: string;
  data_classification: DataClassification;
  primary_language: string;
  secondary_language: string;
  handoff_phone?: string;
  public_slug?: string;
  public_catalog_enabled: boolean;
  version: number;
  updated_at: string;
};

export type BusinessServiceCategory = {
  id: string;
  name: string;
  slug: string;
  description?: string;
  sort_order: number;
  status: string;
  version: number;
  archived_at?: string;
};

export type BusinessConsultationProfile = {
  status: "draft" | "ready" | "disabled";
  recommended_outcomes: string[];
  compatible_current_systems: string[];
  length_capabilities: string[];
  priority_tags: string[];
  finish_options: string[];
  maintenance_note?: string;
  owner_approved_summary?: string;
};

export type BusinessService = {
  id: string;
  name: string;
  description?: string;
  ai_description?: string;
  duration_minutes: number;
  price_from?: number;
  price_display?: string;
  ai_bookable: boolean;
  active: boolean;
  category?: BusinessServiceCategory;
  consultation_profile?: BusinessConsultationProfile;
  management_mode: "local" | "provider_read_only";
  version: number;
  archived_at?: string;
};

export type BusinessStaff = {
  id: string;
  name: string;
  phone?: string;
  email?: string;
  ai_bookable: boolean;
  active: boolean;
  management_mode: "local" | "provider_read_only";
  service_ids: string[];
  version: number;
  eligibility_version: number;
  archived_at?: string;
};

export type BusinessHourPeriod = {
  id?: string;
  day_of_week: number;
  start_local_time: string;
  end_local_time: string;
  end_at_midnight: boolean;
};

export type BusinessHours = {
  periods: BusinessHourPeriod[];
  management_mode: "local" | "provider_read_only";
  version: number;
};

export type BusinessPublicCatalog = {
  public_slug?: string;
  public_catalog_enabled: boolean;
  public_path?: string;
  can_publish: boolean;
  blocked_reason?: string;
  version: number;
};

export type BusinessCustomer = {
  id: string;
  name: string;
  phone?: string;
  email?: string;
  notes?: string;
  active: boolean;
  management_mode: "local" | "provider_read_only";
  version: number;
  archived_at?: string;
};

export type BusinessMutationResponse<T> = { data: T; replayed: boolean };
type NormalizedBusinessResponse<T> = { data: T; meta: { replayed: boolean; resource_version: number } };
export type MutationControl = { action_key: string; expected_version: number };

export function businessDirectoryPath(kind: BusinessSurface["kind"]) {
  return kind === "platform" ? "/api/v2/platform/tenants" : "/api/salons/";
}

export function isSampleData(value: { data_classification: DataClassification }) {
  return value.data_classification === "sample_test";
}

export function businessBasePath(surface: BusinessSurface) {
  return surface.kind === "platform"
    ? `/api/v2/platform/tenants/${encodeURIComponent(surface.salonID)}`
    : `/api/salons/${surface.salonID}/business`;
}

export async function listBusinessSalons(kind: BusinessSurface["kind"]) {
  if (kind === "tenant") return apiRequest<{ salons: BusinessSalonSummary[] }>(businessDirectoryPath(kind));
  const response = await apiRequest<NormalizedBusinessResponse<{ salons: BusinessSalonSummary[] }>>(businessDirectoryPath(kind));
  return response.data;
}

export function platformBusinessResourcePath(salonID: string, resource: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(salonID)}/${normalizedPlatformResource(resource)}`;
}

export async function businessGet<T>(surface: BusinessSurface, resource: string) {
  if (surface.kind === "tenant") return apiRequest<T>(`${businessBasePath(surface)}/${resource}`);
  const response = await apiRequest<NormalizedBusinessResponse<unknown>>(platformBusinessResourcePath(surface.salonID, resource));
  const resourceName = resource.split("?", 1)[0];
  if (resourceName === "services") return { services: response.data } as T;
  if (resourceName === "service-categories") return { categories: response.data } as T;
  if (resourceName === "staff") return { staff: response.data } as T;
  if (resourceName === "customers") return { customers: response.data } as T;
  return response.data as T;
}

export async function businessMutation<T>(
  surface: BusinessSurface,
  resource: string,
  method: "POST" | "PATCH" | "PUT",
  body: Record<string, unknown>
) {
  if (surface.kind === "platform") {
    const { data, response } = await apiRequestWithResponse<NormalizedBusinessResponse<T>>(
      platformBusinessResourcePath(surface.salonID, resource),
      { method, body: JSON.stringify(body) }
    );
    return {
      data: data.data,
      replayed: data.meta.replayed || response.headers.get("X-Idempotent-Replay") === "true"
    };
  }
  const { data, response } = await apiRequestWithResponse<BusinessMutationResponse<T>>(
    `${businessBasePath(surface)}/${resource}`,
    { method, body: JSON.stringify(body) }
  );
  return {
    ...data,
    replayed: data.replayed || response.headers.get("X-Idempotent-Replay") === "true"
  };
}

function normalizedPlatformResource(resource: string) {
  const queryIndex = resource.indexOf("?");
  const path = queryIndex >= 0 ? resource.slice(0, queryIndex) : resource;
  const query = queryIndex >= 0 ? resource.slice(queryIndex) : "";
  let normalized = path;
  if (path === "profile") normalized = "business/profile";
  else if (path === "business-hours") normalized = "business/hours";
  else if (path === "public-catalog") normalized = "business/public-page";
  else if (/^staff\/[^/]+\/services$/.test(path)) normalized = path.replace(/\/services$/, "/service-eligibility");
  return normalized + query;
}

export function newBusinessActionKey(prefix: string) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  return `${prefix}-${random}`;
}

export class BusinessMutationKeyManager {
  private current: { signature: string; key: string } | null = null;

  forPayload(prefix: string, payload: unknown) {
    const signature = JSON.stringify(payload);
    if (!this.current || this.current.signature !== signature) {
      this.current = { signature, key: newBusinessActionKey(prefix) };
    }
    return this.current.key;
  }

  clear() {
    this.current = null;
  }
}
