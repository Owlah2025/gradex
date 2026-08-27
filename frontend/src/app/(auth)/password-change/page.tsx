"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { PasswordChangeForm } from "@/components/auth/password-change-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function PasswordChangePage() {
  const { t } = useLocale();
  return (
    <AuthShell
      title={t.auth.passwordChange.title}
      intro={t.auth.passwordChange.intro}
      audience="session"
    >
      {/* The form reads the returnTo query parameter, so it needs a Suspense
          boundary to stay statically renderable — the same shape the sign-in
          screen uses. */}
      <React.Suspense fallback={null}>
        <PasswordChangeForm />
      </React.Suspense>
    </AuthShell>
  );
}
