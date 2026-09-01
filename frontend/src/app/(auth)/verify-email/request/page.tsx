"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { VerificationRequestForm } from "@/components/auth/verification-request-form";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * The address-addressed recovery step.
 *
 * A Student who closed the verification tab no longer holds a challenge, and
 * this is the only screen that asks for the email address again — because at
 * that point it is genuinely the only thing they can supply. The ordinary
 * journey never reaches it.
 */
export default function VerifyEmailRequestPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.verify.title} intro={t.auth.verify.intro} activeStep={1}>
      {/* VerificationRequestForm reads query parameters, so it needs a
          Suspense boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <VerificationRequestForm />
      </React.Suspense>
    </AuthShell>
  );
}
