import {
  roleRoot,
  type SessionRole,
} from "../../lib/identity/return-to";

export type WorkspaceRole = Extract<SessionRole, "ADMIN" | "INSTRUCTOR">;

export type WorkspaceNavigationKey =
  | "adminHome"
  | "adminAnalytics"
  | "courseReview"
  | "adminCourses"
  | "academicCatalog"
  | "subjectDemand"
  | "bundles"
  | "courseAccess"
  | "courseLifecycle"
  | "reportedContent"
  | "staffOperations"
  | "adminUsers"
  | "adminAudit"
  | "adminEmailDeliveries"
  | "adminMediaFailures"
  | "instructorStudio"
  | "instructorProfile"
  | "adminInstructorProfiles"
  | "instructorDashboard"
  | "courseBuilder";

export type WorkspaceNavigationItem = {
  key: WorkspaceNavigationKey;
  href: string;
};

export type RoleHomeNavigationKey =
  | "dashboard"
  | "instructorDashboard"
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
        ? "instructorDashboard"
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
      { key: "adminHome", href: home },
      { key: "adminAnalytics", href: `/${locale}/admin/analytics` },
      { key: "adminInstructorProfiles", href: `/${locale}/admin/instructor-profiles` },
      // Courses leads, because it is the surface an Admin can start from without already knowing
      // which Course they are looking for. The review queue remains its own entry: it is the exact
      // set of pending decisions, and narrowing to it is a different job from browsing the
      // catalogue.
      { key: "adminCourses", href: `/${locale}/admin/courses` },
      { key: "courseReview", href: `/${locale}/admin/catalog` },
      { key: "academicCatalog", href: `/${locale}/admin/academic-catalog` },
      // Demand sits beside the Academic Catalog rather than beside Courses: it is
      // read against Subjects, and it answers "what should exist" rather than
      // anything about the Courses that already do.
      { key: "subjectDemand", href: `/${locale}/admin/subject-demand` },
      { key: "bundles", href: `/${locale}/admin/bundles` },
      { key: "courseAccess", href: `/${locale}/admin/course-access` },
      { key: "courseLifecycle", href: `/${locale}/admin/course-lifecycle` },
      { key: "reportedContent", href: `/${locale}/admin/reported-content` },
      { key: "adminUsers", href: `/${locale}/admin/users` },
      { key: "adminAudit", href: `/${locale}/admin/audit` },
      { key: "adminEmailDeliveries", href: `/${locale}/admin/email-deliveries` },
      { key: "adminMediaFailures", href: `/${locale}/admin/media/failures` },
      { key: "staffOperations", href: "/staff" },
    ];
  }
  return [
    { key: "instructorDashboard", href: home },
    { key: "instructorProfile", href: `/${locale}/instructor/profile` },
    { key: "courseBuilder", href: `/${locale}/instructor/courses` },
  ];
}
