"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  resendEmailVerificationCode,
  verifyEmailCode,
  type VerificationChallenge,
} from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import {
  challengeParameter,
  forgetChallenge,
  recallChallenge,
  rememberChallenge,
  secondsUntil,
} from "@/lib/identity/verification-challenge";
import { postAuthenticationDestination, withReturnTo } from "@/lib/identity/return-to";
import { setSession } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

/** A code is six digits and nothing else, so the field refuses everything else as it is typed. */
const digitsOnly = /[^0-9]/g;

/**
 * The verification screen.
 *
 * It knows which challenge it is asking about from the address bar and the
 * non-secret context the registration step left behind, so it never asks for
 * the email address a second time. Proving the code signs the Student in and
 * continues to wherever they were going before admission interrupted them.
 */
export function VerificationCodeForm() {
  const { locale, t } = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();
  const returnTo = searchParams.get("returnTo");
  const challengeId = searchParams.get(challengeParameter);

  const [challenge, setChallenge] = React.useState<VerificationChallenge | null>(null);
  const [code, setCode] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const [resending, setResending] = React.useState(false);
  const [cooldown, setCooldown] = React.useState(0);
  // `submitting` is state and does not close the window between two submits
  // dispatched in the same render pass; this does. Every attempt costs one of
  // five, so a double submit is not merely wasteful here.
  const inFlight = React.useRef(false);
  const codeRef = React.useRef<HTMLInputElement>(null);

  // The context is read after mount rather than during render: sessionStorage
  // does not exist on the server, and reading it in render would make the two
  // passes disagree.
  React.useEffect(() => {
    setChallenge(recallChallenge(challengeId));
  }, [challengeId]);

  React.useEffect(() => {
    codeRef.current?.focus();
  }, []);

  // One interval for the countdown, driven by the server's own timestamp rather
  // than by counting down from a local guess.
  React.useEffect(() => {
    if (!challenge) return;
    const tick = () => setCooldown(secondsUntil(challenge.resend_available_at, Date.now()));
    tick();
    const timer = window.setInterval(tick, 1000);
    return () => window.clearInterval(timer);
  }, [challenge]);

  const activeChallengeId = challenge?.challenge_id ?? challengeId;

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (inFlight.current || !activeChallengeId) return;
    setError(null);
    setNotice(null);
    if (code.length !== 6) {
      setError(t.auth.code.malformed);
      codeRef.current?.focus();
      return;
    }
    inFlight.current = true;
    setSubmitting(true);
    try {
      const session = await verifyEmailCode(activeChallengeId, code, locale);
      // The challenge is spent. Dropping it here keeps a stale identifier from
      // sitting in this tab's storage after it stops meaning anything.
      forgetChallenge(activeChallengeId);
      setSession(session);
      router.replace(
        postAuthenticationDestination(
          session.role,
          returnTo,
          locale,
          session.password_change_required === true,
        ),
      );
    } catch (caught) {
      setCode("");
      codeRef.current?.focus();
      setError(verificationMessage(caught, t));
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }

  async function resend() {
    if (resending || !activeChallengeId) return;
    setError(null);
    setNotice(null);
    setResending(true);
    try {
      const accepted = await resendEmailVerificationCode(activeChallengeId, locale);
      // A resend carries no masked address: the caller supplied none, and
      // echoing the stored one would make an eligible resend distinguishable
      // from one on a stale handle. The screen keeps the mask it already has.
      const replacement = {
        ...accepted.verification,
        masked_email: accepted.verification.masked_email || (challenge?.masked_email ?? ""),
      };
      forgetChallenge(activeChallengeId);
      rememberChallenge(replacement);
      setChallenge(replacement);
      setCode("");
      // The identifier changed, so the address bar has to change with it or a
      // reload would ask about a challenge that is already superseded.
      const next = new URLSearchParams(searchParams.toString());
      next.set(challengeParameter, replacement.challenge_id);
      router.replace(`/verify-email?${next.toString()}`);
      setNotice(t.auth.code.resent);
      codeRef.current?.focus();
    } catch (caught) {
      setError(
        caught instanceof ProblemError &&
          caught.problem.code === "VERIFICATION_CODE_RESEND_TOO_SOON"
          ? t.auth.code.resendTooSoon
          : verificationMessage(caught, t),
      );
    } finally {
      setResending(false);
    }
  }

  if (!activeChallengeId) {
    return (
      <div className="space-y-5">
        <Alert tone="error" title={t.auth.code.noChallenge} />
        <Button asChild className="w-full" size="lg">
          <Link href={withReturnTo("/verify-email/request", returnTo)}>
            {t.auth.code.useEmailInstead}
          </Link>
        </Button>
      </div>
    );
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {notice ? <Alert tone="success" title={notice} /> : null}
      {error ? <Alert tone="error" title={error} /> : null}

      {challenge?.masked_email ? (
        <p className="text-sm leading-6 text-muted-foreground">
          {t.auth.code.sentTo}{" "}
          {/* The address is masked and reads left-to-right in both languages. */}
          <bdi dir="ltr" className="font-mono font-semibold text-foreground" data-testid="verification-masked-email">
            {challenge.masked_email}
          </bdi>
        </p>
      ) : null}

      <Field label={t.auth.code.label} htmlFor="verification-code" hint={t.auth.code.hint}>
        {/* One logical field rather than six boxes. Six independent inputs need
            a great deal of care to stay usable with a screen reader, a password
            manager, and platform code autofill; one input with an accessible
            name gets all three for free, and `one-time-code` is what lets the
            phone offer the code from the notification. */}
        <Input
          id="verification-code"
          ref={codeRef}
          type="text"
          inputMode="numeric"
          autoComplete="one-time-code"
          dir="ltr"
          maxLength={6}
          value={code}
          data-testid="verification-code-input"
          className="text-center font-mono text-2xl tracking-[0.5em]"
          aria-invalid={Boolean(error)}
          aria-describedby={error ? "verification-code-error" : "verification-code-hint"}
          onChange={(event) => setCode(event.target.value.replace(digitsOnly, "").slice(0, 6))}
          onPaste={(event) => {
            // Pasting "482 913" or "code: 482913" from a mail client is the
            // common case, so the digits are extracted rather than refused.
            const pasted = event.clipboardData.getData("text").replace(digitsOnly, "");
            if (!pasted) return;
            event.preventDefault();
            setCode(pasted.slice(0, 6));
          }}
        />
      </Field>

      <Button
        type="submit"
        className="w-full"
        size="lg"
        disabled={submitting || code.length !== 6}
        data-testid="verification-code-submit"
      >
        {submitting ? t.auth.code.submitting : t.auth.code.submit}
      </Button>

      <div className="text-center text-sm">
        {cooldown > 0 ? (
          <p className="text-muted-foreground" data-testid="verification-resend-countdown" aria-live="polite">
            {t.auth.code.resendIn.replace("{seconds}", String(cooldown))}
          </p>
        ) : (
          <Button
            type="button"
            variant="ghost"
            onClick={resend}
            disabled={resending}
            data-testid="verification-resend"
          >
            {resending ? t.auth.code.resending : t.auth.code.resend}
          </Button>
        )}
      </div>
    </form>
  );
}

/**
 * One message per outcome the Student can act on.
 *
 * Wrong, unknown, expired, and superseded all arrive as the same problem code
 * from the server on purpose, so they share one message here; there is nothing
 * more specific that could be said without turning the response into an oracle.
 */
function verificationMessage(
  caught: unknown,
  t: ReturnType<typeof useLocale>["t"],
): string {
  if (!(caught instanceof ProblemError)) return t.auth.code.unavailable;
  switch (caught.problem.code) {
    case "VERIFICATION_CODE_INVALID":
      return t.auth.code.invalid;
    case "VERIFICATION_CODE_EXHAUSTED":
      return t.auth.code.exhausted;
    case "VERIFICATION_CODE_RESEND_TOO_SOON":
      return t.auth.code.resendTooSoon;
    case "RATE_LIMITED":
      return t.auth.verify.limited;
    default:
      return t.auth.code.unavailable;
  }
}
