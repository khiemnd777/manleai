import assert from "node:assert/strict";
import { platformAuthorityChangePath } from "./scheduling-authority-routes";

assert.equal(
  platformAuthorityChangePath("salon / one"),
  "/api/v2/platform/tenants/salon%20%2F%20one/scheduling/authority"
);
