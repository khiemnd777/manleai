import { TenantAccessConsole } from "@/features/platform/tenant-access-console";

export default function PlatformTenantAccessPage({ params }: { params: { tenantId: string } }) {
  return <TenantAccessConsole tenantID={params.tenantId} />;
}
