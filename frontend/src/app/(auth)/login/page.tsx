"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { LoginForm } from "@/components/auth/login-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function LoginPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.login.title} intro={t.auth.login.intro}>
      {/* LoginForm reads the reason and returnTo query parameters, so it needs
          a Suspense boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <LoginForm />
      </React.Suspense>
    </AuthShell>
  );
}
