"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { OnboardingForm } from "@/components/auth/onboarding-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function OnboardPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.staff.onboardTitle} intro={t.auth.staff.onboardIntro}>
      <React.Suspense fallback={null}>
        <OnboardingForm />
      </React.Suspense>
    </AuthShell>
  );
}
