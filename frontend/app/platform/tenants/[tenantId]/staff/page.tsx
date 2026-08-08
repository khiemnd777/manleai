import { BusinessStaffManager } from "@/features/business/business-staff";

export default function PlatformTenantStaffPage({ params }: { params: { tenantId: string } }) {
  return <BusinessStaffManager surface={{ kind: "platform", salonID: params.tenantId }} title="Staff & service assignments" />;
}

