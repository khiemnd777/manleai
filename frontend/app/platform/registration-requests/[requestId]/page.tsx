import { TenantRegistrationDetail } from "@/features/platform/tenant-registration-detail";
export default async function RegistrationRequestDetailPage({params}:{params:Promise<{requestId:string}>}){const{requestId}=await params;return <TenantRegistrationDetail requestId={requestId}/>}
