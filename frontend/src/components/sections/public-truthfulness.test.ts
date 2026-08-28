import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { routes } from "../layout/nav-items.js";

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function source(relativePath: string): string {
  return fs.readFileSync(path.join(frontendRoot(), relativePath), "utf8");
}

test("the landing featured surface is backed by the published catalogue client, not fixtures", () => {
  const featured = source("src/components/sections/featured-courses.tsx");

  // The call now also carries the visitor's academic filters. What this asserts is unchanged: the
  // landing reads the real, locale-aware catalogue client rather than a fixture.
  assert.match(featured, /getPublicCourses\(\s*locale\b/, "the landing must request the authoritative locale-aware catalogue");
  assert.match(featured, /result\.items\.slice\(0, 3\)/, "the landing must render a bounded real response, not a static replacement");
  assert.match(featured, /featured-courses-loading/, "the loading state must remain rendered");
  assert.match(featured, /featured-courses-error/, "the failed state must remain distinct from empty");
  assert.match(featured, /emptyTitle/, "the empty state must remain reachable");
  assert.ok(!featured.includes("@/data/courses"), "the landing must not import the Course fixture");

  for (const removed of ["src/data/courses.ts", "src/components/course/course-card.tsx"]) {
    assert.ok(!fs.existsSync(path.join(frontendRoot(), removed)), `${removed} must not survive as a reusable fabricated Course surface`);
  }
});

test("public route helpers always produce real locale-aware catalogue and dashboard paths", () => {
  assert.equal(routes.catalogue("en"), "/en/catalog");
  assert.equal(routes.catalogue("ar"), "/ar/catalog");
  assert.equal(routes.dashboard("en"), "/en/learn/dashboard");
  assert.equal(routes.dashboard("ar"), "/ar/learn/dashboard");

  for (const routeFile of [
    "src/app/[locale]/catalog/page.tsx",
    "src/app/[locale]/learn/dashboard/page.tsx",
  ]) {
    assert.ok(fs.existsSync(path.join(frontendRoot(), routeFile)), `${routeFile} must exist for a production-visible route`);
  }
});

test("the landing removes fabricated testimonials, unavailable company links, and deferred claims", () => {
  const landing = source("src/app/page.tsx");
  const footer = source("src/components/layout/footer.tsx");
  const faq = source("src/data/faq.ts");
  const learning = source("src/components/sections/learning-experience.tsx");

  assert.ok(!landing.includes("Testimonials"), "the landing must not mount a testimonial shell");
  assert.ok(!fs.existsSync(path.join(frontendRoot(), "src/data/testimonials.ts")), "fabricated testimonial data must be removed");
  assert.ok(!fs.existsSync(path.join(frontendRoot(), "src/components/sections/testimonials.tsx")), "the testimonial surface must be removed");

  for (const obsoletePath of ["/about", "/teach", "/contact", "href=\"#\""]) {
    assert.ok(!footer.includes(obsoletePath), `footer must not link to ${obsoletePath}`);
  }
  for (const prohibitedClaim of ["Tap", "hosted checkout", "refund", "community"]) {
    assert.ok(!faq.includes(prohibitedClaim), `FAQ must not claim ${prohibitedClaim}`);
  }
  assert.match(faq, /outside Gradex/, "FAQ must state that payments are external to Gradex");
  assert.match(faq, /Course Access Invitation/, "FAQ must describe the authoritative access invitation");
  assert.match(faq, /Admin reviews and approves/, "FAQ must preserve the Admin approval requirement");
  assert.ok(!learning.includes("Users"), "the learning steps must not advertise a community step");
});
