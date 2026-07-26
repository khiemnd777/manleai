import assert from "node:assert/strict";
import test from "node:test";
import { createBrowserSession } from "./browser-session";

test("browser session keeps access token in memory and removes legacy persisted tokens", () => {
  const removed: string[] = [];
  const session = createBrowserSession(() => ({
    removeItem(key: string) {
      removed.push(key);
    }
  }));

  assert.equal(session.getAccessToken(), "");
  session.setAccessToken("short-lived-access");
  assert.equal(session.getAccessToken(), "short-lived-access");
  assert.deepEqual(removed, ["access_token", "refresh_token"]);

  session.clear();
  assert.equal(session.getAccessToken(), "");
  assert.deepEqual(removed, ["access_token", "refresh_token"]);
});

test("browser session remains usable when persistent storage is unavailable", () => {
  const session = createBrowserSession(() => {
    throw new Error("storage blocked");
  });
  session.setAccessToken("memory-only");
  assert.equal(session.getAccessToken(), "memory-only");
  session.clear();
  assert.equal(session.getAccessToken(), "");
});

