import assert from "node:assert/strict";
import test from "node:test";
import type { ConfigurationBundle } from "../../types/api";
import { inspectPlatformConfiguration, platformTransferBase, platformTransferRequestSignature } from "./platform-configuration-transfer";

function configuration(schemaVersion: string, includedSections?: string[]): ConfigurationBundle {
  return {
    schema_version: schemaVersion,
    exported_at: "2026-07-14T00:00:00Z",
    secrets_exported: false,
    operational_data_exported: false,
    included_sections: includedSections,
    excluded_data: [],
    requires_secret_reentry: []
  };
}

test("Platform Transfer uses one encoded destination tenant path", () => {
  assert.equal(
    platformTransferBase("salon/a"),
    "/api/platform/tenants/salon%2Fa/configuration-transfer"
  );
});

test("review signature changes when source, scope, or uploaded bundle changes", () => {
  const first = platformTransferRequestSignature({
    source_type: "tenant",
    source_tenant_id: "source-a",
    included_sections: ["salon_profile", "knowledge_base"]
  });
  const exact = platformTransferRequestSignature({
    source_type: "tenant",
    source_tenant_id: "source-a",
    included_sections: ["salon_profile", "knowledge_base"]
  });
  const differentScope = platformTransferRequestSignature({
    source_type: "tenant",
    source_tenant_id: "source-a",
    included_sections: ["knowledge_base"]
  });
  const differentSource = platformTransferRequestSignature({
    source_type: "tenant",
    source_tenant_id: "source-b",
    included_sections: ["salon_profile", "knowledge_base"]
  });

  assert.equal(first, exact);
  assert.notEqual(first, differentScope);
  assert.notEqual(first, differentSource);

  const uploaded = platformTransferRequestSignature({
    source_type: "json_upload",
    included_sections: ["knowledge_base"],
    configuration: {
      schema_version: "manleai.salon_configuration.v9",
      exported_at: "2026-07-28T00:00:00Z",
      secrets_exported: false,
      operational_data_exported: false,
      included_sections: ["knowledge_base"],
      excluded_data: [],
      requires_secret_reentry: [],
      knowledge_base: { count: 1, items: [] }
    }
  });
  const changedUpload = platformTransferRequestSignature({
    source_type: "json_upload",
    included_sections: ["knowledge_base"],
    configuration: {
      schema_version: "manleai.salon_configuration.v9",
      exported_at: "2026-07-28T00:00:00Z",
      secrets_exported: false,
      operational_data_exported: false,
      included_sections: ["knowledge_base"],
      excluded_data: [],
      requires_secret_reentry: [],
      knowledge_base: { count: 2, items: [] }
    }
  });
  assert.notEqual(uploaded, changedUpload);
});

test("scoped v7 content packs retain their source payload and select only declared sections", () => {
  const source = configuration("manleai.salon_configuration.v7", [
    "service_categories",
    "service_aliases",
    "service_consultation_profiles"
  ]);
  const inspected = inspectPlatformConfiguration(source);

  assert.equal(inspected.configuration, source);
  assert.equal(inspected.configuration.schema_version, "manleai.salon_configuration.v7");
  assert.equal(inspected.legacy_v7_adapted, true);
  assert.deepEqual(inspected.included_sections, [
    "service_categories",
    "service_aliases",
    "service_consultation_profiles"
  ]);
});

test("v7 runtime scope and pre-v7 schemas fail closed", () => {
  assert.throws(
    () => inspectPlatformConfiguration(configuration("manleai.salon_configuration.v7", ["service_categories", "ai_receptionist"])),
    /only service categories, service aliases, consultation profiles, and knowledge base/
  );
  assert.throws(
    () => inspectPlatformConfiguration(configuration("manleai.salon_configuration.v6", ["service_categories"])),
    /supports v9, v8, and scoped content-only v7/
  );
});

test("v8 files without an explicit scope use the legacy full portable section set", () => {
  const inspected = inspectPlatformConfiguration(configuration("manleai.salon_configuration.v8"));
  assert.equal(inspected.legacy_v7_adapted, false);
  assert.equal(inspected.included_sections.includes("local_business_hours"), false);
  assert.equal(inspected.included_sections.includes("integrations"), true);
  assert.equal(inspected.included_sections.length, 8);
});
