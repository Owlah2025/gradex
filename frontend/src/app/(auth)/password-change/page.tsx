"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { PasswordChangeForm } from "@/components/auth/password-change-form";
import { useSessionView } from "@/lib/identity/use-session";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function PasswordChangePage() {
  const { t } = useLocale();
  const session = useSessionView();
  // The same screen serves two visitors: one the server will refuse everything
  // else until they finish, and one who came here to change their password
  // because they wanted to. It used to open by telling both of them that their
  // account could not continue without this.
  const required = session?.password_change_required === true;

  return (
    <AuthShell
      title={
        required
          ? t.auth.passwordChange.title
          : t.auth.passwordChange.voluntaryTitle
      }
      intro={
        required
          ? t.auth.passwordChange.intro
          : t.auth.passwordChange.voluntaryIntro
      }
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
