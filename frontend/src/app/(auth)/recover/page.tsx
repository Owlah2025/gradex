"use client";

import { AuthShell } from "@/components/auth/auth-shell";
import { RecoveryRequestForm } from "@/components/auth/recovery-request-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function RecoverPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.recover.title} intro={t.auth.recover.intro}>
      <RecoveryRequestForm />
    </AuthShell>
  );
}
