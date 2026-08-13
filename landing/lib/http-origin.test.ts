import assert from "node:assert/strict";
import test from "node:test";

import { exactHTTPOrigin, requiredHTTPOrigin } from "./http-origin";

test("exact HTTP origins normalize an optional root slash without assuming a host or port", () => {
  assert.equal(exactHTTPOrigin("http://catalog-runtime:49152", "TEST_ORIGIN"), "http://catalog-runtime:49152");
  assert.equal(exactHTTPOrigin("http://catalog-runtime:49152/", "TEST_ORIGIN"), "http://catalog-runtime:49152");
  assert.equal(exactHTTPOrigin("https://public-api.example.test", "TEST_ORIGIN"), "https://public-api.example.test");
});

test("required HTTP origins fail closed on missing or non-origin values", () => {
  assert.throws(() => requiredHTTPOrigin(undefined, "TEST_ORIGIN"), /TEST_ORIGIN is required/);
  assert.throws(() => requiredHTTPOrigin("http://user@catalog-runtime:49152", "TEST_ORIGIN"));
  assert.throws(() => requiredHTTPOrigin("http://catalog-runtime:49152/api", "TEST_ORIGIN"));
  assert.throws(() => requiredHTTPOrigin("http://catalog-runtime:49152?mode=test", "TEST_ORIGIN"));
  assert.throws(() => requiredHTTPOrigin("http://catalog-runtime:49152#fragment", "TEST_ORIGIN"));
});
