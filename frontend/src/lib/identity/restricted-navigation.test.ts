import test from "node:test";
import assert from "node:assert/strict";
import { isPrivilegedSurface } from "./restricted-navigation";

test("recognises the authenticated surfaces in both route shapes", () => {
  // Locale-addressable routes and the legacy unprefixed ones both exist in the
  // application, so both must be recognised.
  for (const path of [
    "/staff",
    "/instructor/courses",
    "/en/instructor/courses",
    "/ar/instructor/courses",
    "/en/admin/catalog",
    "/ar/admin/course-access",
    "/en/learn/dashboard",
    "/en/access",
  ]) {
    assert.equal(isPrivilegedSurface(path), true, `expected ${path} privileged`);
  }
});

test("leaves public and identity screens alone", () => {
  // Sign-out lives on the ordinary chrome, so redirecting away from these
  // would trap a restricted visitor rather than help them.
  for (const path of [
    "/",
    "/login",
    "/register",
    "/recover",
    "/password-change",
    "/en/catalog",
    "/ar/catalog/kuwait-101",
    "/en/privacy",
    "/ar/terms",
  ]) {
    assert.equal(isPrivilegedSurface(path), false, `expected ${path} public`);
  }
});

test("does not match a lookalike prefix", () => {
  // A route that merely starts with the same letters is a different route.
  assert.equal(isPrivilegedSurface("/staffing"), false);
  assert.equal(isPrivilegedSurface("/instructors-guide"), false);
  assert.equal(isPrivilegedSurface("/en/administration"), false);
});

test("treats a bare locale segment as public", () => {
  assert.equal(isPrivilegedSurface("/en"), false);
  assert.equal(isPrivilegedSurface("/ar"), false);
});

test("ignores anything that is not an origin-relative path", () => {
  assert.equal(isPrivilegedSurface(""), false);
  assert.equal(isPrivilegedSurface("staff"), false);
  assert.equal(isPrivilegedSurface("https://gradex.example/staff"), false);
});
