import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { platformTenantContextPath } from "./platform-tenant-routes";

assert.equal(platformTenantContextPath("tenant / one"), "/api/v2/platform/tenants/tenant%20%2F%20one/context");

for (const file of [
  "lib/api/business.ts",
  "lib/api/owner-notification-deliveries.ts",
  "lib/api/square-webhook-events.ts",
  "lib/api/scheduling-authority-switches.ts",
  "features/dashboard/calls-dashboard.tsx",
  "features/dashboard/services-dashboard.tsx",
  "features/dashboard/training-dashboard.tsx",
  "features/platform/platform-integration-settings.tsx"
]) {
  const source = readFileSync(file, "utf8");
  assert.doesNotMatch(source, /\/api\/platform\/tenants/, `${file} must not call the legacy Platform tenant API`);
}

const tenantShell = readFileSync("features/platform/tenant-detail-shell.tsx", "utf8");
assert.match(tenantShell, /label: "Platform Controls"/);
assert.match(tenantShell, /label: "Copy configuration", path: "transfer"/);
assert.match(tenantShell, /label: "History"/);
assert.match(tenantShell, /label: "Configuration transfers", path: "configuration-transfers"/);

const copyConfiguration = readFileSync("features/platform/platform-configuration-transfer.tsx", "utf8");
assert.doesNotMatch(copyConfiguration, /listPlatformTransferRuns|Recent transfer runs/);

const transferHistory = readFileSync("features/platform/platform-configuration-transfer-history.tsx", "utf8");
assert.match(transferHistory, /listPlatformTransferRuns/);
assert.doesNotMatch(transferHistory, /applyPlatformTransfer|previewPlatformTransfer|Apply reviewed transfer/);

const technicalRedirect = readFileSync("app/platform/tenants/[tenantId]/technical/page.tsx", "utf8");
assert.match(technicalRedirect, /redirect\(`\/platform\/tenants\/\$\{params\.tenantId\}\/integrations`\)/);
