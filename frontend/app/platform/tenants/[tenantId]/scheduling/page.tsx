import { PlatformSchedulingSettings } from "@/features/platform/platform-scheduling-settings";

export default function PlatformTenantSchedulingPage({ params }: { params: { tenantId: string } }) {
  return <PlatformSchedulingSettings tenantID={params.tenantId} />;
}
