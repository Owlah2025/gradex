"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { RecoveryResetForm } from "@/components/auth/recovery-reset-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function ResetPasswordPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.resetPassword.title} intro={t.auth.resetPassword.intro}>
      {/* RecoveryResetForm reads query parameters, so it needs a Suspense
          boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <RecoveryResetForm />
      </React.Suspense>
    </AuthShell>
  );
}
