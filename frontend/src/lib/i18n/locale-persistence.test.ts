import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  isLocale,
  localeCookieMaxAgeSeconds,
  localeCookieName,
  readLocaleCookie,
} from "./locale-cookie";
import { localeFromPath, localePath, switchLocalePath } from "./locale-path";
import { defaultLocale } from "./config";

/**
 * Source with its comments removed.
 *
 * The "this layout is not dynamic" assertions are about what the code calls.
 * The docblock explaining why it deliberately does not call `cookies()` is not
 * a violation of that, and matching raw source made the two indistinguishable.
 */
function executableSource(relative: string): string {
  return readSource(relative)
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function readSource(relative: string): string {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src", relative), "utf8");
}

test("the root layout stays static, so one navigation does not re-render the shell", () => {
  // This is a performance contract with a measured failure behind it. Calling
  // `cookies()` here — which an earlier attempt did, to get `<html lang>` right
  // on the first byte — makes the root layout dynamic, and a dynamic *root*
  // layout is re-rendered and re-serialized on every client navigation in the
  // product rather than being treated as an unchanged shell. One soft
  // navigation to the Admin review workspace went from ~2.0s to ~4.3s.
  const layout = executableSource("app/layout.tsx");
  assert.doesNotMatch(layout, /\bcookies\(\)/, "the root layout reads cookies and is therefore dynamic");
  assert.doesNotMatch(layout, /\bheaders\(\)/, "the root layout reads headers and is therefore dynamic");
  assert.doesNotMatch(layout, /export const dynamic/, "the root layout opts out of static rendering");
  assert.doesNotMatch(
    layout,
    /export default async function RootLayout/,
    "an async root layout is one `await` away from being dynamic",
  );
  assert.match(layout, /<Providers>/);
});

test("a locale-addressed route needs neither a cookie nor a stored value", () => {
  // These are the routes the reported defect was about, and they resolve their
  // language from the URL on the server as well as the client — so they render
  // the right dictionary on the first byte with the shell still static.
  const provider = readSource("lib/i18n/locale-provider.tsx");
  assert.match(provider, /const routeLocale = localeFromPath\(pathname\)/);
  assert.match(provider, /const locale = routeLocale \?\? preferred/);
  // The correcting effect is scoped to the routes the URL does not name.
  assert.match(provider, /if \(routeLocale\) return;\s*\n\s*const saved = savedLocale\(\)/);
});

test("an unreadable or hostile cookie value falls back to the default", () => {
  assert.equal(readLocaleCookie(`${localeCookieName}=ar`), "ar");
  assert.equal(readLocaleCookie(`other=1; ${localeCookieName}=en; x=2`), "en");
  for (const hostile of [
    `${localeCookieName}=fr`,
    `${localeCookieName}=`,
    `${localeCookieName}=<script>`,
    "unrelated=ar",
    "",
    null,
    undefined,
  ]) {
    assert.equal(readLocaleCookie(hostile), null, `accepted ${String(hostile)}`);
  }
  assert.equal(isLocale("ar"), true);
  assert.equal(isLocale("en"), true);
  assert.equal(isLocale("de"), false);
  assert.equal(isLocale(undefined), false);
  // A year: the choice is a preference, not a session fact.
  assert.equal(localeCookieMaxAgeSeconds, 60 * 60 * 24 * 365);
});

test("an explicit locale URL wins over a saved preference", () => {
  // An English reader opening a shared `/ar/…` link must read Arabic, and an
  // Arabic reader must not have a stale preference re-flip a page they
  // navigated to on purpose.
  assert.equal(localeFromPath("/ar/catalog/operating-systems"), "ar");
  assert.equal(localeFromPath("/en/learn/dashboard"), "en");
  assert.equal(localeFromPath("/ar/access"), "ar");
  // Unprefixed routes name no locale; those are the ones the cookie answers.
  for (const unprefixed of ["/", "/login", "/register", "/verify-email", "/staff"]) {
    assert.equal(localeFromPath(unprefixed), null, `${unprefixed} claimed a locale`);
  }

  const provider = readSource("lib/i18n/locale-provider.tsx");
  assert.match(provider, /const routeLocale = localeFromPath\(pathname\)/);
  assert.match(provider, /const locale = routeLocale \?\? preferred/);
});

test("visiting an explicit locale URL records the choice for the unprefixed screens", () => {
  // This is the half that was missing: `/ar/catalog` set the language for that
  // page and returned before persisting, so following an ordinary link to sign
  // in landed on a screen still rendering an older, unrelated preference.
  const provider = readSource("lib/i18n/locale-provider.tsx");
  assert.match(provider, /if \(!routeLocale\) return;\s*\n\s*setPreferred\(routeLocale\);\s*\n\s*persistLocale\(routeLocale\)/);
  assert.match(provider, /function persistLocale/);
  assert.match(provider, /writeLocaleCookie\(locale\)/);
});

test("switching language keeps the query, which is where the purchase intent lives", () => {
  assert.equal(
    switchLocalePath("/ar/catalog/operating-systems", "?purchase=1", "en"),
    "/en/catalog/operating-systems?purchase=1",
  );
  assert.equal(
    switchLocalePath("/en/catalog/operating-systems", "purchase=1&returnTo=%2Far%2Fx", "ar"),
    "/ar/catalog/operating-systems?purchase=1&returnTo=%2Far%2Fx",
  );
  // Only the locale segment moves. Substring replacement would corrupt any
  // path containing those two letters.
  assert.equal(switchLocalePath("/ar/catalog/arabic-101", "", "en"), "/en/catalog/arabic-101");
  // An unprefixed route has no locale-addressed equivalent; the caller switches
  // the dictionary in place rather than navigating somewhere that would 404.
  for (const unprefixed of ["/", "/login", "/verify-email", "/staff"]) {
    assert.equal(switchLocalePath(unprefixed, "", "en"), null, `${unprefixed} was prefixed`);
  }
});

test("localePath never doubly prefixes an already-addressed route", () => {
  assert.equal(localePath("/catalog", "ar"), "/ar/catalog");
  assert.equal(localePath("/ar/catalog", "en"), "/en/catalog");
  assert.equal(localePath("/", "en"), "/en");
});

test("Arabic remains the default for a visitor who has expressed no preference", () => {
  assert.equal(defaultLocale, "ar");
  const layout = readSource("app/layout.tsx");
  assert.match(layout, /lang=\{defaultLocale\}/);
  assert.match(layout, /dir=\{localeDir\[defaultLocale\]\}/);
});

test("a stored preference is applied on the routes the URL does not name", () => {
  // The landing page, the admission screens and `/staff` carry no locale
  // segment, so the preference is the only thing that can answer for them. It
  // is read from the cookie first — the value a language switch always writes
  // and the one the server could read if it ever needed to — and from
  // `localStorage` second, which covers a choice made before the cookie existed
  // and a browser that refuses cookies but not storage.
  const provider = readSource("lib/i18n/locale-provider.tsx");
  assert.match(provider, /const saved = savedLocale\(\);/);
  assert.match(provider, /setPreferred\(saved\);\s*\n\s*persistLocale\(saved\);/);
  assert.match(provider, /function savedLocale\(\): Locale \| null/);
  assert.match(provider, /readLocaleCookie\(/);
  assert.match(provider, /localStorage\.getItem\(STORAGE_KEY\)/);
  // Reading storage must never be able to throw out of render.
  assert.match(provider, /try \{[\s\S]*?localStorage\.getItem\(STORAGE_KEY\)[\s\S]*?\} catch/);
});

test("the language toggle persists the choice on unprefixed routes too", () => {
  const toggle = readSource("components/common/language-toggle.tsx");
  // On a locale-addressed route the URL is the state, so switching navigates.
  assert.match(toggle, /switchLocalePath\(pathname, search, next\)/);
  assert.match(toggle, /setLocale\(next\);\s*\n\s*router\.push\(target\)/);
  // On an unprefixed route there is nowhere to navigate, so the preference is
  // switched in place — and `toggleLocale` routes through `setLocale`, which is
  // what writes the cookie the server reads next time.
  assert.match(toggle, /if \(target === null\) \{\s*\n\s*toggleLocale\(\);/);
  const provider = readSource("lib/i18n/locale-provider.tsx");
  assert.match(provider, /const toggleLocale = React\.useCallback\(\(\) => \{\s*\n\s*setLocale\(/);
});
