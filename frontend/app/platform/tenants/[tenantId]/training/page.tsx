import { TrainingDashboard } from "@/features/dashboard/training-dashboard";

export default function PlatformTenantTrainingPage({ params }: { params: { tenantId: string } }) {
  return <TrainingDashboard surface="platform" salonID={params.tenantId} />;
}
