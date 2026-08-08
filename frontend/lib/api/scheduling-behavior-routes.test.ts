import { strict as assert } from "node:assert";
import test from "node:test";
import { platformBookingModePath, platformSchedulingBehaviorPath } from "./scheduling-behavior-routes";

test("platform scheduling behavior routes encode salon identity", () => {
  assert.equal(
    platformSchedulingBehaviorPath("salon / one"),
    "/api/v2/platform/tenants/salon%20%2F%20one/scheduling/behavior"
  );
  assert.equal(
    platformBookingModePath("salon / one"),
    "/api/v2/platform/tenants/salon%20%2F%20one/scheduling/booking-mode"
  );
});
