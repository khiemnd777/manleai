import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const source = readFileSync("features/dashboard/calls-dashboard.tsx", "utf8");

assert.match(source, /async function loadSessionDetail/);
assert.match(source, /setSessionDetailError\(err instanceof Error \? err\.message : "Could not load call details\."\)/);
assert.match(source, /title="Call details unavailable"/);
assert.match(source, /Retry details/);
assert.match(source, /setSelectedSession\(nextSummary\);\s*await loadSessionDetail/);
assert.doesNotMatch(source, /setError\(err instanceof Error \? err\.message : "Could not load call details\."\)/);

const types = readFileSync("types/api.ts", "utf8");
assert.match(types, /detail_warnings\?: ConversationDetailWarning\[\]/);
