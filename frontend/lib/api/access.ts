import { apiRequest, apiRequestWithResponse } from "@/lib/api/client";
import { newBusinessActionKey } from "@/lib/api/business";

export type AccessUser={id:string;email:string;full_name:string;status:string};
export type PlatformRoleAssignment={id:string;user_id:string;role:"platform_admin"|"platform_ops";status:"active"|"revoked";version:number;created_at:string;updated_at:string};
export type TenantMembership={id:string;salon_id:string;user_id:string;role:"tenant_owner"|"tenant_business_manager";status:"active"|"revoked";is_owner:boolean;version:number;created_at:string;updated_at:string};
export type SalonAssignment={id:string;salon_id:string;user_id:string;status:"active"|"revoked";permissions:string[];version:number;created_at:string;updated_at:string};
export type PIIGrant={id:string;salon_id:string;user_id:string;scope:"customers"|"calls"|"appointments"|"notifications";reason:string;expires_at:string;revoked_at?:string;version:number;created_by_user_id:string;created_at:string;updated_at:string};
export type AccessEvent={id:string;actor_user_id?:string;salon_id?:string;target_user_id?:string;event_type:string;object_type:string;object_id:string;details:Record<string,unknown>;created_at:string};

export function listAccessUsers(query=""){return apiRequest<{users:AccessUser[]}>(`/api/platform/access/users?query=${encodeURIComponent(query)}&limit=50`)}
export function listPlatformRoles(){return apiRequest<{assignments:PlatformRoleAssignment[]}>("/api/platform/access/roles")}
export function listMemberships(salonID:string){return apiRequest<{memberships:TenantMembership[]}>(`/api/platform/access/salons/${salonID}/memberships`)}
export function listSalonAssignments(salonID:string){return apiRequest<{assignments:SalonAssignment[]}>(`/api/platform/access/salons/${salonID}/assignments`)}
export function listPIIGrants(salonID:string){return apiRequest<{grants:PIIGrant[]}>(`/api/platform/access/salons/${salonID}/pii-grants`)}
export function listAccessAudit(salonID:string,limit=50,offset=0){return apiRequest<{events:AccessEvent[];limit:number;offset:number;has_more:boolean}>(`/api/platform/access/audit?salon_id=${encodeURIComponent(salonID)}&limit=${limit}&offset=${offset}`)}

async function mutate<T>(path:string,method:"PUT"|"POST",body:Record<string,unknown>){const{data,response}=await apiRequestWithResponse<T>(path,{method,body:JSON.stringify(body)});return{data,replayed:response.headers.get("X-Idempotent-Replay")==="true"}}
export function mutatePlatformRole(userID:string,role:string,status:string,expectedVersion:number,actionKey=newBusinessActionKey("platform-role")){return mutate<PlatformRoleAssignment>(`/api/platform/access/users/${userID}/platform-role`,"PUT",{action_key:actionKey,role,status,expected_version:expectedVersion})}
export function mutateMembership(salonID:string,userID:string,role:string,status:string,expectedVersion:number,actionKey=newBusinessActionKey("tenant-membership")){return mutate<TenantMembership>(`/api/platform/access/salons/${salonID}/memberships/${userID}`,"PUT",{action_key:actionKey,role,status,expected_version:expectedVersion})}
export function mutateSalonAssignment(salonID:string,userID:string,status:string,permissions:string[],expectedVersion:number,actionKey=newBusinessActionKey("salon-assignment")){return mutate<SalonAssignment>(`/api/platform/access/salons/${salonID}/assignments/${userID}`,"PUT",{action_key:actionKey,status,permissions,expected_version:expectedVersion})}
export function grantPII(salonID:string,userID:string,scope:string,reason:string,expiresAt:string,actionKey=newBusinessActionKey("pii-grant")){return mutate<PIIGrant>(`/api/platform/access/salons/${salonID}/pii-grants`,"POST",{action_key:actionKey,user_id:userID,scope,reason,expires_at:expiresAt})}
export function revokePII(salonID:string,grantID:string,expectedVersion:number,actionKey=newBusinessActionKey("pii-revoke")){return mutate<PIIGrant>(`/api/platform/access/salons/${salonID}/pii-grants/${grantID}/revoke`,"POST",{action_key:actionKey,expected_version:expectedVersion})}
