import {
  applyCapabilitySelection,
  accessDirectoryPath,
  capabilityLabel,
  delegableCapabilities,
  type CapabilityDefinition
} from "./access-contract";

function assert(condition: unknown, message: string) {
  if (!condition) throw new Error(message);
}

const capabilities: CapabilityDefinition[] = [
  { name: "records.write", display_name: "Manage records", scope: "platform", delegation_scope: "salon", requires: ["records.read"] },
  { name: "global.manage", display_name: "Manage platform", scope: "platform", delegation_scope: "none", requires: [] },
  { name: "records.read", display_name: "Read records", scope: "platform", delegation_scope: "salon", requires: [] }
];

const delegable = delegableCapabilities(capabilities);
assert(delegable.length === 2, "only salon-delegable capabilities should be shown");
assert(delegable[0].name === "records.write", "capabilities should retain backend order");
assert(capabilityLabel("records.write", capabilities) === "Manage records", "backend display name should own visible capability copy");
assert(capabilityLabel("unknown.permission", capabilities) === "unknown.permission", "unknown persisted capability should remain visible without guessing");

const selectedWrite = applyCapabilitySelection([], "records.write", true, capabilities);
assert(selectedWrite.join(",") === "records.read,records.write", "selecting write should include backend-declared requirements");
const removedRead = applyCapabilitySelection(selectedWrite, "records.read", false, capabilities);
assert(removedRead.length === 0, "removing a requirement should remove capabilities that depend on it");

assert(
  accessDirectoryPath("platform", "ops") === "/api/platform/access/platform-users?query=ops&limit=50",
  "Platform account search must use the Platform-only directory"
);
assert(
  accessDirectoryPath("tenant", "manager", "salon-1") === "/api/platform/access/salons/salon-1/tenant-users?query=manager&limit=50",
  "Tenant account search must be scoped to one salon workflow"
);
