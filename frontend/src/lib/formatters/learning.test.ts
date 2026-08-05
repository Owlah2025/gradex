import assert from "node:assert/strict";
import { test } from "node:test";
import {
  DEFAULT_DISPLAY_TIME_ZONE,
  formatLearningExpiry,
  formatLearningInteger,
  formatLearningPercent,
  formatLearningPositionSeconds,
} from "./learning";

test("expiry formatting preserves the authoritative instant and uses the configured timezone", () => {
  const value = "2026-12-22T20:59:59Z";
  const formatted = formatLearningExpiry(value, "en");
  assert.deepEqual(formatted, {
    dateTime: value,
    text: new Intl.DateTimeFormat("en", {
      dateStyle: "medium",
      timeStyle: "short",
      timeZone: DEFAULT_DISPLAY_TIME_ZONE,
    }).format(new Date(value)),
  });
  assert.notEqual(
    formatted?.text,
    new Intl.DateTimeFormat("en", {
      dateStyle: "medium",
      timeStyle: "short",
      timeZone: "UTC",
    }).format(new Date(value)),
  );
});

test("null and malformed expiry values never produce an invented date", () => {
  assert.equal(formatLearningExpiry(null, "ar"), null);
  assert.equal(formatLearningExpiry("not-a-date", "en"), null);
});

test("progress values use locale-aware presentation without changing their values", () => {
  assert.equal(formatLearningInteger(4, "en"), "4");
  assert.equal(formatLearningInteger(4, "ar"), "٤");
  assert.equal(formatLearningPercent(40, "en"), "40%");
  assert.equal(formatLearningPercent(40, "ar"), "٤٠٪");
  assert.equal(formatLearningPositionSeconds(125.5, "en"), "125.5");
  assert.equal(formatLearningPositionSeconds(125.5, "ar"), "١٢٥٫٥");
});
