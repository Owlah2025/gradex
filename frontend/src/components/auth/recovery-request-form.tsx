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

function classifyFailure(caught: unknown): RecoverErrorKey {
  if (caught instanceof ProblemError && caught.problem.code === "RATE_LIMITED") {
    return "limited";
  }
  if (
    caught instanceof ProblemError &&
    [
      "RATE_LIMITING_UNAVAILABLE",
      "TRANSACTIONAL_DELIVERY_UNAVAILABLE",
      "REGISTRATION_UNAVAILABLE",
    ].includes(caught.problem.code)
  ) {
    return "unavailable";
  }
  return "failed";
}

/**
 * Asks for a password reset link.
 *
 * The accepted state is shown for every address the server accepts, which is
 * every syntactically valid one. It deliberately does not confirm that an
 * account exists — the server answers identically for unknown, unverified,
 * suspended, and eligible addresses, and narrowing that back down here would
 * hand back the enumeration oracle the backend just removed.
 *
 * Once the server has accepted the request, the form is *replaced* rather than
 * annotated. What it replaces kept the filled field and a live "Send reset
 * link" underneath a success banner, so the screen said both "this is done" and
 * "do this" at once; the reported behaviour was a reader who could not tell
 * whether pressing the button had achieved anything, and pressed it again.
 * There is exactly one thing to do afterwards — open the email — and one
 * genuine reason to come back to this screen, which is that nothing arrived.
 * That reason gets its own control.
 *
 * Resend re-issues the same request for the address already accepted, which is
 * the only safe shape available: it is the identical call, subject to the same
 * server-side rate limit, and it neither remembers nor reveals anything new.
 * The address is held in component state purely so the reader does not have to
 * retype it; a refusal of the resend is reported as a refusal and never
 * upgrades or downgrades the accepted state, because the first request the
 * server took is still live.
 */
export function RecoveryRequestForm() {
  const { locale, t } = useLocale();
  const searchParams = useSearchParams();
  const [email, setEmail] = React.useState("");
  const [error, setError] = React.useState<RecoverErrorKey | null>(null);
  /**
   * The address the server accepted, or null while none has been.
   *
   * This doubles as the "accepted" flag on purpose: a separate boolean could
   * drift out of step with the value resend depends on, and an accepted screen
   * with no address to resend to is not a state this component should be able
   * to reach.
   */
  const [acceptedEmail, setAcceptedEmail] = React.useState<string | null>(null);
  const [resent, setResent] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const emailRef = React.useRef<HTMLInputElement>(null);
  const acceptedRef = React.useRef<HTMLDivElement>(null);

  // Focus moves to the accepted panel when it replaces the form. Without this a
  // keyboard reader is left focused on a control that no longer exists and is
  // returned to the top of the document, which reads as the page having
  // reloaded and lost the step.
  React.useEffect(() => {
    if (!acceptedEmail) return;
    acceptedRef.current?.focus();
  }, [acceptedEmail]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!validEmail(email)) {
      setError("invalidEmail");
      emailRef.current?.focus();
      return;
    }
    const requested = email.trim();
    setSubmitting(true);
    try {
      await requestPasswordReset(requested, locale);
      // Only after the server has taken it. A transport failure, a 5xx, or a
      // rate-limit refusal must never reach this line.
      setEmail("");
      setAcceptedEmail(requested);
    } catch (caught) {
      setError(classifyFailure(caught));
    } finally {
      setSubmitting(false);
    }
  }

  async function resend() {
    if (!acceptedEmail || submitting) return;
    setError(null);
    setResent(false);
    setSubmitting(true);
    try {
      await requestPasswordReset(acceptedEmail, locale);
      setResent(true);
    } catch (caught) {
      // The accepted state is left standing: the earlier request the server
      // accepted has not been withdrawn by this one failing.
      setError(classifyFailure(caught));
    } finally {
      setSubmitting(false);
    }
  }

  const backToSignIn = (
    <p className="text-center text-sm">
      <Link
        className="underline"
        href={withReturnTo("/login", searchParams.get("returnTo"))}
      >
        {t.auth.recover.backToSignIn}
      </Link>
    </p>
  );

  if (acceptedEmail) {
    return (
      <div className="space-y-5" data-testid="recovery-accepted">
        <div ref={acceptedRef} tabIndex={-1} className="focus-visible:outline-none">
          <Alert tone="success" title={t.auth.recover.acceptedTitle}>
            {/*
              Two sentences that are equally true whether or not an account
              exists behind the address. Neither names the address back: the
              reader typed it a moment ago, and printing it here would be one
              more thing on a screen whose job is now to say "go and look".
            */}
            <p>{t.auth.recover.acceptedBody}</p>
            <p className="mt-2">{t.auth.recover.acceptedNext}</p>
          </Alert>
        </div>
        {error ? <Alert tone="error" title={t.auth.recover[error]} /> : null}
        {resent && !error ? (
          <Alert tone="info" title={t.auth.recover.resent} />
        ) : null}
        <Button
          type="button"
          variant="outline"
          className="w-full"
          size="lg"
          disabled={submitting}
          onClick={() => void resend()}
          data-testid="recovery-resend"
        >
          {submitting ? t.auth.recover.resending : t.auth.recover.resend}
        </Button>
        {backToSignIn}
      </div>
    );
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate data-testid="recovery-request">
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
      <Button
        type="submit"
        className="w-full"
        size="lg"
        disabled={submitting}
        data-testid="recovery-send"
      >
        {submitting ? t.auth.recover.sending : t.auth.recover.send}
      </Button>
      {backToSignIn}
    </form>
  );
}
