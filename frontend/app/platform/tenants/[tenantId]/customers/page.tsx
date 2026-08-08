import { BusinessCustomers } from "@/features/business/business-customers";

export default function PlatformTenantCustomersPage({ params }: { params: { tenantId: string } }) {
  return <BusinessCustomers surface={{ kind: "platform", salonID: params.tenantId }} />;
}

