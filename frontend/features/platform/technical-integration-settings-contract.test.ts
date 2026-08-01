import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const source = readFileSync("features/platform/technical-integration-settings.tsx", "utf8");

for (const required of [
  "Square connection &amp; AI runtime",
  "Scheduling safety",
  "New single booking",
  "Reschedule",
  "Party booking",
  "OAuth write mode",
  "Capability evidence",
  "Reconnect Square",
  "Re-evaluate safety",
  "Active POS provider",
  "Activate Square for this salon",
  "Connection and sync do not select an active POS provider",
  "/active-provider/activate"
]) {
  assert.match(source, new RegExp(required.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
}

assert.match(source, /automatic_single_create\s*\?\s*"Ready"\s*:\s*"Request-only"/);
assert.match(source, /label="Reschedule" value="Request-only"/);
assert.match(source, /label="Party booking" value="Request-only"/);
assert.doesNotMatch(source, /type="checkbox"/);
assert.doesNotMatch(source, /automatic_reschedule\s*:/);
assert.doesNotMatch(source, /automatic_party_create\s*:/);
assert.doesNotMatch(source, /resource_capacity\s*:/);
