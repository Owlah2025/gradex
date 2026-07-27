"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { VerificationConsumer } from "@/components/auth/verification-consumer";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function VerificationResultPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.result.title} intro={t.auth.result.intro}>
      {/* VerificationConsumer reads query parameters, so it needs a Suspense
          boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <VerificationConsumer />
      </React.Suspense>
    </AuthShell>
  );
}
