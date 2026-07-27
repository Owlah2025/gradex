"use client";

import * as React from "react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { consumeEmailVerification } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import {
  captureTokenFromFragment,
  releaseFragmentToken,
  scrubTokenFragment,
} from "@/lib/identity/validation";
import { useLocale } from "@/lib/i18n/locale-provider";

type VerificationState = "checking" | "success" | "invalid";

export function VerificationConsumer() {
  const { locale, t } = useLocale();
  const [state, setState] = React.useState<VerificationState>("checking");
  const started = React.useRef(false);

  React.useEffect(() => {
    if (started.current) return;
    started.current = true;
    // Capture is monotonic and document-scoped, so cleaning the address bar
    // cannot make an already-seen token look absent. Coupling the two was the
    // defect that let a successful scrub read back as a missing link.
    const token = captureTokenFromFragment("EMAIL_VERIFICATION");
    scrubTokenFragment();
    if (!token) {
      setState("invalid");
      return;
    }
    consumeEmailVerification(token, locale)
      .then(() => {
        // Terminal success: release the raw bearer.
        releaseFragmentToken("EMAIL_VERIFICATION");
        setState("success");
      })
      .catch((caught) => {
        // Release only on a definitive refusal. A transport failure or 5xx may
        // leave the secret live, and the holder must be able to retry with the
        // link they were sent.
        if (
          caught instanceof ProblemError &&
          caught.problem.code === "TOKEN_INVALID"
        ) {
          releaseFragmentToken("EMAIL_VERIFICATION");
        }
        setState("invalid");
      });
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
