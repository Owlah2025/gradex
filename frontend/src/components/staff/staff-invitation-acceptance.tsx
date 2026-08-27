"use client";

import * as React from "react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import { AuthShell } from "@/components/auth/auth-shell";
import {
  completeStaffInvitation,
  previewStaffInvitation,
  type StaffInvitationState,
} from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  captureTokenFromFragment,
  passwordMaximum,
  passwordMinimum,
  releaseFragmentToken,
  scrubTokenFragment,
  validDisplayName,
  validPassword,
} from "@/lib/identity/validation";

/**
 * What this screen is showing right now.
 *
 * The four refusals are separate members rather than one `invalid`, because the
 * preview route answers with a state and each of those states has a different
 * next action for the reader: sign in, ask for a new invitation, contact the
 * administrator, or open a more recent email. One collapsed message made the
 * reader guess which of the four was theirs.
 *
 * `missing` is not a refusal — nothing was presented. Someone typed the address
 * or followed a link that lost its fragment, and saying "invalid or expired"
 * about an invitation nobody has seen is simply untrue.
 */
type Screen =
  | { kind: "checking" }
  | { kind: "ready"; role: "INSTRUCTOR" | "ADMIN" }
  | { kind: "refused"; state: Exclude<StaffInvitationState, "PENDING"> }
  | { kind: "missing" }
  | { kind: "unavailable" }
  | { kind: "done" };

type FieldErrors = Partial<Record<"name" | "password" | "confirm", string>>;

export function StaffInvitationAcceptance() {
  const { locale, t } = useLocale();
  const text = t.auth.staffInvitation;
  const [screen, setScreen] = React.useState<Screen>({ kind: "checking" });
  const [displayName, setDisplayName] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [confirmation, setConfirmation] = React.useState("");
  const [errors, setErrors] = React.useState<FieldErrors>({});
  const [requestError, setRequestError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const bearer = React.useRef<string | null>(null);
  const started = React.useRef(false);
  const nameRef = React.useRef<HTMLInputElement>(null);
  const passwordRef = React.useRef<HTMLInputElement>(null);
  const confirmRef = React.useRef<HTMLInputElement>(null);

  React.useEffect(() => {
    if (started.current) return;
    started.current = true;
    bearer.current = captureTokenFromFragment("STAFF_INVITATION");
    scrubTokenFragment();
    if (!bearer.current) {
      setScreen({ kind: "missing" });
      return;
    }
    previewStaffInvitation(bearer.current, "en")
      .then((preview) => {
        // Only a still-open invitation gets a form. The preview route answers 200 for an
        // invitation that is consumed, revoked, superseded, or expired, and offering the form
        // anyway asked the invitee to choose a password before telling them the link was
        // already used — the refusal came from the server either way, but only after the work.
        if (preview.state !== "PENDING") {
          bearer.current = null;
          releaseFragmentToken("STAFF_INVITATION");
          setScreen({ kind: "refused", state: preview.state });
          return;
        }
        setScreen({ kind: "ready", role: preview.invited_role });
      })
      .catch((caught) => {
        if (caught instanceof ProblemError && caught.problem.code === "TOKEN_INVALID") {
          bearer.current = null;
          releaseFragmentToken("STAFF_INVITATION");
          // The server refused the credential itself, so it names no state.
          // "Already used" is the honest floor here: it is the only refusal a
          // real invitee is likely to be holding, and it points at signing in.
          setScreen({ kind: "refused", state: "CONSUMED" });
          return;
        }
        setScreen({ kind: "unavailable" });
      });
    // The preview is read once per document and its only payload is a role and a
    // state, neither of which is prose. Reading it in `en` keeps a language
    // switch from re-presenting a one-time credential.
  }, []);

  function validate(): boolean {
    const next: FieldErrors = {};
    if (!validDisplayName(displayName)) next.name = text.invalidName;
    if (!validPassword(password)) next.password = text.invalidPassword;
    else if (password !== confirmation) next.confirm = text.mismatch;
    setErrors(next);
    if (next.name) nameRef.current?.focus();
    else if (next.password) passwordRef.current?.focus();
    else if (next.confirm) confirmRef.current?.focus();
    return Object.keys(next).length === 0;
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setRequestError(null);
    if (!validate()) return;
    if (!bearer.current) {
      setScreen({ kind: "missing" });
      return;
    }
    setSubmitting(true);
    try {
      await completeStaffInvitation(
        bearer.current,
        displayName,
        password,
        locale,
      );
      bearer.current = null;
      releaseFragmentToken("STAFF_INVITATION");
      setPassword("");
      setConfirmation("");
      setScreen({ kind: "done" });
    } catch (caught) {
      if (caught instanceof ProblemError && caught.problem.code === "TOKEN_INVALID") {
        bearer.current = null;
        releaseFragmentToken("STAFF_INVITATION");
        setScreen({ kind: "refused", state: "CONSUMED" });
      } else {
        setRequestError(text.failed);
      }
      setPassword("");
      setConfirmation("");
    } finally {
      setSubmitting(false);
    }
  }

  const refusal: Record<
    Exclude<StaffInvitationState, "PENDING">,
    { title: string; body: string; signIn: boolean }
  > = {
    CONSUMED: { title: text.consumedTitle, body: text.consumedBody, signIn: true },
    EXPIRED: { title: text.expiredTitle, body: text.expiredBody, signIn: false },
    REVOKED: { title: text.revokedTitle, body: text.revokedBody, signIn: false },
    SUPERSEDED: {
      title: text.supersededTitle,
      body: text.supersededBody,
      signIn: false,
    },
  };

  return (
    <AuthShell
      title={text.title}
      intro={text.intro}
      audience="staff"
      activeStep={1}
    >
      {screen.kind === "checking" ? (
        <Alert title={text.checking} />
      ) : null}

      {screen.kind === "missing" ? (
        <Alert tone="error" title={text.missingTitle}>
          {text.missingBody}
        </Alert>
      ) : null}

      {screen.kind === "unavailable" ? (
        <Alert tone="error" title={text.unavailableTitle}>
          {text.unavailableBody}
        </Alert>
      ) : null}

      {screen.kind === "refused" ? (
        <div className="space-y-5" data-testid="staff-invitation-refused">
          <Alert tone="error" title={refusal[screen.state].title}>
            {refusal[screen.state].body}
          </Alert>
          {refusal[screen.state].signIn ? (
            <Button asChild variant="outline" className="w-full" size="lg">
              <Link href="/login">{text.signIn}</Link>
            </Button>
          ) : null}
        </div>
      ) : null}

      {screen.kind === "done" ? (
        <div className="space-y-5">
          <Alert tone="success" title={text.doneTitle}>
            {text.doneBody}
          </Alert>
          <Button asChild className="w-full" size="lg">
            <Link href="/login">{text.signIn}</Link>
          </Button>
        </div>
      ) : null}

      {screen.kind === "ready" ? (
        <form className="space-y-5" onSubmit={submit} noValidate>
          {requestError ? <Alert tone="error" title={requestError} /> : null}

          <div className="rounded-xl border border-border bg-muted/40 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              {text.role}
            </p>
            <p className="mt-1 text-lg font-semibold" data-testid="staff-invitation-role">
              {screen.role === "ADMIN" ? text.roleAdmin : text.roleInstructor}
            </p>
            <p className="mt-2 text-sm text-muted-foreground">{text.fixed}</p>
          </div>

          <Field label={text.name} htmlFor="staff-display-name" error={errors.name}>
            <Input
              id="staff-display-name"
              ref={nameRef}
              autoComplete="name"
              value={displayName}
              onChange={(event) => setDisplayName(event.target.value)}
              aria-invalid={Boolean(errors.name)}
              aria-describedby={errors.name ? "staff-display-name-error" : undefined}
            />
          </Field>

          {/* The length rule is stated before it is enforced. It used to appear
              only as a refusal, after the reader had composed something. */}
          <Field
            label={text.password}
            htmlFor="staff-password"
            hint={text.invalidPassword}
            error={errors.password}
          >
            <PasswordInput
              id="staff-password"
              ref={passwordRef}
              autoComplete="new-password"
              minLength={passwordMinimum}
              maxLength={passwordMaximum}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              aria-invalid={Boolean(errors.password)}
              aria-describedby={
                errors.password ? "staff-password-error" : "staff-password-hint"
              }
            />
          </Field>

          <Field
            label={text.confirm}
            htmlFor="staff-password-confirm"
            error={errors.confirm}
          >
            <PasswordInput
              id="staff-password-confirm"
              ref={confirmRef}
              autoComplete="new-password"
              maxLength={passwordMaximum}
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
              aria-invalid={Boolean(errors.confirm)}
              aria-describedby={
                errors.confirm ? "staff-password-confirm-error" : undefined
              }
            />
          </Field>

          <Button type="submit" className="w-full" size="lg" disabled={submitting}>
            {submitting ? text.completing : text.complete}
          </Button>
        </form>
      ) : null}
    </AuthShell>
  );
}
