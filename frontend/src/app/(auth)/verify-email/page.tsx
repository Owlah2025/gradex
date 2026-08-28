"use client";

import { AuthShell } from "@/components/auth/auth-shell";
import { VerificationRequestForm } from "@/components/auth/verification-request-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function VerifyEmailPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.verify.title} intro={t.auth.verify.intro} activeStep={1}>
      <VerificationRequestForm />
    </AuthShell>
  );
}
