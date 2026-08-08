import { PlatformIntegrationSettings } from "@/features/platform/platform-integration-settings";

export default function PlatformTenantIntegrationsPage({ params }: { params: { tenantId: string } }) {
  return <PlatformIntegrationSettings tenantID={params.tenantId} />;
}
