import { PlatformTenantDetailShell } from "@/features/platform/tenant-detail-shell";

export default function PlatformTenantLayout({ children, params }: { children: React.ReactNode; params: { tenantId: string } }) {
  return <PlatformTenantDetailShell tenantID={params.tenantId}>{children}</PlatformTenantDetailShell>;
}
