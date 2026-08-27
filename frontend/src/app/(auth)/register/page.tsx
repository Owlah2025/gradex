"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { RegistrationForm } from "@/components/auth/registration-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function RegisterPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.register.title} intro={t.auth.register.intro} activeStep={0}>
      {/* RegistrationForm reads query parameters, so it needs a Suspense
          boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <RegistrationForm />
      </React.Suspense>
    </AuthShell>
  );
}
