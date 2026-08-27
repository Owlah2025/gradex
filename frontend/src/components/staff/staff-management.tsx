"use client";

import * as React from "react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@/components/ui/table";
import {
  WorkspacePage,
  WorkspacePageHeader,
  WorkspaceSection,
} from "@/components/layout/workspace-page";
import { describeApiError } from "@/lib/api/api-error";
import { formatDate } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  createStaffInvitation,
  listInstructorStaffAccounts,
  listStaffInvitations,
  reinstateStaffAccount,
  revokeStaffInvitation,
  suspendStaffAccount,
  type InstructorStaffAccount,
  type StaffInvitationSummary,
} from "@/lib/api/identity";
import { currentCSRFToken } from "@/lib/identity/session";

/**
 * The staff workspace.
 *
 * # WHAT CHANGED, AND WHY IT IS NOT A REDESIGN
 *
 * Two of the three things an Admin needs here had no screen at all. `GET /staff-invitations` and
 * `DELETE /staff-invitations/:id` have existed on the server since the staff lifecycle shipped, and
 * nothing in the product called either one: an invitation could be sent and then never seen again,
 * and there was no way to take one back. An Admin who mis-typed an address had to wait for it to
 * expire. Those two routes are now reachable, which is the whole of the new surface — no contract
 * moved.
 *
 * # WHAT THE PRODUCT ACTUALLY DOES, SAID ONCE
 *
 * There is no "resend". Inviting an address that already has an open invitation supersedes it on
 * the server and issues a fresh link, so re-inviting *is* the resend and needs no cancel first. The
 * copy says so rather than offering a button the API does not have.
 *
 * The invitation list is `state = 'PENDING'` only. A state column would read the same on every row,
 * so the list's own title carries that fact and the rows carry what differs: who, as what, when.
 *
 * # CONSEQUENCES ARE THE CONTRACT'S
 *
 * Suspension revokes the account's live sessions and bumps its session epoch. It changes no Course
 * and revokes no Student's access, and the confirmation says exactly that — an Admin deciding
 * whether to suspend an Instructor is deciding about one person's sign-in, not about their
 * students. Cancelling an invitation moves it to REVOKED, which is what stops the emailed link
 * working.
 *
 * Both mutations require a recently authenticated session, and the server refuses a stale one. That
 * refusal is surfaced as the server worded it rather than flattened into "something went wrong",
 * because "sign in again" is the only useful thing it says.
 */

type Load = "loading" | "ready" | "failed";

type Pending =
  | { kind: "invitation"; invitation: StaffInvitationSummary }
  | { kind: "suspend"; account: InstructorStaffAccount }
  | { kind: "reinstate"; account: InstructorStaffAccount };

export function StaffManagement() {
  const { t, locale } = useLocale();
  const copy = t.adminStaff;

  const [invitations, setInvitations] = React.useState<StaffInvitationSummary[]>([]);
  const [invitationLoad, setInvitationLoad] = React.useState<Load>("loading");
  const [accounts, setAccounts] = React.useState<InstructorStaffAccount[]>([]);
  const [accountLoad, setAccountLoad] = React.useState<Load>("loading");

  const [inviteEmail, setInviteEmail] = React.useState("");
  const [inviting, setInviting] = React.useState(false);

  const [reasonByID, setReasonByID] = React.useState<Record<string, string>>({});
  const [pending, setPending] = React.useState<Pending | null>(null);
  const [busy, setBusy] = React.useState(false);

  // One notice, because there is one thing the Admin just did. `tone` is what separates a completed
  // action from a refused one; a success-styled banner carrying a failure is the one outcome this
  // screen must never produce.
  const [notice, setNotice] = React.useState<{ tone: "success" | "error"; text: string } | null>(
    null,
  );

  const loadInvitations = React.useCallback(async () => {
    setInvitationLoad("loading");
    try {
      const response = await listStaffInvitations(locale);
      setInvitations(response.invitations);
      setInvitationLoad("ready");
    } catch {
      setInvitationLoad("failed");
    }
  }, [locale]);

  const loadAccounts = React.useCallback(async () => {
    setAccountLoad("loading");
    try {
      const response = await listInstructorStaffAccounts(locale);
      setAccounts(response.instructors);
      setAccountLoad("ready");
    } catch {
      setAccountLoad("failed");
    }
  }, [locale]);

  React.useEffect(() => {
    void loadInvitations();
    void loadAccounts();
  }, [loadInvitations, loadAccounts]);

  const invite = async (event: React.FormEvent) => {
    event.preventDefault();
    setInviting(true);
    setNotice(null);
    try {
      await createStaffInvitation(inviteEmail, locale, currentCSRFToken() ?? undefined);
      setInviteEmail("");
      setNotice({ tone: "success", text: copy.invite.success });
      // The new invitation belongs in the list beneath, and it may have superseded one that was
      // already there. Re-reading is the only way to show both facts.
      await loadInvitations();
    } catch (cause) {
      setNotice({ tone: "error", text: describeApiError(cause, locale) || copy.invite.failed });
    } finally {
      setInviting(false);
    }
  };

  /**
   * Every confirmed action runs through here, so all three share one rule: the API is called
   * exactly once, the notice reflects what the server actually answered, and the dialog closes
   * either way rather than leaving the reader inside a modal wondering.
   */
  const confirm = async () => {
    if (!pending || busy) return;
    const csrf = currentCSRFToken() ?? undefined;
    setBusy(true);
    setNotice(null);
    try {
      if (pending.kind === "invitation") {
        await revokeStaffInvitation(pending.invitation.id, locale, csrf);
        setNotice({ tone: "success", text: copy.invitations.cancelled });
        await loadInvitations();
      } else {
        const account = pending.account;
        const reason = (reasonByID[account.id] ?? "").trim();
        if (pending.kind === "suspend") {
          await suspendStaffAccount(account.id, reason, locale, csrf);
          setNotice({ tone: "success", text: copy.instructors.suspendSuccess });
        } else {
          await reinstateStaffAccount(account.id, reason, locale, csrf);
          setNotice({ tone: "success", text: copy.instructors.reinstateSuccess });
        }
        setReasonByID((current) => ({ ...current, [account.id]: "" }));
        await loadAccounts();
      }
    } catch (cause) {
      const fallback =
        pending.kind === "invitation" ? copy.invitations.cancelFailed : copy.instructors.failed;
      setNotice({ tone: "error", text: describeApiError(cause, locale) || fallback });
    } finally {
      setBusy(false);
      setPending(null);
    }
  };

  // Both account actions record a reason on the server; cancelling an invitation does not.
  const needsReason = pending !== null && pending.kind !== "invitation";
  const reasonForPending =
    pending && pending.kind !== "invitation" ? (reasonByID[pending.account.id] ?? "") : "";

  const dialog = pending
    ? pending.kind === "invitation"
      ? {
          title: copy.invitations.cancelTitle,
          body: copy.invitations.cancelBody,
          confirmLabel: copy.invitations.cancelConfirm,
          cancelLabel: copy.invitations.keep,
        }
      : pending.kind === "suspend"
        ? {
            title: copy.instructors.suspendTitle,
            body: copy.instructors.suspendBody,
            confirmLabel: copy.instructors.suspendConfirm,
            cancelLabel: copy.instructors.keep,
          }
        : {
            title: copy.instructors.reinstateTitle,
            body: copy.instructors.reinstateBody,
            confirmLabel: copy.instructors.reinstateConfirm,
            cancelLabel: copy.instructors.keep,
          }
    : null;

  return (
    <WorkspacePage testID="staff-workspace">
      <WorkspacePageHeader title={copy.title} description={copy.intro} />

      {notice ? (
        <div className="mt-6" data-testid="staff-notice" data-tone={notice.tone}>
          <Alert tone={notice.tone} title={notice.text} />
        </div>
      ) : null}

      <WorkspaceSection
        title={copy.invite.title}
        description={copy.invite.lead}
        testID="staff-invite"
      >
        <form onSubmit={invite} className="flex flex-col gap-4 sm:flex-row sm:items-start">
          <Field
            htmlFor="staff-invite-email"
            label={copy.invite.email}
            hint={copy.invite.emailHint}
            className="flex-1"
          >
            <Input
              id="staff-invite-email"
              data-testid="staff-invite-email"
              type="email"
              autoComplete="off"
              value={inviteEmail}
              onChange={(event) => setInviteEmail(event.target.value)}
              required
              disabled={inviting}
            />
          </Field>
          {/* The role is not a choice, so it is not a control. Instructor is the only role this
              screen can issue, and a select with one option is a question with one answer. */}
          <Field htmlFor="staff-invite-role" label={copy.invite.role} hint={copy.invite.roleNote}>
            <Input
              id="staff-invite-role"
              data-testid="staff-invite-role"
              value={copy.invite.roleFixed}
              readOnly
              disabled
            />
          </Field>
          <Button type="submit" disabled={inviting} data-testid="staff-invite-submit" className="sm:mt-7">
            {inviting ? copy.invite.sending : copy.invite.submit}
          </Button>
        </form>
        <p className="mt-3 text-sm text-muted-foreground">{copy.invite.supersedes}</p>
      </WorkspaceSection>

      <WorkspaceSection
        title={copy.invitations.title}
        description={copy.invitations.lead}
        testID="staff-invitations"
      >
        {invitationLoad === "loading" ? (
          <LoadingState label={copy.invitations.loading} testID="staff-invitations-loading" />
        ) : null}
        {invitationLoad === "failed" ? (
          <ErrorState
            title={copy.invitations.loadFailed}
            retryLabel={copy.invitations.retry}
            onRetry={() => void loadInvitations()}
            testID="staff-invitations-failed"
          />
        ) : null}
        {invitationLoad === "ready" && invitations.length === 0 ? (
          <EmptyState
            density="compact"
            title={copy.invitations.emptyTitle}
            description={copy.invitations.emptyBody}
            testID="staff-invitations-empty"
          />
        ) : null}
        {invitationLoad === "ready" && invitations.length > 0 ? (
          <TableContainer>
            <Table>
              <TableCaption>{copy.invitations.caption}</TableCaption>
              <TableHead>
                <TableRow>
                  <TableHeaderCell scope="col">{copy.invitations.invitee}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.invitations.role}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.invitations.sent}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.invitations.actions}</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {invitations.map((invitation) => (
                  <TableRow key={invitation.id} data-testid="staff-invitation-row">
                    {/* An email address is Latin script inside Arabic prose. `bdi` keeps the
                        surrounding sentence from reordering it. */}
                    <TableHeaderCell scope="row">
                      <bdi data-testid="staff-invitation-email">{invitation.email}</bdi>
                    </TableHeaderCell>
                    <TableCell>{roleLabel(invitation.invited_role, copy)}</TableCell>
                    <TableCell>{formatDate(invitation.created_at, locale)}</TableCell>
                    <TableCell>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        data-testid="staff-invitation-cancel"
                        // The accessible name says which invitation, because "Cancel invitation"
                        // repeated down a column names nothing.
                        aria-label={`${copy.invitations.cancel} — ${invitation.email}`}
                        onClick={() => setPending({ kind: "invitation", invitation })}
                      >
                        {copy.invitations.cancel}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        ) : null}
      </WorkspaceSection>

      <WorkspaceSection
        title={copy.instructors.title}
        description={copy.instructors.lead}
        testID="staff-instructors"
      >
        {accountLoad === "loading" ? (
          <LoadingState label={copy.instructors.loading} testID="staff-instructors-loading" />
        ) : null}
        {accountLoad === "failed" ? (
          <ErrorState
            title={copy.instructors.loadFailed}
            retryLabel={copy.instructors.retry}
            onRetry={() => void loadAccounts()}
            testID="staff-instructors-failed"
          />
        ) : null}
        {accountLoad === "ready" && accounts.length === 0 ? (
          <EmptyState
            density="compact"
            title={copy.instructors.emptyTitle}
            description={copy.instructors.emptyBody}
            testID="staff-instructors-empty"
          />
        ) : null}
        {accountLoad === "ready" && accounts.length > 0 ? (
          <>
            {/* The two states and what each one means, said once. */}
            <dl
              data-testid="staff-instructor-legend"
              className="mb-4 grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2"
            >
              {(
                [
                  [copy.instructors.active, copy.instructors.activeDetail],
                  [copy.instructors.suspended, copy.instructors.suspendedDetail],
                ] as const
              ).map(([term, detail]) => (
                <div key={term} className="flex flex-wrap gap-x-2">
                  <dt className="font-semibold text-foreground">{term}</dt>
                  <dd className="min-w-0 flex-1 text-muted-foreground">{detail}</dd>
                </div>
              ))}
            </dl>
            <TableContainer>
            <Table>
              <TableCaption>{copy.instructors.caption}</TableCaption>
              <TableHead>
                <TableRow>
                  {/* Name and address are one column, because they are one thing: who this is. Two
                      columns of identity cost a third of a 390px viewport and pushed the action
                      itself off the end of the container. */}
                  <TableHeaderCell scope="col">{copy.instructors.instructor}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.instructors.state}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.instructors.actions}</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {accounts.map((account) => {
                  const suspended = account.status === "SUSPENDED";
                  return (
                    <TableRow key={account.id} data-testid="staff-instructor-row">
                      <TableHeaderCell scope="row">
                        {/* Never the identifier. An account with no display name is read by its
                            email address, which is the other thing a person recognises. */}
                        <bdi data-testid="staff-instructor-name">
                          {account.display_name || account.email}
                        </bdi>
                        <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
                          <bdi>{account.email}</bdi>
                        </span>
                      </TableHeaderCell>
                      <TableCell>
                        {/* The state word, which is what carries it without colour. What the state
                            *means* is said once in the legend above rather than repeated verbatim
                            down every row of a directory where it is usually the same. */}
                        <StatusBadge
                          tone={suspended ? "neutral" : "success"}
                          label={suspended ? copy.instructors.suspended : copy.instructors.active}
                          labelTestID="staff-instructor-state"
                        />
                      </TableCell>
                      <TableCell>
                        {/* One control. The reason the server requires is collected in the
                            confirmation, beside the consequence it is being recorded against —
                            a field in every row made the directory a column of standing red
                            buttons and cost three times the row height to say nothing. */}
                        <Button
                          type="button"
                          variant={suspended ? "outline" : "destructive"}
                          size="sm"
                          data-testid={
                            suspended ? "staff-instructor-reinstate" : "staff-instructor-suspend"
                          }
                          aria-label={`${
                            suspended ? copy.instructors.reinstate : copy.instructors.suspend
                          } — ${account.display_name || account.email}`}
                          disabled={busy}
                          onClick={() =>
                            setPending({ kind: suspended ? "reinstate" : "suspend", account })
                          }
                        >
                          {suspended ? copy.instructors.reinstate : copy.instructors.suspend}
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
            </TableContainer>
          </>
        ) : null}
      </WorkspaceSection>

      {dialog ? (
        <ConfirmDialog
          open
          onOpenChange={(next) => {
            if (!next && !busy) setPending(null);
          }}
          title={dialog.title}
          body={dialog.body}
          confirmLabel={dialog.confirmLabel}
          cancelLabel={dialog.cancelLabel}
          tone={pending?.kind === "reinstate" ? "default" : "destructive"}
          busy={busy}
          confirmDisabled={needsReason && reasonForPending.trim() === ""}
          onConfirm={() => void confirm()}
          testID="staff-confirm"
        >
          {needsReason ? (
            <Field
              htmlFor="staff-reason"
              label={copy.instructors.reason}
              hint={
                pending?.kind === "reinstate"
                  ? copy.instructors.reinstateReasonHint
                  : copy.instructors.suspendReasonHint
              }
            >
              <Input
                id="staff-reason"
                data-testid="staff-instructor-reason"
                value={reasonForPending}
                onChange={(event) =>
                  setReasonByID((current) => ({
                    ...current,
                    [pending.account.id]: event.target.value,
                  }))
                }
                disabled={busy}
              />
            </Field>
          ) : null}
        </ConfirmDialog>
      ) : null}
    </WorkspacePage>
  );
}

function roleLabel(
  role: StaffInvitationSummary["invited_role"],
  copy: ReturnType<typeof useLocale>["t"]["adminStaff"],
): string {
  // The contract admits ADMIN even though this screen only issues INSTRUCTOR, so a row seeded any
  // other way still reads as a role rather than as an enum.
  return role === "ADMIN" ? copy.instructors.title : copy.invite.roleFixed;
}
