import assert from "node:assert/strict";
import { test } from "node:test";
import { clampMediaValue, formatMediaTime, qualityOptions } from "./player-controls-model";

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
