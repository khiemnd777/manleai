export function platformTenantContextPath(tenantID: string) {
  return `/api/v2/platform/tenants/${encodeURIComponent(tenantID)}/context`;
}
