"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import { Alert } from "@/components/ui/alert";
import {
  getRegistrationPolicySet,
  registerStudent,
  type RegistrationPolicySet,
} from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import {
  validDisplayName,
  validEmail,
  validPassword,
} from "@/lib/identity/validation";
import { withReturnTo } from "@/lib/identity/return-to";
import {
  challengeParameter,
  rememberChallenge,
} from "@/lib/identity/verification-challenge";
import { formatDate } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";

type FieldErrors = Partial<Record<"display_name" | "email" | "password" | "policy", string>>;
type FieldName = keyof FieldErrors;
type FieldRefs = Record<FieldName, React.RefObject<HTMLInputElement>>;

const fieldOrder: FieldName[] = ["display_name", "email", "password", "policy"];

function focusFirstError(errors: FieldErrors, refs: FieldRefs) {
  const first = fieldOrder.find((field) => errors[field]);
  if (first) refs[first].current?.focus();
}

export function RegistrationForm() {
  const { locale, t } = useLocale();
  const router = useRouter();
  const searchParams = useSearchParams();
  const [policySet, setPolicySet] = React.useState<RegistrationPolicySet | null>(null);
  const [policyError, setPolicyError] = React.useState(false);
  const [accepted, setAccepted] = React.useState<Record<string, boolean>>({});
  const [fields, setFields] = React.useState({ displayName: "", email: "", password: "" });
  const [errors, setErrors] = React.useState<FieldErrors>({});
  const [submitting, setSubmitting] = React.useState(false);
  const [requestError, setRequestError] = React.useState<string | null>(null);
  // See the note on the sign-in form: `submitting` is state and does not close
  // the window between two submits dispatched in the same render pass.
  const inFlight = React.useRef(false);
  const refs: FieldRefs = {
    display_name: React.useRef<HTMLInputElement>(null),
    email: React.useRef<HTMLInputElement>(null),
    password: React.useRef<HTMLInputElement>(null),
    policy: React.useRef<HTMLInputElement>(null),
  };

  React.useEffect(() => {
    let active = true;
    setPolicySet(null);
    setPolicyError(false);
    setAccepted({});
    getRegistrationPolicySet(locale)
      .then((next) => active && setPolicySet(next))
      .catch(() => active && setPolicyError(true));
    return () => {
      active = false;
    };
  }, [locale]);

  function validate() {
    const next: FieldErrors = {};
    if (!validDisplayName(fields.displayName)) next.display_name = t.auth.register.invalidName;
    if (!validEmail(fields.email)) next.email = t.auth.register.invalidEmail;
    if (!validPassword(fields.password)) next.password = t.auth.register.invalidPassword;
    if (!policySet || policySet.policies.some((policy) => !accepted[policy.kind])) {
      next.policy = t.auth.register.acceptPolicies;
    }
    setErrors(next);
    focusFirstError(next, refs);
    return Object.keys(next).length === 0;
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    if (inFlight.current) return;
    setRequestError(null);
    if (!validate() || !policySet) return;
    inFlight.current = true;
    setSubmitting(true);
    try {
      const accepted = await registerStudent({
        display_name: fields.displayName,
        email: fields.email,
        password: fields.password,
        locale,
        policy_set_id: policySet.id,
      });
      setFields({ displayName: "", email: "", password: "" });
      // The verification screen is opened knowing which challenge it is about,
      // so it never asks for the email address again. The identifier travels in
      // the URL because it must survive a reload and the back button; the rest
      // of the non-secret context travels beside it in session storage.
      rememberChallenge(accepted.verification);
      const step = withReturnTo("/verify-email", searchParams.get("returnTo"));
      const separator = step.includes("?") ? "&" : "?";
      router.push(
        `${step}${separator}${challengeParameter}=${encodeURIComponent(
          accepted.verification.challenge_id,
        )}`,
      );
    } catch (error) {
      if (error instanceof ProblemError && error.problem.errors?.length) {
        const backendErrors: FieldErrors = {};
        for (const violation of error.problem.errors) {
          const pointer = violation.pointer?.replace("#/", "");
          if (pointer === "display_name") {
            backendErrors.display_name = t.auth.register.invalidName;
          } else if (pointer === "email") {
            backendErrors.email = t.auth.register.invalidEmail;
          } else if (pointer === "password") {
            backendErrors.password = t.auth.register.invalidPassword;
          } else if (pointer === "policy_set_id") {
            backendErrors.policy = t.auth.register.acceptPolicies;
            setAccepted({});
            setPolicyError(false);
            getRegistrationPolicySet(locale)
              .then(setPolicySet)
              .catch(() => {
                setPolicySet(null);
                setPolicyError(true);
              });
          }
        }
        setErrors(backendErrors);
        focusFirstError(backendErrors, refs);
        // A generic banner on top of field messages that already name the
        // problem is two answers to one question, and the vaguer one is
        // louder. Only speak when nothing more precise was said.
        if (Object.keys(backendErrors).length > 0) return;
      }
      setRequestError(t.auth.register.failed);
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }

  return (
    <form className="space-y-5" onSubmit={submit} noValidate>
      {requestError ? <Alert tone="error" title={requestError} /> : null}
      <Field
        label={t.auth.register.displayName}
        htmlFor="display-name"
        hint={t.auth.register.displayHint}
        error={errors.display_name}
      >
        <Input
          id="display-name"
          ref={refs.display_name}
          autoComplete="name"
          value={fields.displayName}
          onChange={(event) => setFields({ ...fields, displayName: event.target.value })}
          aria-invalid={Boolean(errors.display_name)}
          aria-describedby={errors.display_name ? "display-name-error" : "display-name-hint"}
        />
      </Field>
      <Field label={t.auth.register.email} htmlFor="email" error={errors.email}>
        <Input
          id="email"
          ref={refs.email}
          type="email"
          inputMode="email"
          autoComplete="email"
          dir="ltr"
          value={fields.email}
          onChange={(event) => setFields({ ...fields, email: event.target.value })}
          aria-invalid={Boolean(errors.email)}
          aria-describedby={errors.email ? "email-error" : undefined}
        />
      </Field>
      <Field
        label={t.auth.register.password}
        htmlFor="password"
        hint={t.auth.common.passwordRule}
        error={errors.password}
      >
        <PasswordInput
          id="password"
          ref={refs.password}
          autoComplete="new-password"
          value={fields.password}
          onChange={(event) => setFields({ ...fields, password: event.target.value })}
          aria-invalid={Boolean(errors.password)}
          aria-describedby={errors.password ? "password-error" : "password-hint"}
        />
      </Field>

      <fieldset
        className="rounded-lg border bg-card p-4"
        aria-describedby={errors.policy ? "policy-error" : undefined}
      >
        <legend className="px-2 font-display text-sm font-bold">
          {t.auth.register.policiesLegend}
        </legend>
        {!policySet && !policyError ? (
          <p className="text-sm text-muted-foreground">{t.auth.register.policiesLoading}</p>
        ) : null}
        {policyError ? (
          <Alert tone="error" title={t.auth.register.policiesUnavailable} />
        ) : null}
        {policySet?.version ? (
          <p className="mb-3 text-xs text-muted-foreground" data-testid="registration-policy-version">
            {t.auth.register.policySetLabel} {policySet.version}
            {policySet.effective_date
              ? ` · ${t.auth.register.policyEffective} ${formatDate(policySet.effective_date, locale)}`
              : ""}
          </p>
        ) : null}
        <div className="space-y-3">
          {policySet?.policies.map((policy, index) => (
            <label key={policy.kind} className="flex cursor-pointer items-start gap-3 text-sm">
              <input
                ref={index === 0 ? refs.policy : undefined}
                type="checkbox"
                className="mt-1 size-4 accent-gx-blue-deep focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                checked={Boolean(accepted[policy.kind])}
                aria-invalid={Boolean(errors.policy)}
                onChange={(event) =>
                  setAccepted({ ...accepted, [policy.kind]: event.target.checked })
                }
              />
              <span>
                {t.auth.register.acceptPrefix}{" "}
                {/* A new tab, because this link sits mid-form. Following it in
                    place discarded a name, an email, a passphrase and every
                    box already ticked — to read the document the form is
                    asking the reader to agree to. */}
                <a
                  className="font-bold text-primary underline"
                  href={policy.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {policy.label}
                </a>
                <span className="sr-only"> ({t.auth.common.opensInNewTab})</span>
              </span>
            </label>
          ))}
        </div>
        {errors.policy ? (
          <p id="policy-error" className="mt-3 text-sm text-destructive">
            {errors.policy}
          </p>
        ) : null}
      </fieldset>

      <Button type="submit" className="w-full" size="lg" disabled={submitting || !policySet || policyError}>
        {submitting ? t.auth.register.creating : t.auth.register.create}
      </Button>
    </form>
  );
}
