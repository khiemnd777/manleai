import { TechnicalIntegrationSettings } from "@/features/platform/technical-integration-settings";
import { TechnicalSchedulingSettings } from "@/features/platform/technical-scheduling-settings";

export default function TechnicalPage({ params }: { params: { tenantId: string } }) {
  return (
    <div className="space-y-8">
      <TechnicalIntegrationSettings tenantID={params.tenantId} />
      <TechnicalSchedulingSettings tenantID={params.tenantId} />
    </div>
  );
}
