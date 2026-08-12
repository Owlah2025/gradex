"use client";

import * as React from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  createStaffInvitation,
  listInstructorStaffAccounts,
  reinstateStaffAccount,
  suspendStaffAccount,
  type InstructorStaffAccount,
} from "@/lib/api/identity";
import { currentCSRFToken } from "@/lib/identity/session";

type LoadState = "loading" | "ready" | "error";
type StatusChange = (account: InstructorStaffAccount) => Promise<void>;

function InstructorAccountRow({ account, reason, busy, onReasonChange, onStatusChange, t }: {
  account: InstructorStaffAccount;
  reason: string;
  busy: boolean;
  onReasonChange: (reason: string) => void;
  onStatusChange: StatusChange;
  t: ReturnType<typeof useLocale>["t"];
}) {
  const suspended = account.status === "SUSPENDED";
  return <article className="space-y-3 border-t border-border pt-4 first:border-t-0 first:pt-0">
    <div className="flex flex-wrap justify-between gap-2">
      <div><p className="font-medium">{account.display_name || account.email}</p><p className="text-sm text-muted-foreground">{account.email}</p></div>
      <p className="text-sm"><span className="text-muted-foreground">{t.auth.staff.accountStatus}: </span>{suspended ? t.auth.staff.suspended : t.auth.staff.active}</p>
    </div>
    <div className="flex flex-col gap-3 sm:flex-row">
      <Input aria-label={suspended ? t.auth.staff.reinstateReason : t.auth.staff.suspendReason} value={reason} onChange={(event) => onReasonChange(event.target.value)} placeholder={suspended ? t.auth.staff.reinstateReason : t.auth.staff.suspendReason} disabled={busy} />
      <Button type="button" variant={suspended ? "accent" : "destructive"} disabled={busy || !reason.trim()} onClick={() => void onStatusChange(account)}>
        {busy ? (suspended ? t.auth.staff.reinstating : t.auth.staff.suspending) : (suspended ? t.auth.staff.reinstateAction : t.auth.staff.suspendAction)}
      </Button>
    </div>
  </article>;
}

export function StaffManagement() {
  const { t, locale } = useLocale();
  const [accounts, setAccounts] = React.useState<InstructorStaffAccount[]>([]);
  const [loadState, setLoadState] = React.useState<LoadState>("loading");
  const [inviteEmail, setInviteEmail] = React.useState("");
  const [inviteState, setInviteState] = React.useState<"idle" | "sending" | "success" | "error">("idle");
  const [message, setMessage] = React.useState<string | null>(null);
  const [reasonByID, setReasonByID] = React.useState<Record<string, string>>({});
  const [mutatingID, setMutatingID] = React.useState<string | null>(null);

  const loadAccounts = React.useCallback(async () => {
    setLoadState("loading");
    try {
      const response = await listInstructorStaffAccounts(locale);
      setAccounts(response.instructors);
      setLoadState("ready");
    } catch {
      setLoadState("error");
    }
  }, [locale]);

  React.useEffect(() => { void loadAccounts(); }, [loadAccounts]);

  const handleInvite = async (event: React.FormEvent) => {
    event.preventDefault();
    setInviteState("sending");
    setMessage(null);
    try {
      await createStaffInvitation(inviteEmail, locale, currentCSRFToken() ?? undefined);
      setInviteEmail("");
      setInviteState("success");
      setMessage(t.auth.staff.inviteSuccess);
    } catch {
      setInviteState("error");
      setMessage(t.auth.register.failed);
    }
  };

  const changeStatus = async (account: InstructorStaffAccount) => {
    const reason = reasonByID[account.id]?.trim();
    if (!reason) return;
    setMutatingID(account.id);
    setMessage(null);
    try {
      const csrf = currentCSRFToken() ?? undefined;
      if (account.status === "ACTIVE") {
        await suspendStaffAccount(account.id, reason, locale, csrf);
        setMessage(t.auth.staff.suspendSuccess);
      } else {
        await reinstateStaffAccount(account.id, reason, locale, csrf);
        setMessage(t.auth.staff.reinstateSuccess);
      }
      setReasonByID((current) => ({ ...current, [account.id]: "" }));
      await loadAccounts();
    } catch {
      setMessage(t.auth.register.failed);
    } finally {
      setMutatingID(null);
    }
  };

  return (
    <div className="mx-auto max-w-4xl space-y-8">
      <section className="space-y-4 rounded-lg border border-border bg-surface p-6 shadow-sm">
        <h2 className="text-xl font-bold">{t.auth.staff.createTitle}</h2>
        <p className="text-sm text-muted-foreground">{t.auth.staff.createIntro}</p>
        {message && <Alert tone={inviteState === "error" ? "error" : "success"} title={message} />}
        <form onSubmit={handleInvite} className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <Field htmlFor="staff-invite-email" label={t.auth.staff.email} className="flex-1">
            <Input id="staff-invite-email" type="email" value={inviteEmail} onChange={(event) => setInviteEmail(event.target.value)} required disabled={inviteState === "sending"} />
          </Field>
          <Button type="submit" variant="accent" disabled={inviteState === "sending"}>
            {inviteState === "sending" ? t.auth.staff.sendingInvite : t.auth.staff.sendInvite}
          </Button>
        </form>
      </section>

      <section className="space-y-4 rounded-lg border border-border bg-surface p-6 shadow-sm">
        <h2 className="text-xl font-bold">{t.auth.staff.instructorsTitle}</h2>
        {loadState === "loading" && <p className="text-sm text-muted-foreground">{t.auth.staff.loading}</p>}
        {loadState === "error" && <Alert tone="error" title={t.auth.staff.loadingFailed} />}
        {loadState === "ready" && accounts.length === 0 && <p className="text-sm text-muted-foreground">{t.auth.staff.noInstructors}</p>}
        {loadState === "ready" && accounts.map((account) => <InstructorAccountRow key={account.id} account={account} reason={reasonByID[account.id] ?? ""} busy={mutatingID === account.id} onReasonChange={(reason) => setReasonByID((current) => ({ ...current, [account.id]: reason }))} onStatusChange={changeStatus} t={t} />)}
      </section>
    </div>
  );
}
