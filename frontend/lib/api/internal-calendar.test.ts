import assert from "node:assert/strict";
import { internalCalendarMutationPath, internalCalendarPath } from "./internal-calendar";

const tenantID = "salon / one";
const platformRoot = "/api/v2/platform/tenants/salon%20%2F%20one/scheduling/internal-calendar";

assert.equal(internalCalendarPath(tenantID, "platform"), platformRoot);
assert.equal(internalCalendarPath(tenantID, "tenant"), "/api/salons/salon%20%2F%20one/manleai-calendar");

for (const [suffix, expected] of [
  ["/config", "/policy"],
  ["/hours", "/hours"],
  ["/staff/staff-1", "/staff/staff-1"],
  ["/services/service-1", "/services/service-1"],
  ["/resources", "/resources"],
  ["/resources/pool-1/archive", "/resources/pool-1/archive"],
  ["/exceptions", "/exceptions"],
  ["/exceptions/exception-1/cancel", "/exceptions/exception-1/cancel"],
  ["/activate", "/activation"]
] as const) {
  assert.equal(internalCalendarMutationPath(tenantID, suffix, "platform"), platformRoot + expected);
}

assert.equal(
  internalCalendarMutationPath(tenantID, "/config", "tenant"),
  "/api/salons/salon%20%2F%20one/manleai-calendar/config"
);
