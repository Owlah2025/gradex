"use client";

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
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
    setRequestError(null);
    if (!validate() || !policySet) return;
    setSubmitting(true);
    try {
      await registerStudent({
        display_name: fields.displayName,
        email: fields.email,
        password: fields.password,
        locale,
        policy_set_id: policySet.id,
      });
      setFields({ displayName: "", email: "", password: "" });
      // Carry the requested destination to the next admission step. It is
      // revalidated inside withReturnTo, so a hostile value is dropped here
      // rather than trusted because an earlier screen saw it.
      router.push(withReturnTo("/verify-email", searchParams.get("returnTo")));
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
      }
      setRequestError(t.auth.register.failed);
    } finally {
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
        hint={t.auth.register.passwordHint}
        error={errors.password}
      >
        <Input
          id="password"
          ref={refs.password}
          type="password"
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
          {t.auth.register.acceptPrefix}
        </legend>
        {!policySet && !policyError ? (
          <p className="text-sm text-muted-foreground">{t.auth.register.policiesLoading}</p>
        ) : null}
        {policyError ? (
          <Alert tone="error" title={t.auth.register.policiesUnavailable} />
        ) : null}
        <div className="space-y-3">
          {policySet?.policies.map((policy, index) => (
            <label key={policy.kind} className="flex cursor-pointer items-start gap-3 text-sm">
              <input
                ref={index === 0 ? refs.policy : undefined}
                type="checkbox"
                className="mt-1 size-4 accent-gx-blue-deep"
                checked={Boolean(accepted[policy.kind])}
                aria-invalid={Boolean(errors.policy)}
                onChange={(event) =>
                  setAccepted({ ...accepted, [policy.kind]: event.target.checked })
                }
              />
              <span>
                {t.auth.register.acceptPrefix}{" "}
                <a className="font-bold text-primary underline" href={policy.url}>
                  {policy.label}
                </a>
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

      <Button className="w-full" size="lg" disabled={submitting || !policySet || policyError}>
        {submitting ? t.auth.register.creating : t.auth.register.create}
      </Button>
    </form>
  );
}
