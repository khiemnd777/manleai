export type PrincipalScope = "tenant" | "platform";

export function isPlatformSession(session: { principal_scope: PrincipalScope }) {
  return session.principal_scope === "platform";
}
