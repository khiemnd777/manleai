import { PlatformAuditLog } from "@/features/platform/platform-audit-log";
export default async function AuditPage({params}:{params:Promise<{tenantId:string}>}){const{tenantId}=await params;return <PlatformAuditLog tenantID={tenantId}/>}
