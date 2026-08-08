import { PlatformConfigurationTransferHistory } from "@/features/platform/platform-configuration-transfer-history";

export default function PlatformConfigurationTransfersPage({ params }: { params: { tenantId: string } }) {
  return <PlatformConfigurationTransferHistory tenantID={params.tenantId} />;
}
