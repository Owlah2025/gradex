import type { ReactNode } from "react";
import { RoleWorkspaceShell } from "@/components/layout/role-workspace-shell";

export default function InstructorLayout({ children }: { children: ReactNode }) {
  return <RoleWorkspaceShell role="INSTRUCTOR">{children}</RoleWorkspaceShell>;
}
