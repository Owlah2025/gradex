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

/**
 * Source with its comments removed.
 *
 * The "this module touches no credential material" assertions below are about
 * what the code *does*, and prose that explains why it deliberately does not is
 * not a violation of it. Matching against raw source made the two
 * indistinguishable and turned every explanatory comment into a test failure.
 */
function executableSource(relativePath: string): string {
  return readFileSync(frontendPath(relativePath), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

test("S5 keeps Arabic as the default and resolves the locale from the route", () => {
  assert.equal(defaultLocale, "ar");
  assert.deepEqual(localeDir, { en: "ltr", ar: "rtl" });
  assert.equal(STORAGE_KEY, "gradex.locale");

  const provider = readFileSync(frontendPath("src/lib/i18n/locale-provider.tsx"), "utf8");

  // A locale-addressed URL decides the language, and it does so through the
  // shared path helper rather than by indexing path segments inline. The
  // hand-rolled version had to name every locale-addressed prefix — catalog,
  // learn, access — and silently ignored the ones nobody had remembered to add.
  assert.match(provider, /localeFromPath\(pathname\)/);
  assert.match(provider, /const locale = routeLocale \?\? preferred/);

  // The preference is still written to the existing key, so a session that
  // predates the cookie keeps working.
  assert.match(provider, /localStorage\.setItem\(STORAGE_KEY, locale\)/);

  // The language preference is now server-readable, which is what removes the
  // post-hydration language flash. It is the *only* browser-stored value this
  // provider is allowed to touch: it carries no authority, and a locale
  // provider must never come near session or credential state.
  assert.match(provider, /writeLocaleCookie/);
  // The provider never hand-rolls the cookie. Reading goes through
  // `readLocaleCookie` and writing through `writeLocaleCookie`, so the name,
  // lifetime and SameSite policy live in one place rather than being restated
  // at each call site.
  assert.match(provider, /readLocaleCookie\(/);
  assert.doesNotMatch(
    executableSource("src/lib/i18n/locale-provider.tsx"),
    /document\.cookie\s*=/,
  );
  assert.doesNotMatch(
    executableSource("src/lib/i18n/locale-provider.tsx"),
    /session|token|authorization|credential/i,
  );
});

test("the locale cookie carries a preference and no authority", () => {
  const cookieModule = executableSource("src/lib/i18n/locale-cookie.ts");
  // SameSite=Lax so the preference survives an ordinary top-level navigation
  // back into Gradex, and a one-year lifetime because it is a preference rather
  // than a session fact.
  assert.match(cookieModule, /SameSite=Lax/);
  assert.match(cookieModule, /Max-Age=\$\{localeCookieMaxAgeSeconds\}/);
  // Readable by JavaScript on purpose — the toggle writes it — and therefore
  // deliberately not a place any secret may go.
  assert.doesNotMatch(cookieModule, /session|token|authorization|credential/i);
  // sessionStorage is not in play here either: the preference outlives one tab.
  assert.doesNotMatch(cookieModule, /sessionStorage/);
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
