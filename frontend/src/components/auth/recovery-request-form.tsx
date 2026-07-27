"use client";

import * as React from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { requestPasswordReset } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { validEmail } from "@/lib/identity/validation";
import { withReturnTo } from "@/lib/identity/return-to";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * Errors are held as keys so a locale switch re-resolves them; see the note in
 * recovery-reset-form.tsx.
 */
type RecoverErrorKey = "invalidEmail" | "limited" | "unavailable" | "failed";

/**
 * Asks for a password reset link.
 *
 * The accepted state is shown for every address the server accepts, which is
 * every syntactically valid one. It deliberately does not confirm that an
 * account exists — the server answers identically for unknown, unverified,
 * suspended, and eligible addresses, and narrowing that back down here would
 * hand back the enumeration oracle the backend just removed.
 */
export function RecoveryRequestForm() {
  const { locale, t } = useLocale();
  const searchParams = useSearchParams();
  const [email, setEmail] = React.useState("");
  const [error, setError] = React.useState<RecoverErrorKey | null>(null);
  const [accepted, setAccepted] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const emailRef = React.useRef<HTMLInputElement>(null);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setAccepted(false);
    if (!validEmail(email)) {
      setError("invalidEmail");
      emailRef.current?.focus();
      return;
    }
    setSubmitting(true);
    try {
      await requestPasswordReset(email, locale);
      setEmail("");
      setAccepted(true);
    } catch (caught) {
      if (caught instanceof ProblemError && caught.problem.code === "RATE_LIMITED") {
        setError("limited");
      } else if (
        caught instanceof ProblemError &&
        [
          "RATE_LIMITING_UNAVAILABLE",
          "TRANSACTIONAL_DELIVERY_UNAVAILABLE",
          "REGISTRATION_UNAVAILABLE",
        ].includes(caught.problem.code)
      ) {
        setError("unavailable");
      } else {
        setError("failed");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {accepted ? (
        <Alert tone="success" title={t.auth.recover.acceptedTitle}>
          {t.auth.recover.acceptedBody}
        </Alert>
      ) : null}
      {error ? <Alert tone="error" title={t.auth.recover[error]} /> : null}
      <Field label={t.auth.recover.email} htmlFor="recovery-email">
        <Input
          id="recovery-email"
          ref={emailRef}
          type="email"
          inputMode="email"
          autoComplete="email"
          dir="ltr"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
        />
      </Field>
      <Button className="w-full" size="lg" disabled={submitting}>
        {submitting ? t.auth.recover.sending : t.auth.recover.send}
      </Button>
      <p className="text-center text-sm">
        <Link
          className="underline"
          href={withReturnTo("/login", searchParams.get("returnTo"))}
        >
          {t.auth.recover.backToSignIn}
        </Link>
      </p>
    </form>
  );
}
