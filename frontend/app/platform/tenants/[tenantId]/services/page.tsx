import { ServicesDashboard } from "@/features/dashboard/services-dashboard";

export default function PlatformTenantServicesPage({ params }: { params: { tenantId: string } }) {
  return <ServicesDashboard surface="platform" salonID={params.tenantId} />;
}
