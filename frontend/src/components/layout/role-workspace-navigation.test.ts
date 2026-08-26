import assert from "node:assert/strict";
import test from "node:test";
import {
  roleHomeNavigation,
  roleWorkspaceNavigation,
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
