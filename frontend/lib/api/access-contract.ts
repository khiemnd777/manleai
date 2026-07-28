export type CapabilityDefinition = {
  name: string;
  display_name: string;
  scope: string;
  delegation_scope: string;
  requires: string[];
};

export type AccessDirectoryScope = "tenant" | "platform";

export function accessDirectoryPath(scope: AccessDirectoryScope, query = "", salonID?: string) {
  const encodedQuery = encodeURIComponent(query);
  if (scope === "platform") return `/api/platform/access/platform-users?query=${encodedQuery}&limit=50`;
  if (!salonID) throw new Error("A salon is required for the Tenant account directory.");
  return `/api/platform/access/salons/${encodeURIComponent(salonID)}/tenant-users?query=${encodedQuery}&limit=50`;
}

export function delegableCapabilities(capabilities: CapabilityDefinition[]) {
  return capabilities.filter((capability) => capability.delegation_scope === "salon");
}

export function capabilityLabel(name: string, capabilities: CapabilityDefinition[]) {
  return capabilities.find((capability) => capability.name === name)?.display_name || name;
}

export function applyCapabilitySelection(
  selected: string[],
  capabilityName: string,
  checked: boolean,
  capabilities: CapabilityDefinition[]
) {
  const definitions = new Map(capabilities.map((capability) => [capability.name, capability]));
  const next = new Set(selected);
  if (checked) {
    addCapabilityWithRequirements(next, capabilityName, definitions);
  } else {
    next.delete(capabilityName);
    let changed = true;
    while (changed) {
      changed = false;
      for (const selectedName of [...next]) {
        const requirements = definitions.get(selectedName)?.requires ?? [];
        if (requirements.some((required) => !next.has(required))) {
          next.delete(selectedName);
          changed = true;
        }
      }
    }
  }
  return [...next].sort();
}

function addCapabilityWithRequirements(
  selected: Set<string>,
  capabilityName: string,
  definitions: Map<string, CapabilityDefinition>
) {
  if (selected.has(capabilityName)) return;
  selected.add(capabilityName);
  for (const required of definitions.get(capabilityName)?.requires ?? []) {
    addCapabilityWithRequirements(selected, required, definitions);
  }
}
