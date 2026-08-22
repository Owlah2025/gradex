import assert from "node:assert/strict";
import test from "node:test";

import { localeFromPath, switchLocalePath } from "./locale-path";

test("replaces the locale segment on a locale-addressed route", () => {
  assert.equal(switchLocalePath("/ar/catalog", "", "en"), "/en/catalog");
  assert.equal(switchLocalePath("/en/catalog", "", "ar"), "/ar/catalog");
});

test("replaces the bare locale root", () => {
  assert.equal(switchLocalePath("/ar", "", "en"), "/en");
});

test("preserves dynamic nested segments", () => {
  assert.equal(
    switchLocalePath("/ar/catalog/introduction-to-programming", "", "en"),
    "/en/catalog/introduction-to-programming",
  );
  assert.equal(
    switchLocalePath("/en/learn/courses/3f2a/lessons/9b1c", "", "ar"),
    "/ar/learn/courses/3f2a/lessons/9b1c",
  );
});

test("only the locale segment is touched, never the letters elsewhere in the path", () => {
  // A naive `pathname.replace("ar", "en")` corrupts every one of these.
  assert.equal(switchLocalePath("/ar/catalog/arabic-101", "", "en"), "/en/catalog/arabic-101");
  assert.equal(switchLocalePath("/ar/catalog/linear-algebra", "", "en"), "/en/catalog/linear-algebra");
  assert.equal(switchLocalePath("/en/catalog/engineering", "", "ar"), "/ar/catalog/engineering");
  assert.equal(switchLocalePath("/en/learn/courses/dear-search", "", "ar"), "/ar/learn/courses/dear-search");
});

test("carries query state across the switch", () => {
  assert.equal(switchLocalePath("/ar/catalog", "?q=algorithms", "en"), "/en/catalog?q=algorithms");
  // A search term containing the locale letters must survive untouched.
  assert.equal(switchLocalePath("/ar/catalog", "?q=arabic", "en"), "/en/catalog?q=arabic");
  assert.equal(switchLocalePath("/ar/catalog", "q=algorithms", "en"), "/en/catalog?q=algorithms");
  assert.equal(switchLocalePath("/ar/catalog", "", "en"), "/en/catalog");
  assert.equal(switchLocalePath("/ar/catalog", null, "en"), "/en/catalog");
  assert.equal(switchLocalePath("/ar/catalog", "?", "en"), "/en/catalog");
});

test("returns null where no locale-addressed equivalent exists", () => {
  // There is no `/[locale]/page.tsx`, and auth and staff routes are not locale-prefixed.
  // Prefixing them would manufacture a 404, so the caller switches in place instead.
  assert.equal(switchLocalePath("/", "", "en"), null);
  assert.equal(switchLocalePath("/login", "", "en"), null);
  assert.equal(switchLocalePath("/staff/accept", "", "en"), null);
  assert.equal(switchLocalePath(null, "", "en"), null);
});

test("an unsupported locale-looking segment is not treated as a locale", () => {
  assert.equal(switchLocalePath("/fr/catalog", "", "en"), null);
  assert.equal(switchLocalePath("/arabic/catalog", "", "en"), null);
});

test("localeFromPath reads the addressed locale", () => {
  assert.equal(localeFromPath("/ar/catalog"), "ar");
  assert.equal(localeFromPath("/en/learn/dashboard"), "en");
  assert.equal(localeFromPath("/login"), null);
  assert.equal(localeFromPath("/"), null);
  assert.equal(localeFromPath(null), null);
});
