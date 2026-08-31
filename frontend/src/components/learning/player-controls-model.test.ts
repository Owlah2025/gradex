import assert from "node:assert/strict";
import { test } from "node:test";
import {
  clampMediaValue,
  DEFAULT_PLAYBACK_RATE,
  formatMediaTime,
  isPlaybackRate,
  PLAYBACK_RATES,
  playbackRateFromSelectValue,
  playbackRateLabel,
  playbackRateSelectValue,
  qualityOptions,
  SEEK_STEP_SECONDS,
} from "./player-controls-model";

test("media values clamp invalid seek and volume inputs safely", () => {
  assert.equal(clampMediaValue(-2, 120), 0);
  assert.equal(clampMediaValue(140, 120), 120);
  assert.equal(clampMediaValue(Number.NaN, 120), 0);
  assert.equal(clampMediaValue(0.5, Number.POSITIVE_INFINITY), 0);
});

test("media time formatting is deterministic and does not emit invalid values", () => {
  assert.equal(formatMediaTime(Number.NaN), "0:00");
  assert.equal(formatMediaTime(65.8), "1:05");
  assert.equal(formatMediaTime(3665), "1:01:05");
});

test("quality options preserve HLS indexes and disambiguate duplicate heights", () => {
  assert.deepEqual(qualityOptions([
    { height: 720, bitrate: 1 },
    { height: 720, bitrate: 2 },
    { height: Number.NaN },
    { height: 1080, bitrate: 3 },
  ]), [
    { levelIndex: 0, label: "720p" },
    { levelIndex: 1, label: "720p 2" },
    { levelIndex: 3, label: "1080p" },
  ]);
  assert.deepEqual(qualityOptions([]), []);
});

test("the playback rates are the offered set, in order, and nothing else is accepted", () => {
  assert.deepEqual([...PLAYBACK_RATES], [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]);
  assert.equal(DEFAULT_PLAYBACK_RATE, 1);
  assert.equal(PLAYBACK_RATES.includes(DEFAULT_PLAYBACK_RATE), true, "normal speed must be one of the offered rates");
  for (const rate of PLAYBACK_RATES) assert.equal(isPlaybackRate(rate), true);
  for (const rejected of [0, -1, 3, 1.1, Number.NaN, Number.POSITIVE_INFINITY]) {
    assert.equal(isPlaybackRate(rejected), false, `${rejected} is not an offered rate`);
  }
});

test("a rate reads as a number and a unit, and round-trips through the control", () => {
  assert.equal(playbackRateLabel(1), "1×");
  assert.equal(playbackRateLabel(0.75), "0.75×");
  assert.equal(playbackRateLabel(2), "2×");
  for (const rate of PLAYBACK_RATES) {
    assert.equal(playbackRateFromSelectValue(playbackRateSelectValue(rate)), rate);
  }
  // A control value is not trusted to name a real rate.
  assert.equal(playbackRateFromSelectValue("rate-3"), null, "an unoffered rate is refused");
  assert.equal(playbackRateFromSelectValue("1"), null, "a bare number is not a valid control value");
  assert.equal(playbackRateFromSelectValue("rate-"), null);
  assert.equal(playbackRateFromSelectValue(""), null);
});

test("the skip interval is one number, shared by the buttons and the shortcuts", () => {
  assert.equal(SEEK_STEP_SECONDS, 10);
});
