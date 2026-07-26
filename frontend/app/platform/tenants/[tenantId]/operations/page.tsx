import { PlatformOperationsConsole } from "@/features/platform/platform-operations-console";
export default async function OperationsPage({params}:{params:Promise<{tenantId:string}>}){const{tenantId}=await params;return <PlatformOperationsConsole tenantID={tenantId}/>}
