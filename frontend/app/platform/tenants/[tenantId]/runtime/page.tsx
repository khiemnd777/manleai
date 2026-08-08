import { PlatformAIRuntimeControl } from "@/features/platform/ai-runtime-control";

export default function PlatformTenantRuntimePage({ params }: { params: { tenantId: string } }) {
  return <PlatformAIRuntimeControl tenantID={params.tenantId} />;
}
