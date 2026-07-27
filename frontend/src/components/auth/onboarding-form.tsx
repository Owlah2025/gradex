"use client";

import * as React from "react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  captureTokenFromFragment,
  releaseFragmentToken,
  scrubTokenFragment,
  validDisplayName,
  validPassword,
} from "@/lib/identity/validation";

type OnboardState = "ready" | "submitting" | "done" | "error";

export function OnboardingForm() {
  const { t } = useLocale();
  const bearerRef = React.useRef<string>("");
  const [state, setState] = React.useState<OnboardState>("ready");
  const [displayName, setDisplayName] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [errorMessage, setErrorMessage] = React.useState<string | null>(null);

  React.useEffect(() => {
    const bearer = captureTokenFromFragment("STAFF_INVITATION");
    if (bearer) {
      bearerRef.current = bearer;
      scrubTokenFragment();
    }
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMessage(null);

    if (!validDisplayName(displayName)) {
      setErrorMessage(t.auth.register.invalidName);
      return;
    }
    if (!validPassword(password)) {
      setErrorMessage(t.auth.register.invalidPassword);
      return;
    }

    setState("submitting");
    try {
      const res = await fetch("/api/v1/staff/invitations/complete", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          bearer: bearerRef.current,
          display_name: displayName,
          password: password,
        }),
      });

      if (!res.ok) {
        throw new Error("Failed to complete onboarding");
      }

      releaseFragmentToken("STAFF_INVITATION");
      bearerRef.current = "";
      setState("done");
    } catch {
      setState("error");
      setErrorMessage(t.auth.register.failed);
    }
  };

  if (state === "done") {
    return (
      <div className="space-y-4">
        <Alert tone="success" title={t.auth.staff.onboardTitle}>
          <p className="mt-1 text-sm">{t.auth.staff.completeOnboarding}</p>
        </Alert>
        <div className="pt-2">
          <Link href="/login">
            <Button variant="accent" className="w-full">
              {t.nav.login}
            </Button>
          </Link>
        </div>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {errorMessage && <Alert tone="error" title={errorMessage} />}

      <Field htmlFor="onboard-display-name" label={t.auth.staff.displayName} hint={t.auth.register.displayHint}>
        <Input
          id="onboard-display-name"
          type="text"
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          required
          autoComplete="name"
          disabled={state === "submitting"}
        />
      </Field>

      <Field htmlFor="onboard-password" label={t.auth.staff.password} hint={t.auth.register.passwordHint}>
        <Input
          id="onboard-password"
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
          autoComplete="new-password"
          disabled={state === "submitting"}
        />
      </Field>

      <Button type="submit" variant="accent" className="w-full" disabled={state === "submitting"}>
        {state === "submitting" ? t.auth.staff.completingOnboarding : t.auth.staff.completeOnboarding}
      </Button>
    </form>
  );
}
