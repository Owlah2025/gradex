import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  exploreNavigation,
  homeHref,
  primaryNavigation,
  routes,
} from "./nav-items";
import { en } from "../../lib/i18n/dictionaries/en";
import { ar } from "../../lib/i18n/dictionaries/ar";
import { safeReturnTo } from "../../lib/identity/return-to";

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
}

function readSource(relative: string): string {
  return fs.readFileSync(path.join(frontendRoot(), "src", relative), "utf8");
}

/** Every public surface a visitor can be on that is not the landing page. */
const publicSurfaces = [
  "/en/catalog",
  "/ar/catalog",
  "/en/catalog/operating-systems",
  "/ar/catalog/operating-systems",
  "/en/access",
  "/login",
  "/register",
  "/verify-email",
];

test("Home is reachable from every surface that is not the landing page itself", () => {
  // The catalogue was the only route the header offered, which left the logo as
  // the only way back to the start of the product — discoverable if you already
  // know a wordmark is a link, invisible if you do not.
  for (const locale of ["ar", "en"] as const) {
    for (const surface of publicSurfaces) {
      const header = primaryNavigation(surface, locale).map((item) => item.href);
      assert.ok(
        header.includes(routes.home(locale)),
        `${surface} (${locale}) offers no Home in the header`,
      );
      const footer = exploreNavigation(surface, locale).map((item) => item.href);
      assert.ok(
        footer.includes(routes.home(locale)),
        `${surface} (${locale}) offers no Home in the footer`,
      );
    }
  }
});

test("Courses stays reachable alongside Home, and is locale-addressed", () => {
  for (const locale of ["ar", "en"] as const) {
    for (const surface of publicSurfaces) {
      const hrefs = primaryNavigation(surface, locale).map((item) => item.href);
      assert.ok(hrefs.includes(`/${locale}/catalog`), `${surface} lost Courses`);
      // Adding Home must not have pushed the catalogue out, and must not have
      // introduced a route in the other language.
      const other = locale === "ar" ? "en" : "ar";
      assert.ok(
        !hrefs.some((href) => href.startsWith(`/${other}/`)),
        `${surface} (${locale}) offers a ${other} destination`,
      );
    }
  }
});

test("My Learning is offered to a Student session and to nobody else", () => {
  const dashboard = routes.dashboard("en");
  const anonymous = primaryNavigation("/en/catalog", "en").map((item) => item.href);
  assert.ok(
    !anonymous.includes(dashboard),
    "an anonymous visitor is offered a Student surface",
  );

  const student = primaryNavigation("/en/catalog", "en", { studentSession: true }).map(
    (item) => item.href,
  );
  assert.ok(student.includes(dashboard), "a Student is not offered My Learning");

  // An Admin, an Instructor, and a principal whose role the session did not
  // name all reach this with `studentSession` false. `/learn` asserts something
  // about the reader that their session never said.
  const notStudent = primaryNavigation("/en/catalog", "en", { studentSession: false }).map(
    (item) => item.href,
  );
  assert.ok(!notStudent.includes(dashboard), "a non-Student is offered My Learning");
});

test("the header only asks whether the reader is a Student, never invents one", () => {
  for (const surface of ["components/layout/navbar.tsx", "components/layout/mobile-nav.tsx"]) {
    const source = readSource(surface);
    assert.match(
      source,
      /studentSession:\s*session\?\.role === "STUDENT"/,
      `${surface} derives the audience from something other than the session's own role`,
    );
  }
});

test("the mobile sheet offers exactly what the wide header offers", () => {
  // Below `lg` the sheet is the only primary navigation there is, so anything
  // the wide bar has and it does not is unreachable on a phone.
  const navbar = readSource("components/layout/navbar.tsx");
  const sheet = readSource("components/layout/mobile-nav.tsx");
  for (const source of [navbar, sheet]) {
    assert.match(source, /primaryNavigation\(pathname \?\? "\/", locale, \{/);
  }
  // Neither may filter the shared set down afterwards.
  for (const [name, source] of [["navbar", navbar], ["mobile-nav", sheet]] as const) {
    assert.ok(
      !/primary\s*\.\s*(filter|slice)\(/.test(source),
      `${name} narrows the shared navigation set`,
    );
  }
});

test("the logo and the Home entry name one destination", () => {
  // Two controls that both look like the way back, disagreeing about where
  // that is, is worse than one.
  const navbar = readSource("components/layout/navbar.tsx");
  assert.match(navbar, /<Logo href=\{routes\.home\(locale\)\}/);
  for (const locale of ["ar", "en"] as const) {
    assert.equal(homeHref(locale), routes.home(locale));
  }
});

test("the landing page is not locale-prefixed, because that route does not exist", () => {
  // There is no `/[locale]/page.tsx`. Prefixing Home would manufacture a 404,
  // and the language is carried by the preference every `/[locale]/…` visit
  // persists instead.
  const appRoot = path.join(frontendRoot(), "src/app");
  assert.ok(
    !fs.existsSync(path.join(appRoot, "[locale]", "page.tsx")),
    "a locale-addressed landing page exists; Home should now be prefixed",
  );
  assert.equal(homeHref("ar"), "/");
  assert.equal(homeHref("en"), "/");
});

test("both dictionaries name every navigation destination the header renders", () => {
  for (const key of ["home", "courses", "myLearning", "breadcrumb", "primaryNavigation"] as const) {
    assert.equal(typeof en.nav[key], "string", `English nav.${key}`);
    assert.equal(typeof ar.nav[key], "string", `Arabic nav.${key}`);
    assert.notEqual(en.nav[key].trim(), "", `English nav.${key} is blank`);
    assert.notEqual(ar.nav[key].trim(), "", `Arabic nav.${key} is blank`);
    // Arabic must not have silently inherited the English string.
    if (key !== "primaryNavigation") {
      assert.notEqual(ar.nav[key], en.nav[key], `Arabic nav.${key} is untranslated`);
    }
  }
});

test("breadcrumbs are real links, and the current page is not one of them", () => {
  const source = readSource("components/layout/breadcrumbs.tsx");
  // A history-based control cannot be opened in a new tab, cannot be
  // bookmarked, and goes somewhere different depending on how the reader
  // arrived here.
  assert.ok(!source.includes("router.back"), "breadcrumbs use browser history");
  assert.ok(!source.includes("history.back"), "breadcrumbs use browser history");
  assert.match(source, /<Link/, "breadcrumbs render no real link");
  // The last crumb is the page itself: announced, never a link to itself.
  assert.match(source, /aria-current="page"/);
  assert.match(source, /item\.href && !last/);
  // A single crumb describes no hierarchy.
  assert.match(source, /items\.length < 2/);
  // The separator flips with the writing direction and says nothing to a
  // screen reader, because the list structure already conveys the nesting.
  assert.match(source, /locale === "ar" \? ChevronLeft : ChevronRight/);
  assert.match(source, /<Separator[^>]*aria-hidden/);
});

test("the hierarchical surfaces render a breadcrumb", () => {
  const detail = readSource("components/catalog/course-detail.tsx");
  assert.match(detail, /<Breadcrumbs/, "Course Details has no breadcrumb");
  // Stepping up returns to the catalogue the visitor was actually browsing.
  assert.match(detail, /label: dictionary\.nav\.courses, href: backHref/);

  const lesson = readSource(
    "app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx",
  );
  assert.match(lesson, /<Breadcrumbs/, "the Lesson has no breadcrumb");
  assert.match(lesson, /href: `\/\$\{locale\}\/learn\/dashboard`/);
  assert.match(lesson, /href: `\/\$\{locale\}\/learn\/courses\/\$\{lesson\.course_id\}`/);
});

test("a Student inside a Lesson can reach the catalogue and Home without the URL bar", () => {
  const shell = readSource("components/learning/learning-shell.tsx");
  assert.match(shell, /href: dashboardHref, label: labels\.myCourses/);
  assert.match(shell, /href: `\/\$\{locale\}\/catalog`, label: labels\.catalogue/);
  assert.match(shell, /href: "\/", label: labels\.home/);
  // One source of truth, rendered twice — the wide bar and the sheet — so the
  // two cannot drift.
  assert.equal(
    (shell.match(/learningNavigation\.map\(/g) ?? []).length,
    2,
    "the learning navigation is not rendered at both breakpoints from one list",
  );
});

test("the admission screens offer a way out that is not the browser's Back button", () => {
  const source = readSource("components/auth/auth-shell-navigation.tsx");
  assert.match(source, /data-testid="auth-home"/);
  assert.match(source, /data-testid="auth-catalogue"/);
  // The Course link is offered only when the journey came from one, and the
  // destination is revalidated here rather than trusted because an earlier
  // screen saw it. Every hop is an entry point.
  assert.match(source, /safeReturnTo\(searchParams\.get\("returnTo"\)\)/);
  assert.match(source, /destination \?[\s\S]*?data-testid="auth-back-to-course"/);
  const shell = readSource("components/auth/auth-shell.tsx");
  assert.match(shell, /<AuthShellNavigation \/>/);
  assert.match(shell, /React\.Suspense/);
});

test("a hostile returnTo is never rendered as a link on an admission screen", () => {
  for (const hostile of [
    "https://evil.example/steal",
    "//evil.example",
    "/\\evil.example",
    "javascript:alert(1)",
    "/login",
    "/verify-email",
  ]) {
    assert.equal(
      safeReturnTo(hostile),
      null,
      `${hostile} would be offered as "back to the course"`,
    );
  }
  // A genuine Course destination, with its purchase intent, survives intact.
  assert.equal(
    safeReturnTo("/ar/catalog/operating-systems?purchase=1"),
    "/ar/catalog/operating-systems?purchase=1",
  );
});

test("no navigation entry points at a dead in-page anchor off the landing page", () => {
  for (const locale of ["ar", "en"] as const) {
    for (const surface of [...publicSurfaces, "/en/learn/dashboard"]) {
      for (const item of [
        ...primaryNavigation(surface, locale, { studentSession: true }),
        ...exploreNavigation(surface, locale),
      ]) {
        assert.ok(
          !item.href.startsWith("#"),
          `${surface} offers ${item.href}, which resolves on the landing page only`,
        );
      }
    }
  }
});
