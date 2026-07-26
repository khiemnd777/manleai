import { apiRequest, apiRequestWithResponse } from "./client";

export type BusinessSurface = {
  kind: "tenant" | "platform";
  salonID: string;
};

export type BusinessSalonSummary = {
  id: string;
  name: string;
  city?: string;
  state?: string;
  timezone: string;
  public_slug?: string;
  public_catalog_enabled: boolean;
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
export type MutationControl = { action_key: string; expected_version: number };

export function businessDirectoryPath(kind: BusinessSurface["kind"]) {
  return kind === "platform" ? "/api/platform/tenants/" : "/api/salons/";
}

export function businessBasePath(surface: BusinessSurface) {
  return surface.kind === "platform"
    ? `/api/platform/tenants/${surface.salonID}/business`
    : `/api/salons/${surface.salonID}/business`;
}

export function listBusinessSalons(kind: BusinessSurface["kind"]) {
  return apiRequest<{ salons: BusinessSalonSummary[] }>(businessDirectoryPath(kind));
}

export function businessGet<T>(surface: BusinessSurface, resource: string) {
  return apiRequest<T>(`${businessBasePath(surface)}/${resource}`);
}

export async function businessMutation<T>(
  surface: BusinessSurface,
  resource: string,
  method: "POST" | "PATCH" | "PUT",
  body: Record<string, unknown>
) {
  const { data, response } = await apiRequestWithResponse<BusinessMutationResponse<T>>(
    `${businessBasePath(surface)}/${resource}`,
    { method, body: JSON.stringify(body) }
  );
  return {
    ...data,
    replayed: data.replayed || response.headers.get("X-Idempotent-Replay") === "true"
  };
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
