"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { VerificationCodeForm } from "@/components/auth/verification-code-form";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * The verification step.
 *
 * It opens automatically after registration and already knows which challenge
 * it is asking about, so it prompts for the code rather than for the email
 * address the Student typed one screen ago. Requesting a fresh challenge by
 * address lives at `/verify-email/request`, which is where this screen sends a
 * visitor who arrives without one.
 */
export default function VerifyEmailPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.code.title} intro={t.auth.code.intro} activeStep={1}>
      {/* VerificationCodeForm reads query parameters, so it needs a Suspense
          boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <VerificationCodeForm />
      </React.Suspense>
    </AuthShell>
  );
}
