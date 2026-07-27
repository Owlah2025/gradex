"use client";

import * as React from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { completePasswordReset } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import {
  captureTokenFromFragment,
  isFragmentTokenSpent,
  releaseFragmentToken,
  scrubTokenFragment,
  validPassword,
} from "@/lib/identity/validation";
import { withReturnTo } from "@/lib/identity/return-to";
import { useLocale } from "@/lib/i18n/locale-provider";

type ResetState = "capturing" | "ready" | "missing" | "done";

/**
 * Errors are held as keys, not resolved strings.
 *
 * Storing the translated sentence would freeze it in whichever language was
 * active when it was raised, so switching locale left a stale-language error
 * on screen. Resolving at render keeps the message in the current language.
 */
type ResetErrorKey =
  | "weak"
  | "mismatch"
  | "invalidLink"
  | "limited"
  | "unavailable"
  | "failed";

/**
 * Consumes a reset link and sets a new password.
 *
 * Token capture and address-bar cleaning are deliberately separate. Capture is
 * monotonic and lives for the document, so cleaning the URL — or failing to —
 * can never change whether this screen believes it has a link. An earlier
 * version coupled them and a successful scrub made the form disappear, because
 * a remount re-read the fragment it had just emptied.
 *
 * The token is kept in a ref rather than state: it is a credential, not
 * something that should drive rendering, and it must never reach the DOM.
 *
 * Success shows no session and offers only sign-in, because the server
 * invalidates every family on reset and there is nothing to resume.
 */
export function RecoveryResetForm() {
  const { locale, t } = useLocale();
  const searchParams = useSearchParams();
  const [state, setState] = React.useState<ResetState>("capturing");
  const [password, setPassword] = React.useState("");
  const [confirmation, setConfirmation] = React.useState("");
  const [error, setError] = React.useState<ResetErrorKey | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const token = React.useRef<string | null>(null);
  const passwordRef = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    token.current = captureTokenFromFragment("PASSWORD_RESET");
    // Only ever resolve the initial state. A remount — from a locale change or
    // React's development double-mount — must not walk "done" back to a fresh
    // form, and must not walk a captured token back to "missing".
    setState((previous) => {
      if (previous !== "capturing") return previous;
      if (isFragmentTokenSpent("PASSWORD_RESET")) return "done";
      return token.current ? "ready" : "missing";
    });
    scrubTokenFragment();
  }, []);

  function clearEntry() {
    setPassword("");
    setConfirmation("");
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!validPassword(password)) {
      setError("weak");
      passwordRef.current?.focus();
      return;
    }
    if (password !== confirmation) {
      setError("mismatch");
      passwordRef.current?.focus();
      return;
    }
    const presented = token.current;
    if (!presented) {
      setState("missing");
      return;
    }
    setSubmitting(true);
    try {
      await completePasswordReset(presented, password, locale);
      // Terminal success: the secret is spent, so drop the raw bearer. A
      // remount then shows the completed state rather than offering the form
      // again for a link that no longer works.
      token.current = null;
      releaseFragmentToken("PASSWORD_RESET");
      clearEntry();
      setState("done");
    } catch (caught) {
      clearEntry();
      if (caught instanceof ProblemError && caught.problem.code === "TOKEN_INVALID") {
        // Terminal refusal: unknown, expired, already used, or superseded. The
        // link cannot become valid again, so the bearer is released here too.
        token.current = null;
        releaseFragmentToken("PASSWORD_RESET");
        setError("invalidLink");
      } else if (caught instanceof ProblemError && caught.problem.code === "RATE_LIMITED") {
        // Not terminal. The secret is untouched and the holder may retry.
        setError("limited");
      } else if (
        caught instanceof ProblemError &&
        caught.problem.code === "PASSWORD_POLICY"
      ) {
        // Not terminal. The server refused the new password before consuming
        // anything, so the same link must still work.
        setError("weak");
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
        // Network failure, timeout, abort, or 5xx. The server may or may not
        // have consumed the secret, so the bearer is deliberately kept: a
        // retry is the holder's only route forward if it did not.
        setError("failed");
      }
      passwordRef.current?.focus();
    } finally {
      setSubmitting(false);
    }
  }

  if (state === "capturing") {
    return <Alert title={t.auth.resetPassword.checking} />;
  }

  if (state === "missing") {
    return (
      <div className="space-y-5">
        <Alert tone="error" title={t.auth.resetPassword.missingToken} />
        <Button asChild className="w-full" size="lg">
          <Link href={withReturnTo("/recover", searchParams.get("returnTo"))}>
            {t.auth.resetPassword.requestNew}
          </Link>
        </Button>
      </div>
    );
  }

  if (state === "done") {
    return (
      <div className="space-y-5">
        <Alert tone="success" title={t.auth.resetPassword.successTitle}>
          {t.auth.resetPassword.successBody}
        </Alert>
        <Button asChild className="w-full" size="lg">
          <Link href={withReturnTo("/login", searchParams.get("returnTo"))}>
            {t.auth.resetPassword.goToSignIn}
          </Link>
        </Button>
      </div>
    );
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {error ? <Alert tone="error" title={t.auth.resetPassword[error]} /> : null}
      <Field label={t.auth.resetPassword.password} htmlFor="reset-password">
        <Input
          id="reset-password"
          ref={passwordRef}
          type="password"
          autoComplete="new-password"
          dir="ltr"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
        />
      </Field>
      <Field label={t.auth.resetPassword.confirm} htmlFor="reset-password-confirm">
        <Input
          id="reset-password-confirm"
          type="password"
          autoComplete="new-password"
          dir="ltr"
          value={confirmation}
          onChange={(event) => setConfirmation(event.target.value)}
        />
      </Field>
      <Button className="w-full" size="lg" disabled={submitting}>
        {submitting ? t.auth.resetPassword.submitting : t.auth.resetPassword.submit}
      </Button>
    </form>
  );
}
