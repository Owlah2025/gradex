import assert from "node:assert/strict";
import { test } from "node:test";

import {
  asciiDigits,
  contactChatLink,
  contactRegionOptions,
  formatContactNumber,
  isE164ContactNumber,
  normalizeContactNumber,
  splitContactNumber,
} from "./phone";

test("Kuwait is the default region and every country stays reachable", () => {
  assert.equal(normalizeContactNumber("5001 2345", "KW"), "+96550012345");
  assert.equal(normalizeContactNumber("9912-3456", "KW"), "+96599123456");
  assert.equal(normalizeContactNumber("100 123 4567", "EG"), "+201001234567");
  assert.equal(normalizeContactNumber("50 123 4567", "SA"), "+966501234567");
  // An explicit international prefix wins over the selected country.
  assert.equal(normalizeContactNumber("+20 100 123 4567", "KW"), "+201001234567");
  assert.equal(normalizeContactNumber("00965 5001 2345", "EG"), "+96550012345");
});

test("Arabic keyboard digits are the same number", () => {
  assert.equal(asciiDigits("٥٠٠١٢٣٤٥"), "50012345");
  assert.equal(asciiDigits("۵۰۰۱۲۳۴۵"), "50012345");
  assert.equal(normalizeContactNumber("٥٠٠١ ٢٣٤٥", "KW"), "+96550012345");
});

test("fixed lines, short numbers, letters and extensions are refused", () => {
  for (const input of ["", "   ", "123", "2212 3456", "+965 2212 3456", "abc", "5001 2345 ext 1", "5001+2345", "+999 1234 5678"]) {
    assert.equal(normalizeContactNumber(input, "KW"), null, input);
  }
});

test("the stored shape matches the backend and database rule", () => {
  assert.ok(isE164ContactNumber("+96550012345"));
  assert.ok(!isE164ContactNumber("96550012345"));
  assert.ok(!isE164ContactNumber("+965 5001 2345"));
  assert.ok(!isE164ContactNumber("+0123456789"));
  assert.ok(!isE164ContactNumber(null));
});

test("a chat link is only ever built from a stored E.164 number", () => {
  assert.equal(contactChatLink("+96550012345"), "https://wa.me/96550012345");
  for (const value of [null, undefined, "", "50012345", "+965 5001 2345", "javascript:alert(1)"]) {
    assert.equal(contactChatLink(value), null, String(value));
  }
});

test("display and edit prefill round-trip a stored number", () => {
  assert.equal(formatContactNumber("+96550012345"), "+965 500 12345");
  assert.equal(formatContactNumber("+966501234567"), "+966 50 123 4567");
  assert.equal(formatContactNumber("not a number"), "not a number");
  assert.deepEqual(splitContactNumber("+201001234567"), { region: "EG", national: "1001234567" });
  assert.deepEqual(splitContactNumber(null), { region: "KW", national: "" });
});

test("the country list leads with Kuwait and the Gulf, labelled per language", () => {
  const english = contactRegionOptions("en");
  assert.equal(english[0].region, "KW");
  assert.equal(english[0].callingCode, "965");
  assert.ok(english.some((option) => option.region === "EG" && option.callingCode === "20"));
  assert.ok(english.length > 200, "every country is offered");
  assert.equal(new Set(english.map((option) => option.region)).size, english.length, "no duplicates");
  const arabic = contactRegionOptions("ar");
  assert.notEqual(arabic[0].label, english[0].label, "Arabic labels are localized");
});
