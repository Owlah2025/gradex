import type { ReactNode } from "react";
import { RoleWorkspaceShell } from "@/components/layout/role-workspace-shell";

export default function AdminLayout({ children }: { children: ReactNode }) {
  return <RoleWorkspaceShell role="ADMIN">{children}</RoleWorkspaceShell>;
}
