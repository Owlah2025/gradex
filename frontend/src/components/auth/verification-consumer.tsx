"use client";

import * as React from "react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { consumeEmailVerification } from "@/lib/api/identity";
import { takeVerificationTokenFromFragment } from "@/lib/identity/validation";
import { useLocale } from "@/lib/i18n/locale-provider";

type VerificationState = "checking" | "success" | "invalid";

export function VerificationConsumer() {
  const { locale, t } = useLocale();
  const [state, setState] = React.useState<VerificationState>("checking");
  const started = React.useRef(false);

  React.useEffect(() => {
    if (started.current) return;
    started.current = true;
    const token = takeVerificationTokenFromFragment();
    if (!token) {
      setState("invalid");
      return;
    }
    consumeEmailVerification(token, locale)
      .then(() => setState("success"))
      .catch(() => setState("invalid"));
  }, [locale]);

  if (state === "checking") {
    return <Alert title={t.auth.result.checking} />;
  }
  if (state === "success") {
    return (
      <div className="space-y-5">
        <Alert tone="success" title={t.auth.result.successTitle}>
          {t.auth.result.successBody}
        </Alert>
        <Button asChild className="w-full" size="lg">
          <Link href="/login">{t.auth.result.login}</Link>
        </Button>
      </div>
    );
  }
  return (
    <div className="space-y-5">
      <Alert tone="error" title={t.auth.result.invalidTitle}>
        {t.auth.result.invalidBody}
      </Alert>
      <Button asChild className="w-full" size="lg">
        <Link href="/verify-email">{t.auth.result.requestNew}</Link>
      </Button>
    </div>
  );
}
