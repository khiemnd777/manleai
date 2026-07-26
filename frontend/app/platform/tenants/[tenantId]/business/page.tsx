import { PlatformBusinessWorkspace } from "@/features/platform/platform-business-workspace";

export default function PlatformTenantBusinessPage({params}:{params:{tenantId:string}}){return <PlatformBusinessWorkspace tenantID={params.tenantId}/>}
