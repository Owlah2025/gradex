"use client";

import { AuthShell } from "@/components/auth/auth-shell";
import { VerificationConsumer } from "@/components/auth/verification-consumer";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function VerificationResultPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.result.title} intro={t.auth.result.intro}>
      <VerificationConsumer />
    </AuthShell>
  );
}
