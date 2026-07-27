"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { createSession } from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { validEmail } from "@/lib/identity/validation";
import { postLoginDestination } from "@/lib/identity/return-to";
import { setSession } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

type FieldErrors = Partial<Record<"email" | "password", string>>;

const fieldOrder: Array<keyof FieldErrors> = ["email", "password"];

export function LoginForm() {
  const { locale, t } = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();
  const [fields, setFields] = React.useState({ email: "", password: "" });
  const [errors, setErrors] = React.useState<FieldErrors>({});
  const [submitting, setSubmitting] = React.useState(false);
  const [requestError, setRequestError] = React.useState<string | null>(null);
  const refs = {
    email: React.useRef<HTMLInputElement>(null),
    password: React.useRef<HTMLInputElement>(null),
  };

  // The reason a visitor was sent here — an expired, replaced, or revoked
  // session — is a state note, not a sign-in failure, so it renders separately
  // from the generic authentication message.
  const reason = searchParams.get("reason");
  const notice =
    reason === "expired"
      ? { title: t.auth.session.expiredTitle, body: t.auth.session.expiredBody }
      : reason === "replaced"
        ? {
            title: t.auth.session.replacedTitle,
            body: t.auth.session.replacedBody,
          }
        : reason === "reuse"
          ? { title: t.auth.session.reuseTitle, body: t.auth.session.reuseBody }
          : reason === "signed-out"
            ? {
                title: t.auth.session.signedOutTitle,
                body: t.auth.session.signedOutBody,
              }
            : null;

  function validate() {
    const next: FieldErrors = {};
    if (!validEmail(fields.email)) next.email = t.auth.login.invalidEmail;
    if (fields.password.length === 0) {
      next.password = t.auth.login.invalidPassword;
    }
    setErrors(next);
    const first = fieldOrder.find((field) => next[field]);
    if (first) refs[first].current?.focus();
    return Object.keys(next).length === 0;
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setRequestError(null);
    if (!validate()) return;
    setSubmitting(true);
    try {
      const session = await createSession(
        fields.email,
        fields.password,
        locale,
      );
      // Clear the password from component state before navigating so it does
      // not sit in memory behind the next screen.
      setFields({ email: fields.email, password: "" });
      setSession(session);
      router.push(
        postLoginDestination(session.role, searchParams.get("returnTo")),
      );
    } catch (error) {
      // Every hidden Account state shares one message. Only rate limiting and
      // fail-closed unavailability are distinguishable, and neither reveals
      // whether the Account exists.
      if (error instanceof ProblemError) {
        const { code } = error.problem;
        setRequestError(
          code === "RATE_LIMITED"
            ? t.auth.login.limited
            : code === "AUTHENTICATION_UNAVAILABLE"
              ? t.auth.login.unavailable
              : t.auth.login.failed,
        );
      } else {
        setRequestError(t.auth.login.failed);
      }
      setFields({ email: fields.email, password: "" });
      refs.password.current?.focus();
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {notice ? (
        <Alert tone="info" title={notice.title}>
          {notice.body}
        </Alert>
      ) : null}
      {requestError ? <Alert tone="error" title={requestError} /> : null}

      <Field label={t.auth.login.email} htmlFor="email" error={errors.email}>
        <Input
          id="email"
          ref={refs.email}
          type="email"
          inputMode="email"
          autoComplete="email"
          dir="ltr"
          value={fields.email}
          onChange={(event) =>
            setFields({ ...fields, email: event.target.value })
          }
          aria-invalid={Boolean(errors.email)}
          aria-describedby={errors.email ? "email-error" : undefined}
        />
      </Field>

      <Field
        label={t.auth.login.password}
        htmlFor="password"
        error={errors.password}
      >
        <Input
          id="password"
          ref={refs.password}
          type="password"
          autoComplete="current-password"
          value={fields.password}
          onChange={(event) =>
            setFields({ ...fields, password: event.target.value })
          }
          aria-invalid={Boolean(errors.password)}
          aria-describedby={errors.password ? "password-error" : undefined}
        />
      </Field>

      <Button className="w-full" size="lg" disabled={submitting}>
        {submitting ? t.auth.login.signingIn : t.auth.login.signIn}
      </Button>

      <p className="text-center text-sm text-muted-foreground">
        {t.auth.login.noAccount}{" "}
        <Link className="font-bold text-primary underline" href="/register">
          {t.auth.login.createAccount}
        </Link>
      </p>
    </form>
  );
}
