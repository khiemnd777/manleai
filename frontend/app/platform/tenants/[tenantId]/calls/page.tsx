import { CallsDashboard } from "@/features/dashboard/calls-dashboard";

export default function PlatformTenantCallsPage({ params }: { params: { tenantId: string } }) {
  return <CallsDashboard surface="platform" salonID={params.tenantId} />;
}
