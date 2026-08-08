import assert from "node:assert/strict";
import { platformAIRuntimePath } from "./ai-runtime-routes";

assert.equal(
  platformAIRuntimePath("salon / one"),
  "/api/v2/platform/tenants/salon%20%2F%20one/ai-receptionist/runtime"
);

