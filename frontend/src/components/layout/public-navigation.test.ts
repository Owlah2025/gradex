import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  exploreNavigation,
  isWorkspacePath,
  primaryNavigation,
} from "./nav-items";

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
}

function readSource(relative: string): string {
  return fs.readFileSync(path.join(frontendRoot(), "src", relative), "utf8");
}

test("the landing page's own sections are offered only on the landing page", () => {
  const landing = primaryNavigation("/", "en").map((item) => item.href);
  assert.deepEqual(landing, ["#courses", "#why", "#faq"]);

  // Everywhere else those anchors point at sections that do not exist on the
  // page: three controls that look like navigation, are announced as links,
  // and move the reader nowhere.
  for (const elsewhere of [
    "/en/catalog",
    "/ar/catalog/kuwait-101",
    "/en/learn/dashboard",
    "/en/access",
    "/login",
  ]) {
    const hrefs = primaryNavigation(elsewhere, "en").map((item) => item.href);
    assert.deepEqual(
      hrefs.filter((href) => href.startsWith("#")),
      [],
      `${elsewhere} still offers an in-page anchor from another page`,
    );
  }
});

test("a workspace screen gets no site navigation above its own", () => {
  // The workspace navigation row sits directly under this header. A second,
  // unrelated set of links above it is not navigation.
  for (const workspace of [
    "/en/admin/catalog",
    "/ar/admin/course-access",
    "/en/instructor/courses",
    "/instructor/courses",
    "/staff",
  ]) {
    assert.equal(isWorkspacePath(workspace), true, `${workspace} unrecognised`);
    assert.deepEqual(primaryNavigation(workspace, "en"), []);
  }
  // A route that merely starts with the same letters is a different route.
  for (const notWorkspace of ["/en/administration", "/staffing", "/en/catalog", "/"]) {
    assert.equal(isWorkspacePath(notWorkspace), false, `${notWorkspace} misread`);
  }
});

test("every offered destination is one this application can reach", () => {
  for (const locale of ["ar", "en"] as const) {
    for (const pathname of ["/", "/en/catalog", "/en/admin/catalog", "/login"]) {
      for (const item of [
        ...primaryNavigation(pathname, locale),
        ...exploreNavigation(pathname, locale),
      ]) {
        assert.ok(item.href, `a navigation entry on ${pathname} carries no href`);
        assert.ok(
          item.href.startsWith("/") || item.href.startsWith("#"),
          `${item.href} leaves this origin`,
        );
        if (item.href.startsWith("/")) {
          assert.ok(
            !item.href.startsWith(`/${locale}/${locale}`),
            `${item.href} is doubly prefixed`,
          );
        }
      }
    }
  }
});

test("the footer's Explore column is never empty", () => {
  // A footer column with no links is worse than one useful link.
  for (const pathname of ["/", "/en/catalog", "/en/admin/catalog", "/staff"]) {
    assert.ok(
      exploreNavigation(pathname, "en").length > 0,
      `the Explore column is empty on ${pathname}`,
    );
  }
});

test("the shared header carries no control with nothing behind it", () => {
  const source = readSource("components/layout/auth-actions.tsx");
  // The notifications bell had no href, no handler, and no feature.
  assert.ok(!source.includes("Bell"), "the notifications control is back");
  assert.ok(
    !source.includes("t.nav.notifications"),
    "the notifications label is back",
  );
  // Every Button in here either navigates or does something.
  const buttons = source.match(/<Button[^>]*>/g) ?? [];
  for (const button of buttons) {
    assert.ok(
      button.includes("asChild") || button.includes("onClick") || button.includes("SignOut"),
      `a header button does nothing: ${button}`,
    );
  }
});

test("an unclassifiable session is offered no workspace and no invented role", () => {
  const source = readSource("components/layout/auth-actions.tsx");
  // The forced-state prop's fallback named the Student dashboard for a session
  // whose role had not been read at all.
  assert.ok(!source.includes("AuthState"), "the forced auth state is back");
  assert.ok(
    !source.includes("routes.dashboard"),
    "the header names a Student destination for a session that did not say it was one",
  );
  assert.ok(
    /roleHomeNavigation\(session\.role, locale\)\s*:\s*null/.test(source),
    "an unknown role falls back to a destination instead of to nothing",
  );
  // And the control is rendered only when there is one.
  assert.ok(
    source.includes("{authenticatedHome && ("),
    "a workspace link is rendered without checking there is a workspace",
  );
});

test("the page announces one skip link, from the root layout", () => {
  const layout = readSource("app/layout.tsx");
  assert.ok(layout.includes("<SkipLink />"), "the root layout lost its skip link");
  // Rendering a second one inside a page gives a keyboard reader two identical
  // "skip to content" stops before the content.
  for (const surface of [
    "components/catalog/course-detail.tsx",
    "components/catalog/public-catalogue.tsx",
  ]) {
    assert.ok(
      !readSource(surface).includes("<SkipLink />"),
      `${surface} renders a second skip link under the layout's own`,
    );
  }
});

test("the landing page states no fact the product cannot back", () => {
  const sections = fs
    .readdirSync(path.join(frontendRoot(), "src/components/sections"))
    .filter((file) => file.endsWith(".tsx"))
    .map((file) => readSource(path.join("components/sections", file)))
    .join("\n");
  // No invented social proof: student counts, ratings, testimonials, or
  // universities the catalogue does not actually hold.
  for (const invented of [
    "testimonial",
    "Testimonial",
    "students enrolled",
    "5-star",
    "trusted by",
    "Trusted by",
  ]) {
    assert.ok(!sections.includes(invented), `the landing page claims: ${invented}`);
  }
  // Numbers followed by a "+" are the shape a fabricated statistic takes.
  const fabricated = sections.match(/>\s*[\d,]{2,}\+/g) ?? [];
  assert.deepEqual(fabricated, [], "the landing page shows an invented count");
});
