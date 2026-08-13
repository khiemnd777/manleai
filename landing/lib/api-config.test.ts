import assert from "node:assert/strict";
import test from "node:test";

test("server API configuration is resolved at request time instead of image build time", async () => {
  const previousValue = process.env.LANDING_API_BASE_URL;
  delete process.env.LANDING_API_BASE_URL;

  try {
    await assert.doesNotReject(import("./api"));
  } finally {
    if (previousValue === undefined) {
      delete process.env.LANDING_API_BASE_URL;
    } else {
      process.env.LANDING_API_BASE_URL = previousValue;
    }
  }
});
