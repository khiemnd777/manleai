import { PlatformInternalCalendarSettings } from "@/features/platform/platform-internal-calendar-settings";

export default function PlatformTenantCalendarSetupPage({ params }: { params: { tenantId: string } }) {
  return <PlatformInternalCalendarSettings tenantID={params.tenantId} />;
}
