import { apiRequest } from "@/lib/api/client";
import type { User } from "@/types/api";
import type { PrincipalScope } from "@/lib/api/session-contract";

export { isPlatformSession } from "@/lib/api/session-contract";

export type CurrentSession = {
  user: User;
  roles: string[];
  salon_id?: string;
  principal_scope: PrincipalScope;
};

export function getCurrentSession() {
  return apiRequest<CurrentSession>("/api/auth/me");
}
