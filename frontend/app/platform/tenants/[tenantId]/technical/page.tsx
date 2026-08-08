import { redirect } from "next/navigation";

export default function TechnicalPage({ params }: { params: { tenantId: string } }) {
  redirect(`/platform/tenants/${params.tenantId}/integrations`);
}
