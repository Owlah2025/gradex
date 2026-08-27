"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { PasswordInput } from "@/components/ui/password-input";
import { changePassword } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { passwordMaximum, passwordMinimum, validPassword } from "@/lib/identity/validation";
import { postPasswordChangeDestination, roleRoot } from "@/lib/identity/return-to";
import { currentCSRFToken, setSession } from "@/lib/identity/session";
import { useSessionView } from "@/lib/identity/use-session";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * Errors are held as keys rather than resolved sentences, so switching language
 * re-renders the message instead of freezing it in whichever locale raised it.
 * The recovery screen holds them the same way.
 */
type ChangeErrorKey =
  | "weak"
  | "mismatch"
  | "sameAsCurrent"
  | "wrongCurrent"
  | "rejected"
  | "reauthenticate"
  | "signedOut"
  | "limited"
  | "failed";

/**
 * The mandatory password change.
 *
 * The bootstrap Administrator is created with a CHANGE_REQUIRED credential and
 * the bootstrap command tells the operator the first sign-in must change it.
 * Until this screen existed there was no way to: the account authenticated, and
 * then every privileged request was refused with no route to the state the
 * command described. This is that route.
 *
 * A successful change rotates the session server-side. The response is
 * therefore not an acknowledgement but the replacement session, and it is
 * installed before navigating — a browser that kept the old in-memory CSRF
 * token would be signed out on its very next state-changing request.
 */
export function PasswordChangeForm() {
  const { locale, t } = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();
  const session = useSessionView();
  const [fields, setFields] = React.useState({
    current: "",
    next: "",
    confirmation: "",
  });
  const [error, setError] = React.useState<ChangeErrorKey | null>(null);
  /**
   * Which field a local rule rejected, when the message is about one field.
   *
   * "Both new password fields must match" is a fact about the confirmation box,
   * and "choose a longer password" is a fact about the new-password box. Both
   * were announced in a banner at the top of the form, leaving the control that
   * was actually wrong unmarked and undescribed.
   */
  const [errorField, setErrorField] =
    React.useState<"next" | "confirmation" | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const currentRef = React.useRef<HTMLInputElement>(null);
  const nextRef = React.useRef<HTMLInputElement>(null);
  const confirmationRef = React.useRef<HTMLInputElement>(null);

  function clearEntry() {
    setFields({ current: "", next: "", confirmation: "" });
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    setErrorField(null);

    if (!validPassword(fields.next)) {
      setError("weak");
      setErrorField("next");
      nextRef.current?.focus();
      return;
    }
    if (fields.next !== fields.confirmation) {
      setError("mismatch");
      setErrorField("confirmation");
      confirmationRef.current?.focus();
      return;
    }
    // Checked here as well as on the server. The server refuses reuse
    // regardless — re-entering the temporary password would otherwise satisfy
    // the workflow while changing nothing — but catching it locally spends no
    // Argon2id verification and gives a precise message.
    if (fields.next === fields.current) {
      setError("sameAsCurrent");
      setErrorField("next");
      nextRef.current?.focus();
      return;
    }

    const csrf = currentCSRFToken();
    if (!csrf) {
      // No in-memory session: this document was opened without one, or the
      // rehydrating read failed. Nothing here can proceed without it.
      setError("signedOut");
      return;
    }

    setSubmitting(true);
    try {
      const rotated = await changePassword(
        fields.current,
        fields.next,
        csrf,
        locale,
      );
      clearEntry();
      // The replacement session, including its new CSRF token, before any
      // navigation. The credential this form authenticated with is superseded.
      setSession(rotated);
      router.push(
        postPasswordChangeDestination(
          rotated.role,
          searchParams.get("returnTo"),
          locale,
        ),
      );
    } catch (caught) {
      // The current password is cleared on every failure, the new one is kept:
      // a mistyped current password should not cost the visitor the passphrase
      // they just composed, and a refused new password is the one thing they
      // need to edit.
      setFields((previous) => ({ ...previous, current: "" }));
      // A server refusal is about the request, not about one box.
      setErrorField(null);
      setError(errorKeyFor(caught));
      currentRef.current?.focus();
    } finally {
      setSubmitting(false);
    }
  }

  const required = session?.password_change_required === true;
  // Somewhere to go when the change is optional. A restricted principal is
  // deliberately given none: leaving is the thing the server refuses.
  const escape = session ? roleRoot(session.role, locale) : null;

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {/* Only when a change actually is required. A signed-in visitor who came
          here deliberately used to be told their account could not continue
          without one, which was simply not true of them. */}
      {required ? (
        <Alert tone="info" title={t.auth.passwordChange.requiredTitle}>
          {t.auth.passwordChange.requiredBody}
        </Alert>
      ) : null}
      {/* The shared Alert takes only its own props, so the test handle lives on
          a wrapper rather than widening that component's contract. */}
      {error && errorField === null ? (
        <div data-testid="password-change-error">
          <Alert tone="error" title={t.auth.passwordChange[error]} />
        </div>
      ) : null}

      <Field
        label={t.auth.passwordChange.current}
        htmlFor="password-change-current"
      >
        <PasswordInput
          id="password-change-current"
          data-testid="password-change-current"
          ref={currentRef}
          autoComplete="current-password"
          maxLength={passwordMaximum}
          value={fields.current}
          onChange={(event) =>
            setFields({ ...fields, current: event.target.value })
          }
        />
      </Field>

      <Field
        label={t.auth.passwordChange.next}
        htmlFor="password-change-new"
        hint={t.auth.common.passwordRule}
        error={errorField === "next" && error ? t.auth.passwordChange[error] : undefined}
      >
        <PasswordInput
          id="password-change-new"
          data-testid="password-change-new"
          ref={nextRef}
          autoComplete="new-password"
          minLength={passwordMinimum}
          maxLength={passwordMaximum}
          value={fields.next}
          onChange={(event) =>
            setFields({ ...fields, next: event.target.value })
          }
          aria-invalid={errorField === "next"}
          aria-describedby={
            errorField === "next"
              ? "password-change-new-error"
              : "password-change-new-hint"
          }
        />
      </Field>

      <Field
        label={t.auth.passwordChange.confirm}
        htmlFor="password-change-confirm"
        error={
          errorField === "confirmation" && error
            ? t.auth.passwordChange[error]
            : undefined
        }
      >
        <PasswordInput
          id="password-change-confirm"
          data-testid="password-change-confirm"
          ref={confirmationRef}
          autoComplete="new-password"
          maxLength={passwordMaximum}
          value={fields.confirmation}
          onChange={(event) =>
            setFields({ ...fields, confirmation: event.target.value })
          }
          aria-invalid={errorField === "confirmation"}
          aria-describedby={
            errorField === "confirmation"
              ? "password-change-confirm-error"
              : undefined
          }
        />
      </Field>

      <Button
        type="submit"
        className="w-full"
        size="lg"
        disabled={submitting}
        data-testid="password-change-submit"
      >
        {submitting
          ? t.auth.passwordChange.submitting
          : t.auth.passwordChange.submit}
      </Button>

      {!required && escape ? (
        <p className="text-center text-sm">
          <Link className="underline" href={escape}>
            {t.auth.passwordChange.cancel}
          </Link>
        </p>
      ) : null}

      {session ? (
        <p className="text-center text-sm text-muted-foreground">
          {t.auth.passwordChange.signedInAs} {session.display_name}
        </p>
      ) : null}
    </form>
  );
}

/**
 * Maps the server's refusal onto a message.
 *
 * The two the visitor can act on are kept apart: a wrong current password and a
 * refused new one. Everything else collapses, because the server deliberately
 * does not distinguish it and neither should this screen.
 */
function errorKeyFor(caught: unknown): ChangeErrorKey {
  if (!(caught instanceof ProblemError)) return "failed";
  switch (caught.problem.code) {
    case "AUTHENTICATION_FAILED":
      return "wrongCurrent";
    case "VALIDATION_FAILED":
      // Too short, too long, a known-breached value, or the current password
      // again. The server does not say which rule matched and the message does
      // not guess.
      return "rejected";
    case "NOT_AUTHORIZED":
      // The session is genuine but authenticated too long ago for a credential
      // change. Signing in again is the recovery.
      return "reauthenticate";
    case "AUTHENTICATION_REQUIRED":
    case "SESSION_REPLACED":
    case "SESSION_REUSE_DETECTED":
    case "CSRF_FAILED":
      return "signedOut";
    case "RATE_LIMITED":
      return "limited";
    default:
      return "failed";
  }
}
