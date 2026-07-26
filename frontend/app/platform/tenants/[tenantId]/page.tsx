import { redirect } from "next/navigation";

export default function PlatformTenantPage({ params }: { params: { tenantId: string } }) { redirect(`/platform/tenants/${params.tenantId}/business`); }
