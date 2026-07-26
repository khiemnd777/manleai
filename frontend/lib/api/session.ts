import { apiRequest } from "@/lib/api/client";
import type { User } from "@/types/api";

export type CurrentSession = {
  user: User;
  roles: string[];
  salon_id?: string;
};

export function getCurrentSession() {
  return apiRequest<CurrentSession>("/api/auth/me");
}

export function isPlatformSession(session: CurrentSession) {
  return session.roles.includes("platform_admin") || session.roles.includes("platform_ops");
}
