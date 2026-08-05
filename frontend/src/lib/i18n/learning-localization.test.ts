import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { defaultLocale, localeDir, STORAGE_KEY } from "./config";
import { ar } from "./dictionaries/ar";
import { en } from "./dictionaries/en";

function frontendPath(relativePath: string): string {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : join(process.cwd(), "frontend");
  return join(root, relativePath);
}

test("S5 keeps Arabic as the default and uses the existing persisted locale mechanism", () => {
  assert.equal(defaultLocale, "ar");
  assert.deepEqual(localeDir, { en: "ltr", ar: "rtl" });
  assert.equal(STORAGE_KEY, "gradex.locale");

  const provider = readFileSync(frontendPath("src/lib/i18n/locale-provider.tsx"), "utf8");
  assert.match(provider, /localStorage\.getItem\(STORAGE_KEY\)/);
  assert.match(provider, /localStorage\.setItem\(STORAGE_KEY, next\)/);
  assert.match(provider, /routeSegments\[2\] === "learn"/);
  assert.doesNotMatch(provider, /session|token|authorization|cookie/i);
});

test("S5 interface dictionaries have matching learning and player keys", () => {
  assert.deepEqual(Object.keys(ar.learning).sort(), Object.keys(en.learning).sort());
  assert.deepEqual(Object.keys(ar.player).sort(), Object.keys(en.player).sort());
  for (const key of Object.keys(en.learning) as Array<keyof typeof en.learning>) {
    assert.equal(typeof en.learning[key], "string", `English learning.${String(key)}`);
    assert.equal(typeof ar.learning[key], "string", `Arabic learning.${String(key)}`);
  }
  for (const key of Object.keys(en.player) as Array<keyof typeof en.player>) {
    assert.equal(typeof en.player[key], "string", `English player.${String(key)}`);
    assert.equal(typeof ar.player[key], "string", `Arabic player.${String(key)}`);
  }
});

test("learning pages preserve authored content and do not expose internal denial causes", () => {
  const sources = [
    readFileSync(frontendPath("src/components/learning/learning-views.tsx"), "utf8"),
    readFileSync(frontendPath("src/app/[locale]/learn/dashboard/page.tsx"), "utf8"),
    readFileSync(frontendPath("src/app/[locale]/learn/courses/[courseId]/page.tsx"), "utf8"),
    readFileSync(frontendPath("src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx"), "utf8"),
  ].join("\n");

  assert.match(sources, /course\.title/);
  assert.match(sources, /section\.title/);
  assert.match(sources, /lesson\.title/);
  assert.doesNotMatch(sources, /revoked|suspended|retired|entitlement|enrollment/i);
});

test("learning surfaces use locale direction and semantic expiry output", () => {
  for (const page of [
    "src/app/[locale]/learn/dashboard/page.tsx",
    "src/app/[locale]/learn/courses/[courseId]/page.tsx",
    "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx",
  ]) {
    const source = readFileSync(frontendPath(page), "utf8");
    assert.match(source, /dir=\{locale === "ar" \? "rtl" : "ltr"\}/);
  }
  const views = readFileSync(frontendPath("src/components/learning/learning-views.tsx"), "utf8");
  assert.match(views, /<time dateTime=\{formatted\.dateTime\}>/);
  assert.match(views, /labels\.noExpiry/);
});
