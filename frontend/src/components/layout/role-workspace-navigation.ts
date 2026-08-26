import {
  roleRoot,
  type SessionRole,
} from "../../lib/identity/return-to";

export type WorkspaceRole = Extract<SessionRole, "ADMIN" | "INSTRUCTOR">;

export type WorkspaceNavigationKey =
  | "courseReview"
  | "adminCourses"
  | "academicCatalog"
  | "courseAccess"
  | "courseLifecycle"
  | "reportedContent"
  | "staffOperations"
  | "instructorStudio"
  | "courseBuilder";

export type WorkspaceNavigationItem = {
  key: WorkspaceNavigationKey;
  href: string;
};

export type RoleHomeNavigationKey =
  | "dashboard"
  | "instructorStudio"
  | "adminWorkspace";

export function roleHomeNavigation(
  role: SessionRole,
  locale: "ar" | "en",
): { key: RoleHomeNavigationKey; href: string } {
  const key: RoleHomeNavigationKey =
    role === "ADMIN"
      ? "adminWorkspace"
      : role === "INSTRUCTOR"
        ? "instructorStudio"
        : "dashboard";
  return { key, href: roleRoot(role, locale) };
}

export function roleWorkspaceNavigation(
  role: WorkspaceRole,
  locale: "ar" | "en",
): WorkspaceNavigationItem[] {
  const home = roleRoot(role, locale);
  if (role === "ADMIN") {
    return [
      // Courses leads, because it is the surface an Admin can start from without already knowing
      // which Course they are looking for. The review queue remains its own entry: it is the exact
      // set of pending decisions, and narrowing to it is a different job from browsing the
      // catalogue.
      { key: "adminCourses", href: `/${locale}/admin/courses` },
      { key: "courseReview", href: home },
      { key: "academicCatalog", href: `/${locale}/admin/academic-catalog` },
      { key: "courseAccess", href: `/${locale}/admin/course-access` },
      { key: "courseLifecycle", href: `/${locale}/admin/course-lifecycle` },
      { key: "reportedContent", href: `/${locale}/admin/reported-content` },
      { key: "staffOperations", href: "/staff" },
    ];
  }
  return [
    { key: "instructorStudio", href: home },
    { key: "courseBuilder", href: `${home}#course-builder` },
  ];
}
