"use client";

import * as React from "react";
import { StaffManagement } from "@/components/staff/staff-management";
import { RoleWorkspaceShell } from "@/components/layout/role-workspace-shell";

export default function StaffPage() {
  return (
    <RoleWorkspaceShell role="ADMIN">
      {/* The workspace owns its own width, gutters and direction, the same as every other
          operational screen. A second container here was a fifth content measure. */}
      <main id="main">
        <StaffManagement />
      </main>
    </RoleWorkspaceShell>
  );
}
