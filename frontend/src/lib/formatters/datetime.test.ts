import assert from "node:assert/strict";
import test from "node:test";
import { DISPLAY_TIME_ZONE, formatTimestamp } from "./datetime";
import { formatLearningExpiry } from "./learning";

const INSTANT = "2026-08-27T19:25:00Z";

test("an Arabic date is written in Arabic-Indic digits", () => {
  // The defect this replaced rendered `toLocaleString("ar-KW")`, which returns Latin digits beside
  // Arabic words. Asserting on the digits themselves rather than the whole string keeps this
  // independent of how ICU chooses to order the parts.
  const text = formatTimestamp(INSTANT, "ar");
  assert.ok(text !== null);
  assert.match(text, /[٠-٩]/, "an Arabic timestamp fell back to Latin digits");
  assert.doesNotMatch(text, /[0-9]/, "Latin digits survived in an Arabic timestamp");
});

test("an English date is written in Latin digits", () => {
  const text = formatTimestamp(INSTANT, "en");
  assert.ok(text !== null);
  assert.match(text, /[0-9]/);
});

test("every rendered instant is in the platform's display timezone", () => {
  // Kuwait is UTC+3 and observes no daylight saving, so 19:25Z is 22:25 local on this date. The
  // point is that the answer does not depend on where the reader's machine happens to be — a price
  // history an Administrator reconciles against an audit log has to name one clock.
  const text = formatTimestamp(INSTANT, "en");
  assert.ok(text !== null);
  assert.match(text, /10:25/, `expected Kuwait local time in "${text}"`);
  assert.equal(DISPLAY_TIME_ZONE, "Asia/Kuwait");
});

test("an unparseable instant is refused rather than rendered as Invalid Date", () => {
  assert.equal(formatTimestamp("not-a-date", "en"), null);
});

test("the learning expiry keeps the exact text the shared formatter produces", () => {
  // `formatLearningExpiry` now delegates. If the two ever disagree, a Student's access-expiry date
  // and an Administrator's audit row are describing the same instant differently again.
  const expiry = formatLearningExpiry(INSTANT, "ar");
  assert.ok(expiry !== null);
  assert.equal(expiry.text, formatTimestamp(INSTANT, "ar"));
  assert.equal(expiry.dateTime, INSTANT);
});
