import assert from "node:assert/strict";
import test from "node:test";

import { schedulerEventPositionRule } from "./scheduler-style";

test("scheduler placement emits nonce-compatible stylesheet rules without inline attributes", () => {
  assert.equal(
    schedulerEventPositionRule("scheduler-event-week-2-7", { top: 31, height: 58, lane: 1, laneCount: 2 }),
    ".scheduler-event-week-2-7{top:31px;height:58px;left:calc(50% + 2px);width:calc(50% - 4px)}"
  );
});

test("scheduler placement rejects selector injection and normalizes invalid numbers", () => {
  assert.throws(() => schedulerEventPositionRule("event}body{display:none", { top: 0, height: 1, lane: 0, laneCount: 1 }));
  assert.equal(
    schedulerEventPositionRule("scheduler-event-day-0-0", { top: Number.NaN, height: -10, lane: 4, laneCount: 0 }),
    ".scheduler-event-day-0-0{top:0px;height:1px;left:calc(0% + 2px);width:calc(100% - 4px)}"
  );
});
