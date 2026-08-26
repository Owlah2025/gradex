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

/**
 * The one workspace entry the shared header offers a signed-in visitor, or `null` when the session
 * names no role this application recognises.
 *
 * `null` rather than a fallback destination: the header's job here is to offer the visitor *their*
 * workspace, and there is no honest answer to that for an unclassifiable principal. Naming one
 * would either invent a role or hand out a link the server refuses. The caller renders no workspace
 * control at all in that case — Sign out remains, which is the action that actually applies.
 */
export function roleHomeNavigation(
  role: SessionRole,
  locale: "ar" | "en",
): { key: RoleHomeNavigationKey; href: string } | null {
  const href = roleRoot(role, locale);
  if (href === null) return null;
  const key: RoleHomeNavigationKey =
    role === "ADMIN"
      ? "adminWorkspace"
      : role === "INSTRUCTOR"
        ? "instructorStudio"
        : "dashboard";
  return { key, href };
}

export function roleWorkspaceNavigation(
  role: WorkspaceRole,
  locale: "ar" | "en",
): WorkspaceNavigationItem[] {
  const home = roleRoot(role, locale);
  // `WorkspaceRole` narrows to ADMIN | INSTRUCTOR, both of which have a root — but the value is
  // still a runtime string off the session, and an empty navigation is the correct answer to "which
  // workspace entries does an unrecognised role get" rather than a row of links built on `null`.
  if (home === null) return [];
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
