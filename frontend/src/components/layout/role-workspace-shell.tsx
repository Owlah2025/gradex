"use client";

import type { ReactNode } from "react";
import Link from "next/link";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Footer } from "./footer";
import { Navbar } from "./navbar";
import {
  roleWorkspaceNavigation,
  type WorkspaceNavigationKey,
  type WorkspaceRole,
} from "./role-workspace-navigation";

export function RoleWorkspaceShell({
  role,
  children,
}: {
  role: WorkspaceRole;
  children: ReactNode;
}) {
  const { locale, t } = useLocale();
  const labels: Record<WorkspaceNavigationKey, string> = {
    courseReview: t.nav.courseReview,
    academicCatalog: t.nav.academicCatalog,
    courseAccess: t.nav.courseAccess,
    courseLifecycle: t.nav.courseLifecycle,
    reportedContent: t.nav.reportedContent,
    staffOperations: t.nav.staffOperations,
    instructorStudio: t.nav.instructorStudio,
    courseBuilder: t.nav.courseBuilder,
  };

  return (
    <>
      <Navbar />
      <div className="border-b border-border bg-muted/45">
        <nav
          aria-label={t.nav.workspaceNavigation}
          className="mx-auto flex max-w-container flex-wrap gap-2 px-5 py-3 sm:px-6"
        >
          {roleWorkspaceNavigation(role, locale).map((entry) => (
            <Link
              key={entry.key}
              href={entry.href}
              className="rounded-md px-3 py-2 text-sm font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              {labels[entry.key]}
            </Link>
          ))}
        </nav>
      </div>
      {children}
      <Footer />
    </>
  );
}
