import assert from "node:assert/strict";
import type { RegistrationDetail,RegistrationListItem } from "../../lib/api/tenant-registration-types";
import { isPlatformRegistrationAdmin,provisionDefaults,registrationQueueContactLine,reviewableTransitions,toProvisioningDraft } from "./tenant-registration-contract";

assert.equal(isPlatformRegistrationAdmin(["platform_admin"]),true);
assert.equal(isPlatformRegistrationAdmin(["platform_ops"]),false);
assert.deepEqual(reviewableTransitions(["qualified","converted","declined"]),["qualified","declined"],"frontend consumes backend transitions but never offers generic converted transition");

const listItem={contact_full_name:"Owner",contact_email_masked:"o••••@example.test",contact_phone_masked:"•••-•••-0148"} as RegistrationListItem;
const contactLine=registrationQueueContactLine(listItem);
assert.match(contactLine,/o••••@example\.test/);
assert.doesNotMatch(contactLine,/owner@example\.test|3125550148/);

const detail={version:4,contact_email:"owner@example.test",contact_full_name:"Owner",contact_phone:"312-555-0148",salon_name:"Prepared Nails",salon_phone:"773-555-0180",city:"Chicago",state:"IL",zip_code:"60614",preferred_contact_language:"vi"} as RegistrationDetail;
const provision=provisionDefaults(detail);
assert.equal(provision.expected_version,4);
assert.equal(provision.owner.mode,"create_invited");
assert.equal(provision.salon.timezone,"America/Chicago");
assert.equal(provision.salon.primary_language,"vi");
assert.equal(provision.salon.secondary_language,"en");
const draft=toProvisioningDraft(provision);
assert.equal(draft.owner_email,"owner@example.test");
assert.equal(draft.salon_name,"Prepared Nails");
