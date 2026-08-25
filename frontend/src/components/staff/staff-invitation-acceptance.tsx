"use client";

import * as React from "react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { AuthShell } from "@/components/auth/auth-shell";
import {
  completeStaffInvitation,
  previewStaffInvitation,
} from "@/lib/api/identity";
import { ProblemError } from "@/lib/api/problem";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  captureTokenFromFragment,
  releaseFragmentToken,
  scrubTokenFragment,
  validDisplayName,
  validPassword,
} from "@/lib/identity/validation";

type State = "checking" | "ready" | "invalid" | "unavailable" | "done";

const copy = {
  en: {
    title: "Complete your staff invitation",
    intro: "Review the assigned role and create your Gradex staff account.",
    checking: "Checking the invitation…",
    invalid: "This invitation is invalid, expired, revoked, or already used.",
    unavailable: "The invitation could not be checked. Reload the page to try again.",
    role: "Assigned role",
    roles: { INSTRUCTOR: "Instructor", ADMIN: "Administrator" },
    fixed: "The assigned role is fixed by the invitation and cannot be changed here.",
    name: "Display name",
    password: "Password",
    confirm: "Confirm password",
    complete: "Create staff account",
    completing: "Creating account…",
    mismatch: "The password confirmation does not match.",
    invalidName: "Enter a display name using 2–50 Arabic or Latin letters.",
    invalidPassword: "Use a password between 15 and 128 characters.",
    failed: "The invitation could not be completed. Try again.",
    done: "Your staff account is ready. Sign in with the invited email address.",
    signIn: "Sign in",
  },
  ar: {
    title: "أكمل دعوة حساب الفريق",
    intro: "راجع الدور المعيّن وأنشئ حساب فريق Gradex.",
    checking: "جارٍ التحقق من الدعوة…",
    invalid: "هذه الدعوة غير صالحة أو منتهية أو ملغاة أو مستخدمة سابقًا.",
    unavailable: "تعذر التحقق من الدعوة. أعد تحميل الصفحة للمحاولة مرة أخرى.",
    role: "الدور المعيّن",
    roles: { INSTRUCTOR: "مدرّس", ADMIN: "مسؤول" },
    fixed: "الدور محدد في الدعوة ولا يمكن تغييره هنا.",
    name: "اسم العرض",
    password: "كلمة المرور",
    confirm: "تأكيد كلمة المرور",
    complete: "إنشاء حساب الفريق",
    completing: "جارٍ إنشاء الحساب…",
    mismatch: "تأكيد كلمة المرور غير مطابق.",
    invalidName: "أدخل اسم عرض من حرفين إلى 50 حرفًا عربيًا أو لاتينيًا.",
    invalidPassword: "استخدم كلمة مرور من 15 إلى 128 حرفًا.",
    failed: "تعذر إكمال الدعوة. حاول مرة أخرى.",
    done: "حساب الفريق جاهز. سجّل الدخول باستخدام البريد المدعو.",
    signIn: "تسجيل الدخول",
  },
} as const;

export function StaffInvitationAcceptance() {
  const { locale } = useLocale();
  const text = copy[locale];
  const [state, setState] = React.useState<State>("checking");
  const [role, setRole] = React.useState<"INSTRUCTOR" | "ADMIN" | null>(null);
  const [displayName, setDisplayName] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [confirmation, setConfirmation] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);
  const bearer = React.useRef<string | null>(null);
  const started = React.useRef(false);

  React.useEffect(() => {
    if (started.current) return;
    started.current = true;
    bearer.current = captureTokenFromFragment("STAFF_INVITATION");
    scrubTokenFragment();
    if (!bearer.current) {
      setState("invalid");
      return;
    }
    previewStaffInvitation(bearer.current, locale)
      .then((preview) => {
        // Only a still-open invitation gets a form. The preview route answers 200 for an
        // invitation that is consumed, revoked, superseded, or expired, and offering the form
        // anyway asked the invitee to choose a password before telling them the link was
        // already used — the refusal came from the server either way, but only after the work.
        if (preview.state !== "PENDING") {
          bearer.current = null;
          releaseFragmentToken("STAFF_INVITATION");
          setState("invalid");
          return;
        }
        setRole(preview.invited_role);
        setState("ready");
      })
      .catch((caught) => {
        if (caught instanceof ProblemError && caught.problem.code === "TOKEN_INVALID") {
          bearer.current = null;
          releaseFragmentToken("STAFF_INVITATION");
          setState("invalid");
          return;
        }
        setState("unavailable");
      });
  }, [locale]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError(null);
    if (!validDisplayName(displayName)) {
      setError(text.invalidName);
      return;
    }
    if (!validPassword(password)) {
      setError(text.invalidPassword);
      return;
    }
    if (password !== confirmation) {
      setError(text.mismatch);
      return;
    }
    if (!bearer.current) {
      setState("invalid");
      return;
    }
    setSubmitting(true);
    try {
      await completeStaffInvitation(bearer.current, displayName, password, locale);
      bearer.current = null;
      releaseFragmentToken("STAFF_INVITATION");
      setPassword("");
      setConfirmation("");
      setState("done");
    } catch (caught) {
      if (caught instanceof ProblemError && caught.problem.code === "TOKEN_INVALID") {
        bearer.current = null;
        releaseFragmentToken("STAFF_INVITATION");
        setState("invalid");
      } else {
        setError(text.failed);
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthShell title={text.title} intro={text.intro}>
      {state === "checking" ? <Alert title={text.checking} /> : null}
      {state === "invalid" ? <Alert tone="error" title={text.invalid} /> : null}
      {state === "unavailable" ? <Alert tone="error" title={text.unavailable} /> : null}
      {state === "done" ? (
        <div className="space-y-5">
          <Alert tone="success" title={text.done} />
          <Button asChild className="w-full" size="lg">
            <Link href="/login">{text.signIn}</Link>
          </Button>
        </div>
      ) : null}
      {state === "ready" ? (
        <form className="space-y-5" onSubmit={submit} noValidate>
          {error ? <Alert tone="error" title={error} /> : null}
          <div className="rounded-xl border border-border bg-muted/40 p-4">
            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{text.role}</p>
            <p className="mt-1 text-lg font-semibold">{role ? text.roles[role] : null}</p>
            <p className="mt-2 text-sm text-muted-foreground">{text.fixed}</p>
          </div>
          <Field label={text.name} htmlFor="staff-display-name">
            <Input id="staff-display-name" autoComplete="name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
          </Field>
          <Field label={text.password} htmlFor="staff-password">
            <Input id="staff-password" type="password" autoComplete="new-password" dir="ltr" value={password} onChange={(event) => setPassword(event.target.value)} />
          </Field>
          <Field label={text.confirm} htmlFor="staff-password-confirm">
            <Input id="staff-password-confirm" type="password" autoComplete="new-password" dir="ltr" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} />
          </Field>
          <Button className="w-full" size="lg" disabled={submitting}>
            {submitting ? text.completing : text.complete}
          </Button>
        </form>
      ) : null}
    </AuthShell>
  );
}
