import assert from "node:assert/strict";
import { test } from "node:test";
import { lessonPlaybackPlan } from "./lesson-state";

test("expired Lesson read models issue no playback and mount no Progress reporter", () => {
  assert.deepEqual(lessonPlaybackPlan("expired"), { mountPlayer: false, mountProgressReporter: false });
  assert.deepEqual(lessonPlaybackPlan("active"), { mountPlayer: true, mountProgressReporter: true });
});
