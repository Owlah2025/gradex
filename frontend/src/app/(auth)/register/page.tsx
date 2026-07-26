"use client";

import { AuthShell } from "@/components/auth/auth-shell";
import { RegistrationForm } from "@/components/auth/registration-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function RegisterPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.register.title} intro={t.auth.register.intro}>
      <RegistrationForm />
    </AuthShell>
  );
}
