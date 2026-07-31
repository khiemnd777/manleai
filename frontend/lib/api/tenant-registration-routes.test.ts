import assert from "node:assert/strict";
import { ownerInvitationTokenFromFragment,registrationInvitationPath,registrationListPath,registrationNotePath,registrationProvisionPath,registrationRequestPath,tenantIdentitySearchPath } from "./tenant-registration-routes";

assert.equal(registrationListPath({status:"qualified",limit:25,offset:0,query:""}),"/api/platform/registration-requests?status=qualified&limit=25&offset=0");
assert.equal(registrationRequestPath("request/id"),"/api/platform/registration-requests/request%2Fid");
assert.equal(registrationNotePath("request/id"),"/api/platform/registration-requests/request%2Fid/notes");
assert.equal(registrationProvisionPath("request/id"),"/api/platform/registration-requests/request%2Fid/provision");
assert.equal(registrationInvitationPath("request/id"),"/api/platform/registration-requests/request%2Fid/owner-invitation");
assert.equal(tenantIdentitySearchPath("owner+test@example.test"),"/api/platform/tenant-identities?query=owner%2Btest%40example.test");
assert.equal(ownerInvitationTokenFromFragment("#token=opaque%2Btoken"),"opaque+token");
assert.equal(ownerInvitationTokenFromFragment("#other=value"),"");
