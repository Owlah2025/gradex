"use client";

import { AuthShell } from "@/components/auth/auth-shell";
import { RecoveryResetForm } from "@/components/auth/recovery-reset-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function ResetPasswordPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.resetPassword.title} intro={t.auth.resetPassword.intro}>
      <RecoveryResetForm />
    </AuthShell>
  );
}
