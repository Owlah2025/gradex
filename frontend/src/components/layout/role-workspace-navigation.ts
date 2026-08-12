import {
  roleRoot,
  type SessionRole,
} from "../../lib/identity/return-to";

export type WorkspaceRole = Extract<SessionRole, "ADMIN" | "INSTRUCTOR">;

export type WorkspaceNavigationKey =
  | "courseReview"
  | "courseAccess"
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
      { key: "courseReview", href: home },
      { key: "courseAccess", href: `/${locale}/admin/course-access` },
      { key: "staffOperations", href: "/staff" },
    ];
  }
  return [
    { key: "instructorStudio", href: home },
    { key: "courseBuilder", href: `${home}#course-builder` },
  ];
}
