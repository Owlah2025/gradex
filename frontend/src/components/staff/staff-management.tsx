"use client";

import * as React from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/lib/i18n/locale-provider";

export function StaffManagement() {
  const { t } = useLocale();

  // Invite state
  const [inviteEmail, setInviteEmail] = React.useState("");
  const [inviteRole, setInviteRole] = React.useState<"INSTRUCTOR" | "ADMIN">("INSTRUCTOR");
  const [inviteStatus, setInviteStatus] = React.useState<"idle" | "sending" | "success" | "error">("idle");
  const [inviteMsg, setInviteMsg] = React.useState<string | null>(null);

  // Suspend state
  const [suspendID, setSuspendID] = React.useState("");
  const [suspendReason, setSuspendReason] = React.useState("");
  const [suspendStatus, setSuspendStatus] = React.useState<"idle" | "submitting" | "success" | "error">("idle");
  const [suspendMsg, setSuspendMsg] = React.useState<string | null>(null);

  // Reinstate state
  const [reinstateID, setReinstateID] = React.useState("");
  const [reinstateReason, setReinstateReason] = React.useState("");
  const [reinstateStatus, setReinstateStatus] = React.useState<"idle" | "submitting" | "success" | "error">("idle");
  const [reinstateMsg, setReinstateMsg] = React.useState<string | null>(null);

  const handleCreateInvite = async (e: React.FormEvent) => {
    e.preventDefault();
    setInviteStatus("sending");
    setInviteMsg(null);

    try {
      const res = await fetch("/api/v1/staff/invitations", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: inviteEmail, role: inviteRole }),
      });

      if (!res.ok) throw new Error("Invite failed");
      setInviteStatus("success");
      setInviteEmail("");
    } catch {
      setInviteStatus("error");
      setInviteMsg(t.auth.register.failed);
    }
  };

  const handleSuspend = async (e: React.FormEvent) => {
    e.preventDefault();
    setSuspendStatus("submitting");
    setSuspendMsg(null);

    try {
      const res = await fetch(`/api/v1/staff/${encodeURIComponent(suspendID)}/suspend`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason: suspendReason }),
      });

      if (!res.ok) throw new Error("Suspend failed");
      setSuspendStatus("success");
      setSuspendID("");
      setSuspendReason("");
    } catch {
      setSuspendStatus("error");
      setSuspendMsg(t.auth.register.failed);
    }
  };

  const handleReinstate = async (e: React.FormEvent) => {
    e.preventDefault();
    setReinstateStatus("submitting");
    setReinstateMsg(null);

    try {
      const res = await fetch(`/api/v1/staff/${encodeURIComponent(reinstateID)}/reinstate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason: reinstateReason }),
      });

      if (!res.ok) throw new Error("Reinstate failed");
      setReinstateStatus("success");
      setReinstateID("");
      setReinstateReason("");
    } catch {
      setReinstateStatus("error");
      setReinstateMsg(t.auth.register.failed);
    }
  };

  return (
    <div className="max-w-4xl mx-auto space-y-8">
      {/* Create Invitation Section */}
      <section className="bg-surface p-6 rounded-lg shadow-sm border border-border space-y-4">
        <h2 className="text-xl font-bold">{t.auth.staff.createTitle}</h2>
        <p className="text-sm text-muted-foreground">{t.auth.staff.createIntro}</p>

        {inviteMsg && <Alert tone={inviteStatus === "error" ? "error" : "success"} title={inviteMsg} />}

        <form onSubmit={handleCreateInvite} className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Field htmlFor="staff-invite-email" label={t.auth.staff.email}>
              <Input
                id="staff-invite-email"
                type="email"
                value={inviteEmail}
                onChange={(e) => setInviteEmail(e.target.value)}
                required
                disabled={inviteStatus === "sending"}
              />
            </Field>

            <Field htmlFor="staff-invite-role" label={t.auth.staff.role}>
              <select
                id="staff-invite-role"
                value={inviteRole}
                onChange={(e) => setInviteRole(e.target.value as "INSTRUCTOR" | "ADMIN")}
                className="w-full px-3 py-2 border rounded-md bg-background text-foreground"
                disabled={inviteStatus === "sending"}
              >
                <option value="INSTRUCTOR">{t.auth.staff.roleInstructor}</option>
                <option value="ADMIN">{t.auth.staff.roleAdmin}</option>
              </select>
            </Field>
          </div>

          <Button type="submit" variant="accent" disabled={inviteStatus === "sending"}>
            {inviteStatus === "sending" ? t.auth.staff.sendingInvite : t.auth.staff.sendInvite}
          </Button>
        </form>
      </section>

      {/* Account Suspension Section */}
      <section className="bg-surface p-6 rounded-lg shadow-sm border border-border space-y-4">
        <h2 className="text-xl font-bold text-destructive">{t.auth.staff.suspendTitle}</h2>

        {suspendMsg && <Alert tone={suspendStatus === "error" ? "error" : "success"} title={suspendMsg} />}

        <form onSubmit={handleSuspend} className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Field htmlFor="suspend-account-id" label="Account ID">
              <Input
                id="suspend-account-id"
                type="text"
                value={suspendID}
                onChange={(e) => setSuspendID(e.target.value)}
                required
                disabled={suspendStatus === "submitting"}
              />
            </Field>

            <Field htmlFor="suspend-reason" label={t.auth.staff.suspendReason}>
              <Input
                id="suspend-reason"
                type="text"
                value={suspendReason}
                onChange={(e) => setSuspendReason(e.target.value)}
                required
                disabled={suspendStatus === "submitting"}
              />
            </Field>
          </div>

          <Button type="submit" variant="destructive" disabled={suspendStatus === "submitting"}>
            {suspendStatus === "submitting" ? t.auth.staff.suspending : t.auth.staff.suspendAction}
          </Button>
        </form>
      </section>

      {/* Account Reinstatement Section */}
      <section className="bg-surface p-6 rounded-lg shadow-sm border border-border space-y-4">
        <h2 className="text-xl font-bold">{t.auth.staff.reinstateTitle}</h2>

        {reinstateMsg && <Alert tone={reinstateStatus === "error" ? "error" : "success"} title={reinstateMsg} />}

        <form onSubmit={handleReinstate} className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Field htmlFor="reinstate-account-id" label="Account ID">
              <Input
                id="reinstate-account-id"
                type="text"
                value={reinstateID}
                onChange={(e) => setReinstateID(e.target.value)}
                required
                disabled={reinstateStatus === "submitting"}
              />
            </Field>

            <Field htmlFor="reinstate-reason" label={t.auth.staff.reinstateReason}>
              <Input
                id="reinstate-reason"
                type="text"
                value={reinstateReason}
                onChange={(e) => setReinstateReason(e.target.value)}
                disabled={reinstateStatus === "submitting"}
              />
            </Field>
          </div>

          <Button type="submit" variant="accent" disabled={reinstateStatus === "submitting"}>
            {reinstateStatus === "submitting" ? t.auth.staff.reinstating : t.auth.staff.reinstateAction}
          </Button>
        </form>
      </section>
    </div>
  );
}
