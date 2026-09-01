"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { requestEmailVerification } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { validEmail } from "@/lib/identity/validation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { withReturnTo } from "@/lib/identity/return-to";
import {
  challengeParameter,
  rememberChallenge,
} from "@/lib/identity/verification-challenge";

export function VerificationRequestForm() {
  const { locale, t } = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [accepted, setAccepted] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const emailRef = React.useRef<HTMLInputElement>(null);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setAccepted(false);
    if (!validEmail(email)) {
      setError(t.auth.register.invalidEmail);
      emailRef.current?.focus();
      return;
    }
    setSubmitting(true);
    try {
      const accepted = await requestEmailVerification(email, locale);
      setEmail("");
      setAccepted(true);
      // The response carries a challenge whether or not the address was
      // eligible, which is what keeps this route from confirming that an
      // account exists. Continuing to the code screen is therefore the same
      // navigation in both cases; an ineligible address simply never matches.
      rememberChallenge(accepted.verification);
      const step = withReturnTo("/verify-email", searchParams.get("returnTo"));
      const separator = step.includes("?") ? "&" : "?";
      router.push(
        `${step}${separator}${challengeParameter}=${encodeURIComponent(
          accepted.verification.challenge_id,
        )}`,
      );
    } catch (caught) {
      if (caught instanceof ProblemError && caught.problem.code === "RATE_LIMITED") {
        setError(t.auth.verify.limited);
      } else if (
        caught instanceof ProblemError &&
        [
          "RATE_LIMITING_UNAVAILABLE",
          "TRANSACTIONAL_DELIVERY_UNAVAILABLE",
        ].includes(caught.problem.code)
      ) {
        setError(t.auth.verify.unavailable);
      } else {
        setError(t.auth.register.failed);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {accepted ? (
        <Alert tone="success" title={t.auth.verify.acceptedTitle}>
          {t.auth.verify.acceptedBody}
        </Alert>
      ) : null}
      {error ? <Alert tone="error" title={error} /> : null}
      <Field label={t.auth.verify.email} htmlFor="verification-email">
        <Input
          id="verification-email"
          ref={emailRef}
          type="email"
          inputMode="email"
          autoComplete="email"
          dir="ltr"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
      </Field>
      <Button type="submit" className="w-full" size="lg" disabled={submitting}>
        {submitting ? t.auth.verify.sending : t.auth.verify.send}
      </Button>
    </form>
  );
}
