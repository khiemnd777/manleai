import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const page = readFileSync("features/platform/platform-scheduling-settings.tsx", "utf8");
const control = readFileSync("features/platform/platform-scheduling-authority-control.tsx", "utf8");
const api = readFileSync("lib/api/scheduling-behavior.ts", "utf8");

assert.match(page, /getPlatformSchedulingBehavior/);
assert.match(page, /Promise\.allSettled/);
assert.match(page, /setCanSetBookingMode[\s\S]*set_booking_mode/);
assert.match(page, /setCanChangeAuthority[\s\S]*set_authority/);
assert.match(page, />Retry</);
assert.match(control, /Scheduling authority chooses the execution source/);
assert.match(control, /AI booking mode/);
assert.match(control, /Current effective behavior/);
assert.match(control, /Save AI booking mode/);
assert.match(control, /updatePlatformBookingMode/);
assert.match(control, /behavior\.allowed_booking_modes/);
assert.match(control, /behavior\.policy_version/);
assert.match(control, /!canChangeAuthority/);
assert.match(control, /The AI booking mode was not changed/);
assert.match(control, /Scheduling authority was not changed/);
assert.match(control, /latest\.status === "committed"/);
assert.match(control, /Latest readiness check/);
assert.doesNotMatch(control, /Choose who confirms new scheduling work/);
assert.doesNotMatch(control, /Confirm new work only after verified internal availability/);
assert.match(api, /expected_version: expectedVersion/);
assert.match(api, /action_key: actionKey/);
