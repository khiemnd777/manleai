import { apiBaseUrl } from "@/lib/config/env";
import { apiRequest } from "./client";
import { registrationInvitationPath,registrationListPath,registrationNotePath,registrationProvisionPath,registrationRequestPath,tenantIdentitySearchPath } from "./tenant-registration-routes";
import type { InvitationResult,ProvisionInput,ProvisioningDraft,ProvisionResult,RegistrationDetail,RegistrationListResponse,RegistrationMutationResult,RegistrationStatus,TenantIdentity } from "./tenant-registration-types";
export type { InvitationResult,ProvisionInput,ProvisioningDraft,ProvisionResult,RegistrationDetail,RegistrationEvent,RegistrationListItem,RegistrationListResponse,RegistrationMutationResult,RegistrationNote,RegistrationStatus,TenantIdentity } from "./tenant-registration-types";

export function listRegistrationRequests(filters:Record<string,string|number|undefined>,signal?:AbortSignal){return apiRequest<RegistrationListResponse>(registrationListPath(filters),{signal})}
export function getRegistrationRequest(id:string,signal?:AbortSignal){return apiRequest<RegistrationDetail>(registrationRequestPath(id),{signal})}
export function mutateRegistrationRequest(id:string,input:{action_key:string;expected_version:number;status?:RegistrationStatus;assigned_to_user_id?:string;provisioning_draft?:ProvisioningDraft}){return apiRequest<RegistrationMutationResult>(registrationRequestPath(id),{method:"PATCH",body:JSON.stringify(input)})}
export function addRegistrationNote(id:string,input:{action_key:string;expected_version:number;content:string}){return apiRequest<{request_id:string;note_id:string;version:number;replayed:boolean}>(registrationNotePath(id),{method:"POST",body:JSON.stringify(input)})}
export function provisionRegistration(id:string,input:ProvisionInput){return apiRequest<ProvisionResult>(registrationProvisionPath(id),{method:"POST",body:JSON.stringify(input)})}
export function createOwnerInvitation(id:string,input:{action_key:string;expected_version:number;rotate:boolean}){return apiRequest<InvitationResult>(registrationInvitationPath(id),{method:"POST",body:JSON.stringify(input)})}
export function searchTenantIdentities(query:string,signal?:AbortSignal){return apiRequest<{users:TenantIdentity[]}>(tenantIdentitySearchPath(query),{signal})}
export async function acceptOwnerInvitation(token:string,password:string){const response=await fetch(`${apiBaseUrl}/api/auth/owner-invitations/accept`,{method:"POST",headers:{Accept:"application/json","Content-Type":"application/json"},credentials:"omit",cache:"no-store",referrerPolicy:"no-referrer",body:JSON.stringify({token,password})});if(!response.ok)throw new Error("This invitation is invalid, expired, or already used.");return response.json() as Promise<{status:"accepted"}>}
