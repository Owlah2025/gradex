import test from "node:test";
import assert from "node:assert/strict";
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
  assert.equal(postLoginDestination("STUDENT", "/courses"), "/courses");
  assert.equal(
    postLoginDestination("STUDENT", "https://evil.example"),
    roleRoot("STUDENT"),
  );
  assert.equal(postLoginDestination("ADMIN", null), roleRoot("ADMIN"));
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
    postAuthenticationDestination("ADMIN", null, true),
    passwordChangePath,
  );
  assert.equal(
    postAuthenticationDestination("INSTRUCTOR", "/en/instructor/courses", true),
    `${passwordChangePath}?returnTo=%2Fen%2Finstructor%2Fcourses`,
  );
});

test("an unrestricted principal is routed normally", () => {
  assert.equal(
    postAuthenticationDestination("STUDENT", "/courses", false),
    "/courses",
  );
  assert.equal(
    postAuthenticationDestination("ADMIN", null, false),
    roleRoot("ADMIN"),
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
  assert.equal(postPasswordChangeDestination("ADMIN", null, "en"), "/staff");
  assert.equal(
    postPasswordChangeDestination("STUDENT", null, "en"),
    roleRoot("STUDENT"),
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
    "/staff",
  );
});
