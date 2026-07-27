import test from "node:test";
import assert from "node:assert/strict";
import { postLoginDestination, roleRoot, safeReturnTo } from "./return-to";

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
