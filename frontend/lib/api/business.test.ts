import { BusinessMutationKeyManager, businessBasePath, businessDirectoryPath, isSampleData, platformBusinessResourcePath } from "./business";

function assert(condition: unknown, message: string) {
  if (!condition) throw new Error(message);
}

const tenant = businessBasePath({ kind: "tenant", salonID: "salon-a" });
const platform = businessBasePath({ kind: "platform", salonID: "salon-a" });
assert(tenant === "/api/salons/salon-a/business", `unexpected tenant path ${tenant}`);
assert(platform === "/api/v2/platform/tenants/salon-a", `unexpected platform path ${platform}`);
assert(platformBusinessResourcePath("salon / a", "profile") === "/api/v2/platform/tenants/salon%20%2F%20a/business/profile", "profile path must use canonical v2 resource");
assert(platformBusinessResourcePath("salon-a", "business-hours") === "/api/v2/platform/tenants/salon-a/business/hours", "hours path must use canonical v2 resource");
assert(platformBusinessResourcePath("salon-a", "staff/staff-1/services") === "/api/v2/platform/tenants/salon-a/staff/staff-1/service-eligibility", "staff eligibility path must be parent-owned");
assert(businessDirectoryPath("tenant") === "/api/salons/", "tenant directory path must be fixed");
assert(businessDirectoryPath("platform") === "/api/v2/platform/tenants", "platform directory path must use the normalized collection");
assert(isSampleData({ data_classification: "sample_test" }), "sample_test must be visibly classifiable");
assert(!isSampleData({ data_classification: "live" }), "live data must not be marked as sample");

const keys = new BusinessMutationKeyManager();
const first = keys.forPayload("service-save", { name: "Gel manicure", duration: 45 });
const exactRetry = keys.forPayload("service-save", { name: "Gel manicure", duration: 45 });
const changed = keys.forPayload("service-save", { name: "Gel manicure", duration: 60 });
assert(first === exactRetry, "exact retry must preserve the action key");
assert(first !== changed, "changed input must replace the action key");
