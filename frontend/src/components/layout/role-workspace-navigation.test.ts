import assert from "node:assert/strict";
import test from "node:test";
import type { SessionRole } from "@/lib/identity/return-to";
import {
  roleHomeNavigation,
  roleWorkspaceNavigation,
  type WorkspaceRole,
} from "./role-workspace-navigation";

// Courses leads the Admin workspace: it is the only entry an Admin can use without already knowing
// which Course they want. The review queue keeps its own entry — it is the exact set of pending
// decisions, which is a different job from browsing the catalogue.
test("Admin workspace navigation exposes the existing launch operations", () => {
  assert.deepEqual(roleWorkspaceNavigation("ADMIN", "en"), [
    { key: "adminCourses", href: "/en/admin/courses" },
    { key: "courseReview", href: "/en/admin/catalog" },
    { key: "academicCatalog", href: "/en/admin/academic-catalog" },
    { key: "courseAccess", href: "/en/admin/course-access" },
    { key: "courseLifecycle", href: "/en/admin/course-lifecycle" },
    { key: "reportedContent", href: "/en/admin/reported-content" },
    { key: "staffOperations", href: "/staff" },
  ]);
  assert.deepEqual(roleWorkspaceNavigation("ADMIN", "ar"), [
    { key: "adminCourses", href: "/ar/admin/courses" },
    { key: "courseReview", href: "/ar/admin/catalog" },
    { key: "academicCatalog", href: "/ar/admin/academic-catalog" },
    { key: "courseAccess", href: "/ar/admin/course-access" },
    { key: "courseLifecycle", href: "/ar/admin/course-lifecycle" },
    { key: "reportedContent", href: "/ar/admin/reported-content" },
    { key: "staffOperations", href: "/staff" },
  ]);
});

test("Instructor workspace navigation exposes the existing authoring journey", () => {
  assert.deepEqual(roleWorkspaceNavigation("INSTRUCTOR", "en"), [
    { key: "instructorStudio", href: "/en/instructor/courses" },
    {
      key: "courseBuilder",
      href: "/en/instructor/courses#course-builder",
    },
  ]);
  assert.deepEqual(roleWorkspaceNavigation("INSTRUCTOR", "ar"), [
    { key: "instructorStudio", href: "/ar/instructor/courses" },
    {
      key: "courseBuilder",
      href: "/ar/instructor/courses#course-builder",
    },
  ]);
});

test("authenticated header destinations and labels follow the signed-in role", () => {
  assert.deepEqual(roleHomeNavigation("STUDENT", "en"), {
    key: "dashboard",
    href: "/en/learn/dashboard",
  });
  assert.deepEqual(roleHomeNavigation("INSTRUCTOR", "ar"), {
    key: "instructorStudio",
    href: "/ar/instructor/courses",
  });
  assert.deepEqual(roleHomeNavigation("ADMIN", "en"), {
    key: "adminWorkspace",
    href: "/en/admin/catalog",
  });
});

/**
 * Regression: the shared header offers no workspace control when the session names no role this
 * application recognises.
 *
 * `roleHomeNavigation` used to hand back `{ key: "dashboard", href: undefined }` for such a role,
 * and the header rendered that as `<Link href={undefined}>` — an anchor with no `href`, which looks
 * operable and navigates nowhere. Returning `null` is what lets the header render nothing at all,
 * and the type makes forgetting to handle it a compile error rather than a dead control.
 *
 * The subsequent `href` assertions are the guard against the other wrong answer: substituting a
 * role workspace. An unknown principal is not a Student, so it is not offered `/learn` either.
 */
const UNKNOWN_ROLE = "SUPPORT" as unknown as SessionRole;

test("an unrecognised role is offered no header workspace control", () => {
  for (const locale of ["en", "ar"] as const) {
    assert.equal(roleHomeNavigation(UNKNOWN_ROLE, locale), null);
  }
});

test("an unrecognised role is offered no workspace navigation entries", () => {
  for (const locale of ["en", "ar"] as const) {
    assert.deepEqual(
      roleWorkspaceNavigation(UNKNOWN_ROLE as unknown as WorkspaceRole, locale),
      [],
    );
  }
});

/**
 * Nothing this module hands the header may carry a missing destination. This is the assertion that
 * would have caught the original defect at its source rather than in the browser console.
 */
test("every navigation entry for every known role carries a real href", () => {
  for (const role of ["STUDENT", "INSTRUCTOR", "ADMIN"] as SessionRole[]) {
    for (const locale of ["en", "ar"] as const) {
      const home = roleHomeNavigation(role, locale);
      assert.notEqual(home, null);
      assert.equal(typeof home!.href, "string");
      assert.ok(home!.href.startsWith(`/${locale}/`), `${role} ${locale}: ${home!.href}`);
    }
  }
  for (const role of ["INSTRUCTOR", "ADMIN"] as WorkspaceRole[]) {
    for (const locale of ["en", "ar"] as const) {
      for (const entry of roleWorkspaceNavigation(role, locale)) {
        assert.equal(typeof entry.href, "string", `${role} ${locale} ${entry.key}`);
        assert.notEqual(entry.href.length, 0, `${role} ${locale} ${entry.key}`);
        assert.ok(!entry.href.includes("undefined"), `${role} ${locale} ${entry.key}: ${entry.href}`);
      }
    }
  }
});
