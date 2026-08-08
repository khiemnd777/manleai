import { apiRequest } from "@/lib/api/client";
import { platformTenantContextPath } from "@/lib/api/platform-tenant-routes";
import type { BusinessSalonProfile } from "@/lib/api/business";

export type PlatformTenantContext = {
  data: BusinessSalonProfile;
  meta: {
    resource_version: number;
    permissions: {
      can_read: boolean;
      allowed_actions: string[];
      pii_scopes: string[];
    };
  };
};

export function getPlatformTenantContext(tenantID: string) {
  return apiRequest<PlatformTenantContext>(platformTenantContextPath(tenantID));
}
