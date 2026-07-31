import type { ProvisionInput,ProvisioningDraft,RegistrationDetail,RegistrationListItem,RegistrationStatus } from "../../lib/api/tenant-registration-types";

export function isPlatformRegistrationAdmin(roles: readonly string[]) {
  return roles.includes("platform_admin");
}

export function reviewableTransitions(transitions: readonly RegistrationStatus[]) {
  return transitions.filter((status) => status !== "converted");
}

export function registrationQueueContactLine(item: RegistrationListItem) {
  return [item.contact_full_name,item.contact_email_masked,item.contact_phone_masked].filter(Boolean).join(" · ");
}

export function provisionDefaults(item: RegistrationDetail):ProvisionInput {
  const draft=item.provisioning_draft;
  return {action_key:"",expected_version:item.version,owner:{mode:"create_invited",email:draft?.owner_email||item.contact_email||"",full_name:draft?.owner_full_name||item.contact_full_name,phone:draft?.owner_phone||item.contact_phone},salon:{name:draft?.salon_name||item.salon_name,phone:draft?.salon_phone||item.salon_phone||"",address:draft?.address||"",city:draft?.city||item.city||"",state:draft?.state||item.state||"",zip_code:draft?.zip_code||item.zip_code||"",timezone:draft?.timezone||"America/Chicago",primary_language:draft?.primary_language||(item.preferred_contact_language==="vi"?"vi":"en"),secondary_language:draft?.secondary_language||(item.preferred_contact_language==="vi"?"en":"vi"),handoff_phone:draft?.handoff_phone||item.salon_phone||""}};
}

export function toProvisioningDraft(input:ProvisionInput):ProvisioningDraft {
  return {owner_email:input.owner.email,owner_full_name:input.owner.full_name,owner_phone:input.owner.phone,salon_name:input.salon.name,salon_phone:input.salon.phone,address:input.salon.address,city:input.salon.city,state:input.salon.state,zip_code:input.salon.zip_code,timezone:input.salon.timezone,primary_language:input.salon.primary_language,secondary_language:input.salon.secondary_language,handoff_phone:input.salon.handoff_phone};
}
