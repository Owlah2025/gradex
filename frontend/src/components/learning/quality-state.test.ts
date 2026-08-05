import assert from "node:assert/strict";
import { test } from "node:test";
import {
  applySelectValue,
  initialQualityState,
  intendedHlsLevel,
  levelSwitched,
  levelsLoaded,
  qualitySelectValue,
  qualityValueText,
  selectAuto,
  selectManual,
  sourceReplaced,
} from "./quality-state";

const labels = { quality: "Quality", auto: "Auto" };
const levels = [{ height: 360 }, { height: 720 }, { height: 1080 }];

function loaded(sourceKey = "lesson-a") {
  return levelsLoaded(initialQualityState(sourceKey), levels, sourceKey);
}

test("quality starts in Auto with no active level and drives HLS adaptively", () => {
  const state = loaded();
  assert.deepEqual(state.selection, { mode: "auto" });
  assert.equal(state.activeLevelIndex, -1);
  assert.equal(intendedHlsLevel(state), -1);
  assert.equal(qualitySelectValue(state), "auto");
  assert.equal(qualityValueText(state, labels), "Auto");
});

test("an adaptive LEVEL_SWITCHED records the active level and never converts Auto into a manual pin", () => {
  const switched = levelSwitched(loaded(), 2, "lesson-a");
  assert.deepEqual(switched.selection, { mode: "auto" }, "adaptive switching must not become a Student selection");
  assert.equal(switched.activeLevelIndex, 2);
  // Still adaptive: the player is not pinned to the level it happens to be rendering.
  assert.equal(intendedHlsLevel(switched), -1);
  assert.equal(qualitySelectValue(switched), "auto");
  // Honest about both: Auto is selected, 1080p is playing.
  assert.equal(qualityValueText(switched, labels), "Auto (1080p)");
});

test("a manual selection pins a real HLS level and survives later LEVEL_SWITCHED events", () => {
  const manual = selectManual(loaded(), 1);
  assert.deepEqual(manual.selection, { mode: "manual", levelIndex: 1 });
  assert.equal(intendedHlsLevel(manual), 1, "a manual selection must change the intended real HLS level");
  assert.equal(qualityValueText(manual, labels), "720p");

  const afterSwitch = levelSwitched(manual, 0, "lesson-a");
  assert.deepEqual(afterSwitch.selection, { mode: "manual", levelIndex: 1 }, "an active-level event must not overwrite the manual selection");
  assert.equal(afterSwitch.activeLevelIndex, 0);
  assert.equal(intendedHlsLevel(afterSwitch), 1);
  assert.equal(qualityValueText(afterSwitch, labels), "720p");
});

test("returning to Auto restores adaptive mode", () => {
  const back = selectAuto(selectManual(loaded(), 2));
  assert.deepEqual(back.selection, { mode: "auto" });
  assert.equal(intendedHlsLevel(back), -1);
});

test("source or Lesson replacement resets the selected mode to Auto", () => {
  const pinned = levelSwitched(selectManual(loaded("lesson-a"), 2), 2, "lesson-a");
  const replaced = sourceReplaced("lesson-b");
  assert.deepEqual(replaced.selection, { mode: "auto" }, "a manual pin must not carry into the next Lesson");
  assert.equal(replaced.activeLevelIndex, -1);
  assert.deepEqual(replaced.options, []);
  assert.deepEqual(pinned.selection, { mode: "manual", levelIndex: 2 }, "the previous state object is not mutated");
});

test("stale events from the destroyed HLS instance cannot alter the new player", () => {
  const fresh = levelsLoaded(sourceReplaced("lesson-b"), levels, "lesson-b");
  const stale = levelSwitched(fresh, 2, "lesson-a");
  assert.equal(stale.activeLevelIndex, -1, "a level event from the previous source is ignored");
  assert.deepEqual(stale, fresh);

  const staleLevels = levelsLoaded(fresh, [{ height: 144 }], "lesson-a");
  assert.deepEqual(staleLevels.options, fresh.options, "levels from the previous source are ignored");
});

test("an out-of-range or non-integer level is refused rather than pinned", () => {
  const state = loaded();
  assert.deepEqual(selectManual(state, 9).selection, { mode: "auto" });
  assert.deepEqual(selectManual(state, -1).selection, { mode: "auto" });
  assert.equal(levelSwitched(state, -1, "lesson-a").activeLevelIndex, -1);
  assert.equal(levelSwitched(state, 1.5, "lesson-a").activeLevelIndex, -1);
});

test("select values round-trip without trusting the control to supply an HLS index", () => {
  const state = loaded();
  assert.deepEqual(applySelectValue(state, "auto").selection, { mode: "auto" });
  assert.deepEqual(applySelectValue(state, "level-2").selection, { mode: "manual", levelIndex: 2 });
  assert.deepEqual(applySelectValue(state, "level-99").selection, { mode: "auto" }, "an unoffered level is refused");
  assert.deepEqual(applySelectValue(state, "2").selection, { mode: "auto" }, "a bare index is not a valid control value");
  assert.deepEqual(applySelectValue(state, "").selection, { mode: "auto" });
});

test("accessible text names the selection and resolution, never an internal HLS index", () => {
  const duplicated = levelsLoaded(initialQualityState("s"), [{ height: 720 }, { height: 720 }], "s");
  assert.deepEqual(duplicated.options, [
    { levelIndex: 0, label: "720p" },
    { levelIndex: 1, label: "720p 2" },
  ], "duplicate resolution labels stay deterministic");

  const pinnedSecond = selectManual(duplicated, 1);
  assert.equal(qualityValueText(pinnedSecond, labels), "720p 2");
  for (const state of [duplicated, pinnedSecond, levelSwitched(duplicated, 1, "s")]) {
    const text = qualityValueText(state, labels);
    assert.doesNotMatch(text, /level|index|\blevelIndex\b/i, `accessible text must not expose HLS internals: ${text}`);
  }
});
