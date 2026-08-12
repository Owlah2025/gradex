"use client";

import * as React from "react";
import { StaffManagement } from "@/components/staff/staff-management";
import { RoleWorkspaceShell } from "@/components/layout/role-workspace-shell";

export default function StaffPage() {
  return (
    <RoleWorkspaceShell role="ADMIN">
      <main id="main" className="container mx-auto py-8 px-4">
        <StaffManagement />
      </main>
    </RoleWorkspaceShell>
  );
}
