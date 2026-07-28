import assert from "node:assert/strict";
import { isPlatformSession } from "./session-contract";

const tenantWithForgedPlatformRole = {
  user: { id: "tenant-1", email: "owner@example.test", full_name: "Owner", status: "active" },
  principal_scope: "tenant" as const,
  salon_id: "salon-1",
  roles: ["tenant_owner", "platform_admin"]
};
assert.equal(isPlatformSession(tenantWithForgedPlatformRole), false, "roles must not change a Tenant identity into a Platform identity");

const platformIdentity = {
  user: { id: "platform-1", email: "ops@example.test", full_name: "Ops", status: "active" },
  principal_scope: "platform" as const,
  roles: []
};
assert.equal(isPlatformSession(platformIdentity), true, "the immutable principal scope owns workspace routing");
