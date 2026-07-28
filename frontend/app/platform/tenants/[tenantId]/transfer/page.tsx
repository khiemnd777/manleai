import { PlatformConfigurationTransfer } from "@/features/platform/platform-configuration-transfer";

export default function TransferPage({ params }: { params: { tenantId: string } }) {
  return <PlatformConfigurationTransfer tenantID={params.tenantId} />;
}
