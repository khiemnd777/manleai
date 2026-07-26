import assert from "node:assert/strict";
import test from "node:test";

import { buildContentSecurityPolicy } from "./content-security-policy";

test("production CSP is nonce-bound and excludes unsafe or wildcard sources", () => {
  const policy = buildContentSecurityPolicy({
    nonce: "releaseGateNonce_123456789",
    apiBaseURL: "https://api.example.com/v1/",
    development: false
  });

  assert.match(policy, /script-src 'self' 'nonce-releaseGateNonce_123456789' 'strict-dynamic'/);
  assert.match(policy, /connect-src 'self' https:\/\/api\.example\.com/);
  assert.match(policy, /style-src-attr 'none'/);
  assert.match(policy, /upgrade-insecure-requests/);
  assert.doesNotMatch(policy, /unsafe-inline|unsafe-eval|\*/);
});

test("CSP rejects non-HTTP API origins and malformed nonces", () => {
  assert.throws(() => buildContentSecurityPolicy({
    nonce: "releaseGateNonce_123456789",
    apiBaseURL: "javascript:alert(1)",
    development: false
  }));
  assert.throws(() => buildContentSecurityPolicy({
    nonce: "quoted'nonce",
    apiBaseURL: "https://api.example.com",
    development: false
  }));
});
