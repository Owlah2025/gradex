"use client";

import * as React from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { requestEmailVerification } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { validEmail } from "@/lib/identity/validation";
import { useLocale } from "@/lib/i18n/locale-provider";

export function VerificationRequestForm() {
  const { locale, t } = useLocale();
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
      await requestEmailVerification(email, locale);
      setEmail("");
      setAccepted(true);
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
