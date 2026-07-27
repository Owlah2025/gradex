"use client";

import * as React from "react";
import { AuthShell } from "@/components/auth/auth-shell";
import { RecoveryRequestForm } from "@/components/auth/recovery-request-form";
import { useLocale } from "@/lib/i18n/locale-provider";

export default function RecoverPage() {
  const { t } = useLocale();
  return (
    <AuthShell title={t.auth.recover.title} intro={t.auth.recover.intro}>
      {/* RecoveryRequestForm reads query parameters, so it needs a Suspense
          boundary to stay statically renderable. */}
      <React.Suspense fallback={null}>
        <RecoveryRequestForm />
      </React.Suspense>
    </AuthShell>
  );
}
