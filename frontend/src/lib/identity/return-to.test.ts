import test from "node:test";
import assert from "node:assert/strict";
import type { SessionRole } from "./return-to";
import {
  passwordChangePath,
  postAuthenticationDestination,
  postLoginDestination,
  postPasswordChangeDestination,
  roleRoot,
  safeReturnTo,
  withReturnTo,
} from "./return-to";

test("keeps internal paths, including query and fragment", () => {
  assert.equal(safeReturnTo("/courses"), "/courses");
  assert.equal(safeReturnTo("/courses?page=2"), "/courses?page=2");
  assert.equal(safeReturnTo("/courses#lesson-3"), "/courses#lesson-3");
  assert.equal(safeReturnTo("/courses/kuwait-101"), "/courses/kuwait-101");
});

test("rejects destinations that leave this origin", () => {
  // Each of these navigates off-origin while looking path-like to a naive
  // "starts with a slash" check.
  for (const hostile of [
    "https://evil.example/steal",
    "//evil.example/steal",
    "/\\evil.example",
    "\\\\evil.example",
    "javascript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "http://evil.example",
  ]) {
    assert.equal(safeReturnTo(hostile), null, `expected null for ${hostile}`);
  }
});

test("rejects control characters and oversized values", () => {
  assert.equal(safeReturnTo("/courses\nSet-Cookie: a=b"), null);
  assert.equal(safeReturnTo("/courses\u0000"), null);
  assert.equal(safeReturnTo("/courses\u007f"), null);
  assert.equal(safeReturnTo(`/${"a".repeat(512)}`), null);
});

test("rejects non-strings and empty values", () => {
  for (const value of [null, undefined, "", 42, {}, [], true]) {
    assert.equal(safeReturnTo(value), null);
  }
});

test("refuses to loop back into admission or the API surface", () => {
  assert.equal(safeReturnTo("/login"), null);
  assert.equal(safeReturnTo("/register"), null);
  assert.equal(safeReturnTo("/verify-email"), null);
  assert.equal(safeReturnTo("/verify-email/result"), null);
  assert.equal(safeReturnTo("/api/v1/session"), null);
});

test("does not treat a lookalike prefix as blocked", () => {
  // "/logins" is a different route from "/login" and must survive.
  assert.equal(safeReturnTo("/logins"), "/logins");
  assert.equal(safeReturnTo("/registered-courses"), "/registered-courses");
});

test("prefers a safe destination and falls back to the role root", () => {
  assert.equal(postLoginDestination("STUDENT", "/courses", "en"), "/courses");
  assert.equal(
    postLoginDestination("STUDENT", "https://evil.example", "en"),
    roleRoot("STUDENT", "en"),
  );
  assert.equal(
    postLoginDestination("ADMIN", null, "ar"),
    roleRoot("ADMIN", "ar"),
  );
});

test("maps every role to its existing localized home", () => {
  assert.equal(roleRoot("STUDENT", "en"), "/en/learn/dashboard");
  assert.equal(roleRoot("STUDENT", "ar"), "/ar/learn/dashboard");
  assert.equal(roleRoot("INSTRUCTOR", "en"), "/en/instructor/courses");
  assert.equal(roleRoot("INSTRUCTOR", "ar"), "/ar/instructor/courses");
  assert.equal(roleRoot("ADMIN", "en"), "/en/admin/catalog");
  assert.equal(roleRoot("ADMIN", "ar"), "/ar/admin/catalog");
});

test("carries a validated destination across an admission hop", () => {
  assert.equal(
    withReturnTo("/verify-email", "/courses/kuwait-101"),
    "/verify-email?returnTo=%2Fcourses%2Fkuwait-101",
  );
  assert.equal(
    withReturnTo("/login", "/courses?page=2"),
    "/login?returnTo=%2Fcourses%3Fpage%3D2",
  );
});

test("drops a hostile destination at every admission hop", () => {
  // Each hop is its own entry point, so each revalidates rather than trusting
  // that an earlier screen checked the value. A hostile destination must not
  // survive the hop in any form, raw or encoded.
  const hostile = [
    "https://evil.example/steal",
    "//evil.example/steal",
    "/\\evil.example",
    "\\\\evil.example",
    "javascript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "http://evil.example",
    "/courses\u0000",
    "/courses\nSet-Cookie: a=b",
  ];
  for (const step of ["/login", "/register", "/verify-email", "/recover"]) {
    for (const value of hostile) {
      assert.equal(
        withReturnTo(step, value),
        step,
        `expected ${step} to drop ${JSON.stringify(value)}`,
      );
    }
  }
});

test("does not let an admission step become its own destination", () => {
  // Forwarding a destination that points back into admission would loop the
  // journey, so those roots are refused and the hop stays plain.
  for (const looping of [
    "/login",
    "/register",
    "/verify-email",
    "/api/v1/session",
  ]) {
    assert.equal(withReturnTo("/verify-email", looping), "/verify-email");
  }
});

test("treats absent and non-string destinations as no destination", () => {
  for (const empty of [null, undefined, "", 42, {}, []]) {
    assert.equal(withReturnTo("/login", empty), "/login");
  }
});

test("a restricted principal goes to the password change, whatever it asked for", () => {
  // The founder's finding: signing in successfully and then being refused every
  // screen. A restricted principal must reach the one screen it can act on.
  assert.equal(
    postAuthenticationDestination("ADMIN", null, "en", true),
    passwordChangePath,
  );
  assert.equal(
    postAuthenticationDestination(
      "INSTRUCTOR",
      "/en/instructor/courses",
      "en",
      true,
    ),
    `${passwordChangePath}?returnTo=%2Fen%2Finstructor%2Fcourses`,
  );
});

test("an unrestricted principal is routed normally", () => {
  assert.equal(
    postAuthenticationDestination("STUDENT", "/courses", "en", false),
    "/courses",
  );
  assert.equal(
    postAuthenticationDestination("ADMIN", null, "ar", false),
    roleRoot("ADMIN", "ar"),
  );
});

test("the password-change screen is never an accepted destination", () => {
  // Otherwise a crafted link could park an unrestricted visitor on a form they
  // have no reason to complete.
  assert.equal(safeReturnTo(passwordChangePath), null);
  assert.equal(safeReturnTo(`${passwordChangePath}/anything`), null);
  assert.equal(withReturnTo("/login", passwordChangePath), "/login");
});

test("a completed change lands each role on its own authorized surface", () => {
  assert.equal(
    postPasswordChangeDestination("INSTRUCTOR", null, "en"),
    "/en/instructor/courses",
  );
  assert.equal(
    postPasswordChangeDestination("INSTRUCTOR", null, "ar"),
    "/ar/instructor/courses",
  );
  assert.equal(
    postPasswordChangeDestination("ADMIN", null, "en"),
    "/en/admin/catalog",
  );
  assert.equal(
    postPasswordChangeDestination("STUDENT", null, "en"),
    roleRoot("STUDENT", "en"),
  );
});

test("a completed change still honours the destination the visitor was interrupted on", () => {
  assert.equal(
    postPasswordChangeDestination("ADMIN", "/en/admin/catalog", "en"),
    "/en/admin/catalog",
  );
  // But not a hostile one, revalidated here like at every other hop.
  assert.equal(
    postPasswordChangeDestination("ADMIN", "https://evil.example", "en"),
    "/en/admin/catalog",
  );
});

/**
 * Regression: every navigation destination this module hands out is a string.
 *
 * `roleRoot` was an exhaustive switch over `SessionRole` with no `default`. The compiler accepted
 * it because the union is closed, but the role reaching it is read off `GET /session` and cast
 * without validation, so an unrecognised role fell through and the function returned `undefined`
 * from a signature declaring `string`.
 *
 * The visible consequence was in the shared header: `roleHomeNavigation` put that `undefined` on
 * the workspace `<Link>`, which React rendered as an anchor carrying no `href` — a dead control,
 * and the "prop `href` … got `undefined`" development-overlay warning that appeared on every route
 * in both locales.
 *
 * These assertions fail against the previous behaviour: each one received `undefined`.
 */
const UNKNOWN_ROLE = "SUPPORT" as unknown as SessionRole;

test("an unrecognised session role still resolves to a real destination", () => {
  for (const locale of ["en", "ar"] as const) {
    assert.equal(roleRoot(UNKNOWN_ROLE, locale), `/${locale}/learn/dashboard`);
    // Least privilege: an unknown principal is never pointed at a workspace.
    assert.ok(!roleRoot(UNKNOWN_ROLE, locale).includes("/admin/"));
    assert.ok(!roleRoot(UNKNOWN_ROLE, locale).includes("/instructor/"));
  }
});

test("every known role still resolves to its own surface", () => {
  assert.equal(roleRoot("STUDENT", "en"), "/en/learn/dashboard");
  assert.equal(roleRoot("INSTRUCTOR", "en"), "/en/instructor/courses");
  assert.equal(roleRoot("ADMIN", "en"), "/en/admin/catalog");
  assert.equal(roleRoot("ADMIN", "ar"), "/ar/admin/catalog");
});

test("post-login navigation never resolves to undefined for any role", () => {
  for (const role of ["STUDENT", "INSTRUCTOR", "ADMIN", UNKNOWN_ROLE] as SessionRole[]) {
    for (const locale of ["en", "ar"] as const) {
      assert.equal(typeof postLoginDestination(role, undefined, locale), "string");
    }
  }
});
