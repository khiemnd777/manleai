export type RegistrationFilterValue = string | number | undefined;

export function registrationListPath(filters: Record<string, RegistrationFilterValue>) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") params.set(key, String(value));
  }
  const query = params.toString();
  return `/api/platform/registration-requests${query ? `?${query}` : ""}`;
}

export function registrationRequestPath(id: string) {
  return `/api/platform/registration-requests/${encodeURIComponent(id)}`;
}

export function registrationNotePath(id: string) {
  return `${registrationRequestPath(id)}/notes`;
}

export function registrationProvisionPath(id: string) {
  return `${registrationRequestPath(id)}/provision`;
}

export function registrationInvitationPath(id: string) {
  return `${registrationRequestPath(id)}/owner-invitation`;
}

export function tenantIdentitySearchPath(query: string) {
  return `/api/platform/tenant-identities?query=${encodeURIComponent(query)}`;
}

export function ownerInvitationTokenFromFragment(fragment: string) {
  return new URLSearchParams(fragment.replace(/^#/, "")).get("token") ?? "";
}
