"use client";

import { useCallback, useEffect, useState } from "react";
import {
  adjustEntitlementExpiry,
  approveCourseAccessInvitation,
  cancelCourseAccessInvitation,
  createCourseAccessInvitation,
  getAdminEntitlementDetail,
  listAdminCourseAccessInvitations,
  rejectCourseAccessInvitation,
  resendCourseAccessInvitation,
  revokeEntitlement,
  setCourseDefaultAccessExpiry,
  type AdminEntitlementDetail,
  type CourseAccessInvitation,
} from "@/lib/api/access";
import { describeApiError } from "@/lib/api/api-error";
import { getPublicCourses } from "@/lib/api/public-catalog";
import { formatDate, formatDateTime } from "@/lib/i18n/format";
import { useLocale } from "@/lib/i18n/locale-provider";
import { PublishedCourseSelector } from "@/components/admin/published-course-selector";
import { PurchaseRequestsPanel } from "@/components/admin/purchase-requests";
import {
  buildPublishedCourseOptions,
  findPublishedCourse,
  invitationCourseLabel,
  type PublishedCourseOption,
} from "@/components/admin/published-courses";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { LoadingState } from "@/components/common/loading-state";
import { Sheet, SheetContent, SheetTitle } from "@/components/ui/sheet";
import { StatusBadge } from "@/components/common/status-badge";
import { Textarea } from "@/components/ui/textarea";
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

/**
 * Course access operations.
 *
 * # WHAT THIS SCREEN IS, AND WHAT IT REFUSES TO LOOK LIKE
 *
 * Gradex grants Course access by hand. Money moves somewhere the product cannot see, an
 * Administrator records that it moved, and the grant follows. Nothing here settles a payment, and
 * the copy is written so that no control on the page could be mistaken for one.
 *
 * # THE FIVE STATES AN ADMIN ACTUALLY READS
 *
 * The queue used to print `inv.state` — PENDING_ADMIN_APPROVAL — into a coloured pill, and the
 * access record printed ACTIVE and REVOKED. Every one of those is now the state said as what it
 * means for the two people involved, with the sentence beside it that a colour cannot carry. The
 * enums stay in the payload, in the API call, and in `data-testid`, which is where they belong.
 *
 * # AND NO IDENTIFIERS
 *
 * An invitation was previously named to the Admin by its UUID — in the rejection dialog, in the
 * success notice, in the resend notice. An invitation is named by the person it was sent to and the
 * Course it is for. The identifier is still what the API is called with and still what the row's
 * test id is built from; it is not reading matter.
 */

type Load = "loading" | "ready" | "failed";

/** One page of the server's invitation list. The screen says so rather than implying completeness. */
const QUEUE_PAGE_LIMIT = 100;

/** Which confirmation is open, and what it is about. */
type Pending =
  | { kind: "approve" | "reject" | "resend" | "cancel"; invitation: CourseAccessInvitation }
  | { kind: "revoke" };

const INVITATION_TONE: Record<
  CourseAccessInvitation["state"],
  "default" | "accent" | "success" | "neutral"
> = {
  PENDING_STUDENT_ACCEPTANCE: "default",
  PENDING_ADMIN_APPROVAL: "accent",
  APPROVED: "success",
  REJECTED: "neutral",
  CANCELLED: "neutral",
};

export default function AdminCourseAccessPage() {
  const { locale, t } = useLocale();
  const copy = t.adminAccess;

  const [invitations, setInvitations] = useState<CourseAccessInvitation[]>([]);
  // What the server says exists, against what this page asked for. The queue is a bounded page, and
  // a directory that shows a hundred rows while saying nothing reads as the whole list — which is
  // the Tranche A lesson about a server-bounded directory not being an actionable queue.
  const [queueTotal, setQueueTotal] = useState(0);
  const [queueLoad, setQueueLoad] = useState<Load>("loading");
  const [notice, setNotice] = useState<{ tone: "success" | "error"; text: string } | null>(null);

  const [courseOptions, setCourseOptions] = useState<PublishedCourseOption[]>([]);
  const [coursesLoading, setCoursesLoading] = useState(true);
  const [coursesError, setCoursesError] = useState<string | null>(null);
  const [selectedCourseId, setSelectedCourseId] = useState("");

  const [expiryDate, setExpiryDate] = useState("");
  const [expiryReason, setExpiryReason] = useState("");
  const [expirySubmitting, setExpirySubmitting] = useState(false);

  const [createEmail, setCreateEmail] = useState("");
  const [createNote, setCreateNote] = useState("");
  const [createRef, setCreateRef] = useState("");
  const [createSubmitting, setCreateSubmitting] = useState(false);

  const [pending, setPending] = useState<Pending | null>(null);
  const [rejectReason, setRejectReason] = useState("");
  const [busy, setBusy] = useState(false);

  const [detail, setDetail] = useState<AdminEntitlementDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailNotice, setDetailNotice] = useState<{ tone: "success" | "error"; text: string } | null>(
    null,
  );
  const [detailBusy, setDetailBusy] = useState(false);
  const [adjustDate, setAdjustDate] = useState("");
  const [adjustReason, setAdjustReason] = useState("");
  const [adjustSupportRef, setAdjustSupportRef] = useState("");
  const [revokeReason, setRevokeReason] = useState("");
  const [revokeSupportRef, setRevokeSupportRef] = useState("");

  const fetchInvitations = useCallback(async () => {
    setQueueLoad("loading");
    try {
      const res = await listAdminCourseAccessInvitations(1, QUEUE_PAGE_LIMIT, locale);
      setInvitations(res?.invitations ?? []);
      setQueueTotal(res?.total ?? res?.invitations?.length ?? 0);
      setQueueLoad("ready");
    } catch {
      setQueueLoad("failed");
    }
  }, [locale]);

  /**
   * The published catalogue is the authoritative list of Courses a launch grant can target:
   * published, not emergency-suspended, not retired, live revision. The second read only supplies
   * the other-locale title, so its failure narrows the label rather than the Course list.
   */
  const fetchCourses = useCallback(async () => {
    setCoursesLoading(true);
    setCoursesError(null);
    try {
      const alternateLocale = locale === "ar" ? "en" : "ar";
      const [primary, alternate] = await Promise.all([
        getPublicCourses(locale),
        getPublicCourses(alternateLocale).catch(() => null),
      ]);
      const options = buildPublishedCourseOptions(primary.items ?? [], alternate?.items ?? []);
      setCourseOptions(options);
      // A Course that left the published catalogue must not stay silently selected under a stale
      // label.
      setSelectedCourseId((current) =>
        current && options.some((option) => option.id === current) ? current : "",
      );
    } catch (cause) {
      setCourseOptions([]);
      setSelectedCourseId("");
      setCoursesError(describeApiError(cause, locale));
    } finally {
      setCoursesLoading(false);
    }
  }, [locale]);

  useEffect(() => {
    void fetchInvitations();
  }, [fetchInvitations]);

  useEffect(() => {
    void fetchCourses();
  }, [fetchCourses]);

  const selectedCourse = findPublishedCourse(courseOptions, selectedCourseId);
  const courseLabel = (courseID: string): string =>
    invitationCourseLabel(courseOptions, courseID);

  const handleSetExpiry = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!selectedCourseId || !expiryDate || !expiryReason.trim()) return;
    setExpirySubmitting(true);
    setNotice(null);
    try {
      await setCourseDefaultAccessExpiry(selectedCourseId, expiryDate, expiryReason.trim(), locale);
      setNotice({ tone: "success", text: copy.expiry.saved });
      setExpiryDate("");
      setExpiryReason("");
    } catch (cause) {
      setNotice({ tone: "error", text: describeApiError(cause, locale) || copy.expiry.failed });
    } finally {
      setExpirySubmitting(false);
    }
  };

  const handleCreateInvitation = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!selectedCourseId || !createEmail) return;
    setCreateSubmitting(true);
    setNotice(null);
    try {
      await createCourseAccessInvitation(
        selectedCourseId,
        createEmail,
        createNote || undefined,
        createRef || undefined,
        locale,
      );
      setNotice({ tone: "success", text: copy.invite.sent });
      setCreateEmail("");
      setCreateNote("");
      setCreateRef("");
      await fetchInvitations();
    } catch (cause) {
      setNotice({ tone: "error", text: describeApiError(cause, locale) || copy.invite.failed });
    } finally {
      setCreateSubmitting(false);
    }
  };

  /**
   * Every confirmed queue decision runs through here.
   *
   * One path, so all four obey the same rule: the API is called exactly once, the queue is re-read
   * from the server rather than patched locally, and a refusal is reported as a refusal instead of
   * leaving a success notice standing over a change that did not happen.
   */
  const confirmQueueAction = async () => {
    if (!pending || pending.kind === "revoke" || busy) return;
    const { kind, invitation } = pending;
    setBusy(true);
    setNotice(null);
    try {
      if (kind === "approve") {
        await approveCourseAccessInvitation(invitation.id, locale);
        setNotice({ tone: "success", text: copy.queue.approved });
      } else if (kind === "reject") {
        await rejectCourseAccessInvitation(invitation.id, rejectReason.trim(), locale);
        setNotice({ tone: "success", text: copy.queue.rejected });
        setRejectReason("");
      } else if (kind === "resend") {
        await resendCourseAccessInvitation(invitation.id, locale);
        setNotice({ tone: "success", text: copy.queue.resent });
      } else {
        await cancelCourseAccessInvitation(invitation.id, locale);
        setNotice({ tone: "success", text: copy.queue.cancelled });
      }
      await fetchInvitations();
    } catch (cause) {
      setNotice({ tone: "error", text: describeApiError(cause, locale) || copy.genericFailure });
    } finally {
      setBusy(false);
      setPending(null);
    }
  };

  const resetEntitlementForms = () => {
    setDetailNotice(null);
    setAdjustDate("");
    setAdjustReason("");
    setAdjustSupportRef("");
    setRevokeReason("");
    setRevokeSupportRef("");
  };

  const handleViewEntitlement = async (entitlementId: string) => {
    setDetailLoading(true);
    resetEntitlementForms();
    try {
      const loaded = await getAdminEntitlementDetail(entitlementId, locale);
      if (loaded) setDetail(loaded);
    } catch (cause) {
      setNotice({
        tone: "error",
        text: describeApiError(cause, locale) || copy.entitlement.loadFailed,
      });
    } finally {
      setDetailLoading(false);
    }
  };

  const closeEntitlementDetail = () => {
    setDetail(null);
    resetEntitlementForms();
  };

  /**
   * One expiry adjustment. The direction is the server's business: a later date extends access, an
   * earlier one shortens it, and a past date ends it immediately. The Admin supplies the date and
   * the required reason.
   */
  const handleAdjustExpiry = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!detail || !adjustDate || !adjustReason.trim() || detailBusy) return;
    setDetailBusy(true);
    setDetailNotice(null);
    try {
      const updated = await adjustEntitlementExpiry(
        detail.entitlement.id,
        adjustDate,
        adjustReason.trim(),
        {
          supportReference: adjustSupportRef.trim() || undefined,
          // The revision the Admin is looking at. A grant changed by someone else in the meantime
          // is refused, not silently overwritten.
          expectedRevision: detail.entitlement.revision,
        },
        locale,
      );
      if (updated) setDetail(updated);
      setDetailNotice({ tone: "success", text: copy.entitlement.adjusted });
      setAdjustDate("");
      setAdjustReason("");
      setAdjustSupportRef("");
      await fetchInvitations();
    } catch (cause) {
      setDetailNotice({
        tone: "error",
        text: describeApiError(cause, locale) || copy.entitlement.adjustFailed,
      });
    } finally {
      setDetailBusy(false);
    }
  };

  const handleRevokeEntitlement = async () => {
    if (!detail || !revokeReason.trim() || detailBusy) return;
    setDetailBusy(true);
    setDetailNotice(null);
    try {
      const updated = await revokeEntitlement(
        detail.entitlement.id,
        revokeReason.trim(),
        {
          supportReference: revokeSupportRef.trim() || undefined,
          expectedRevision: detail.entitlement.revision,
        },
        locale,
      );
      if (updated) setDetail(updated);
      setDetailNotice({ tone: "success", text: copy.entitlement.revoked });
      setRevokeReason("");
      setRevokeSupportRef("");
      await fetchInvitations();
    } catch (cause) {
      setDetailNotice({
        tone: "error",
        text: describeApiError(cause, locale) || copy.entitlement.revokeFailed,
      });
    } finally {
      setDetailBusy(false);
      setPending(null);
    }
  };

  const queueDialog =
    pending && pending.kind !== "revoke"
      ? {
          approve: {
            title: copy.queue.approveTitle,
            body: copy.queue.approveBody,
            confirmLabel: copy.queue.approveAccept,
            tone: "default" as const,
          },
          reject: {
            title: copy.queue.rejectTitle,
            body: copy.queue.rejectBody,
            confirmLabel: copy.queue.rejectAccept,
            tone: "destructive" as const,
          },
          resend: {
            title: copy.queue.resendTitle,
            body: copy.queue.resendBody,
            confirmLabel: copy.queue.resendAccept,
            tone: "default" as const,
          },
          cancel: {
            title: copy.queue.cancelTitle,
            body: copy.queue.cancelBody,
            confirmLabel: copy.queue.cancelAccept,
            tone: "destructive" as const,
          },
        }[pending.kind]
      : null;

  const entitlement = detail?.entitlement;
  const active = entitlement?.state === "ACTIVE";

  return (
    // The page's own landmark, and the target the skip link jumps to. `WorkspacePage` decides width,
    // gutters and direction; it is deliberately not a landmark, because a screen composes one.
    <main id="main">
      <WorkspacePage testID="course-access-workspace">
        <WorkspacePageHeader title={copy.title} description={copy.intro} />

      {notice ? (
        <div className="mt-6" data-testid="course-access-notice" data-tone={notice.tone}>
          <Alert tone={notice.tone} title={notice.text} />
        </div>
      ) : null}

      <PurchaseRequestsPanel />

      <WorkspaceSection title={copy.course.title} description={copy.course.lead}>
        <PublishedCourseSelector
          options={courseOptions}
          loading={coursesLoading}
          error={coursesError}
          selectedCourseID={selectedCourseId}
          onSelect={setSelectedCourseId}
          onRetry={fetchCourses}
        />
      </WorkspaceSection>

      <div className="grid gap-8 md:grid-cols-2">
        <WorkspaceSection title={copy.expiry.title} description={copy.expiry.lead}>
          <p className="text-sm text-muted-foreground" data-testid="expiry-course-context">
            {selectedCourse
              ? `${copy.expiry.appliesTo}: ${selectedCourse.title}`
              : copy.course.none}
          </p>
          <form onSubmit={handleSetExpiry} className="mt-4 space-y-4">
            <Field htmlFor="access-expiry-date" label={copy.expiry.date}>
              <Input
                id="access-expiry-date"
                type="date"
                required
                value={expiryDate}
                onChange={(event) => setExpiryDate(event.target.value)}
              />
            </Field>
            <Field
              htmlFor="access-expiry-reason"
              label={copy.expiry.reason}
              hint={copy.expiry.reasonHint}
            >
              <Input
                id="access-expiry-reason"
                required
                value={expiryReason}
                onChange={(event) => setExpiryReason(event.target.value)}
                placeholder={copy.expiry.reasonPlaceholder}
              />
            </Field>
            <Button
              type="submit"
              data-testid="access-expiry-submit"
              disabled={expirySubmitting || !selectedCourseId}
            >
              {expirySubmitting ? copy.expiry.saving : copy.expiry.submit}
            </Button>
          </form>
        </WorkspaceSection>

        <WorkspaceSection title={copy.invite.title} description={copy.invite.lead}>
          <p className="text-sm text-muted-foreground" data-testid="invitation-course-context">
            {selectedCourse
              ? `${copy.expiry.appliesTo}: ${selectedCourse.title}`
              : copy.course.none}
          </p>
          <form onSubmit={handleCreateInvitation} className="mt-4 space-y-4">
            <Field
              htmlFor="access-invite-email"
              label={copy.invite.email}
              hint={copy.invite.emailHint}
            >
              <Input
                id="access-invite-email"
                type="email"
                required
                value={createEmail}
                onChange={(event) => setCreateEmail(event.target.value)}
              />
            </Field>
            <Field htmlFor="access-invite-note" label={copy.invite.note} hint={copy.invite.noteHint}>
              <Input
                id="access-invite-note"
                value={createNote}
                onChange={(event) => setCreateNote(event.target.value)}
                placeholder={copy.invite.notePlaceholder}
              />
            </Field>
            <Field
              htmlFor="access-invite-reference"
              label={copy.invite.reference}
              hint={copy.invite.referenceHint}
            >
              <Input
                id="access-invite-reference"
                value={createRef}
                onChange={(event) => setCreateRef(event.target.value)}
                placeholder={copy.invite.referencePlaceholder}
              />
            </Field>
            {/* Named, because "Send invitation" is a substring of the purchase panel's "Confirm
                payment & send invitation" and a text-matching selector reached the wrong one. */}
            <Button
              type="submit"
              data-testid="access-invite-submit"
              disabled={createSubmitting || !selectedCourseId}
            >
              {createSubmitting ? copy.invite.sending : copy.invite.submit}
            </Button>
          </form>
        </WorkspaceSection>
      </div>

      <WorkspaceSection
        title={copy.queue.title}
        description={copy.queue.lead}
        testID="access-queue"
        actions={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void fetchInvitations()}
            disabled={queueLoad === "loading"}
          >
            {copy.refresh}
          </Button>
        }
      >
        {queueLoad === "loading" ? (
          <LoadingState label={copy.queue.loading} testID="access-queue-loading" />
        ) : null}
        {queueLoad === "failed" ? (
          <ErrorState
            title={copy.queue.loadFailed}
            retryLabel={copy.queue.retry}
            onRetry={() => void fetchInvitations()}
            testID="access-queue-failed"
          />
        ) : null}
        {queueLoad === "ready" && invitations.length === 0 ? (
          <EmptyState
            density="compact"
            title={copy.queue.emptyTitle}
            description={copy.queue.emptyBody}
            testID="access-queue-empty"
          />
        ) : null}
        {queueLoad === "ready" && invitations.length > 0 ? (
          <>
            {/* How much of the list this is, and what each state means — said once, above a table
                that would otherwise repeat the same sentence on every one of its rows. */}
            <p className="mb-3 text-sm text-muted-foreground" data-testid="access-queue-bound">
              {queueTotal > invitations.length
                ? copy.queue.bounded
                    .replace("{shown}", String(invitations.length))
                    .replace("{total}", String(queueTotal))
                : copy.queue.complete.replace("{total}", String(invitations.length))}
            </p>
            <dl className="mb-4 grid gap-x-6 gap-y-1 text-sm sm:grid-cols-2 lg:grid-cols-3">
              {(
                Object.keys(copy.queue.status) as (keyof typeof copy.queue.status)[]
              ).map((state) => (
                <div key={state} className="flex flex-wrap gap-x-2">
                  <dt className="font-semibold text-foreground">{copy.queue.status[state]}</dt>
                  <dd className="min-w-0 flex-1 text-muted-foreground">
                    {copy.queue.statusDetail[state]}
                  </dd>
                </div>
              ))}
            </dl>
            <TableContainer>
            <Table>
              <TableCaption>{copy.queue.caption}</TableCaption>
              <TableHead>
                <TableRow>
                  <TableHeaderCell scope="col">{copy.queue.student}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.queue.course}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.queue.state}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.queue.when}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.queue.actions}</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {invitations.map((inv) => {
                  const lastChange =
                    inv.decided_at ?? inv.cancelled_at ?? inv.accepted_at ?? inv.created_at;
                  return (
                    <TableRow key={inv.id} data-testid="access-invitation-row">
                      {/* An invitation is named by the person it was sent to. Its identifier is what
                          the API is called with and what this row's test id is built from — it is
                          not something an Admin should ever have to read or repeat. A `td` rather
                          than a row header because the queue is scanned by person, and the
                          harness locates a row by the address in its cells. */}
                      <TableCell>
                        <bdi className="font-medium text-foreground">{inv.email}</bdi>
                      </TableCell>
                      <TableCell data-testid={`invitation-course-${inv.id}`}>
                        {courseLabel(inv.course_id)}
                      </TableCell>
                      <TableCell>
                        <StatusBadge
                          tone={INVITATION_TONE[inv.state]}
                          label={copy.queue.status[inv.state]}
                          labelTestID="access-invitation-state"
                        />
                        {inv.decision_reason ? (
                          <p className="mt-1 text-xs text-muted-foreground">
                            {copy.queue.reason}: {inv.decision_reason}
                          </p>
                        ) : null}
                      </TableCell>
                      <TableCell>{formatDate(lastChange, locale)}</TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-2">
                          {inv.state === "PENDING_ADMIN_APPROVAL" ? (
                            <>
                              <Button
                                type="button"
                                size="sm"
                                disabled={busy}
                                onClick={() => setPending({ kind: "approve", invitation: inv })}
                                aria-label={`${copy.queue.approve} — ${inv.email}`}
                              >
                                {copy.queue.approve}
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                disabled={busy}
                                onClick={() => {
                                  setRejectReason("");
                                  setPending({ kind: "reject", invitation: inv });
                                }}
                                aria-label={`${copy.queue.reject} — ${inv.email}`}
                              >
                                {copy.queue.reject}
                              </Button>
                            </>
                          ) : null}

                          {inv.entitlement_id ? (
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              disabled={detailLoading}
                              data-testid={`manage-access-${inv.id}`}
                              onClick={() =>
                                void handleViewEntitlement(inv.entitlement_id as string)
                              }
                              aria-label={`${copy.queue.manage} — ${inv.email}`}
                            >
                              {detailLoading ? copy.queue.opening : copy.queue.manage}
                            </Button>
                          ) : null}

                          {inv.state === "PENDING_STUDENT_ACCEPTANCE" ? (
                            <>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                disabled={busy}
                                onClick={() => setPending({ kind: "resend", invitation: inv })}
                                aria-label={`${copy.queue.resend} — ${inv.email}`}
                              >
                                {copy.queue.resend}
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                disabled={busy}
                                onClick={() => setPending({ kind: "cancel", invitation: inv })}
                                aria-label={`${copy.queue.cancel} — ${inv.email}`}
                              >
                                {copy.queue.cancel}
                              </Button>
                            </>
                          ) : null}
                        </div>
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

      {queueDialog && pending && pending.kind !== "revoke" ? (
        <ConfirmDialog
          open
          onOpenChange={(next) => {
            if (!next && !busy) setPending(null);
          }}
          title={queueDialog.title}
          body={queueDialog.body}
          confirmLabel={queueDialog.confirmLabel}
          cancelLabel={copy.queue.keep}
          tone={queueDialog.tone}
          busy={busy}
          confirmDisabled={pending.kind === "reject" && rejectReason.trim() === ""}
          onConfirm={() => void confirmQueueAction()}
          testID="access-queue-confirm"
        >
          {pending.kind === "reject" ? (
            <Field
              htmlFor="access-reject-reason"
              label={copy.queue.rejectReason}
              hint={copy.queue.rejectReasonHint}
            >
              <Textarea
                id="access-reject-reason"
                data-testid="access-reject-reason"
                rows={3}
                value={rejectReason}
                onChange={(event) => setRejectReason(event.target.value)}
                disabled={busy}
              />
            </Field>
          ) : null}
        </ConfirmDialog>
      ) : null}

      {/* The access record. A sheet rather than the hand-rolled fixed overlay this screen used to
          paint: that one trapped no focus, closed on no key, and scrolled the page behind it. */}
      <Sheet
        open={detail !== null}
        onOpenChange={(next) => {
          if (!next && !detailBusy) closeEntitlementDetail();
        }}
      >
        {/* Wider than the navigation drawer this primitive was built for, because the record it
            holds is a pair of forms rather than a list of links. Composed rather than forked: the
            focus trap, the escape key, the overlay and the labelled close control are exactly what
            the hand-rolled `fixed inset-0` panel this replaces had none of. */}
        <SheetContent
          side="right"
          className="w-[min(38rem,94vw)] max-w-none overflow-y-auto"
          closeLabel={copy.entitlement.close}
          data-testid="entitlement-detail"
        >
          <SheetTitle>{copy.entitlement.title}</SheetTitle>
          {entitlement ? (
          <div className="space-y-6">
            <dl className="space-y-3 rounded-lg border border-border bg-muted/40 p-4 text-sm">
              <div className="flex flex-wrap gap-x-2">
                <dt className="font-semibold text-foreground">{copy.entitlement.course}</dt>
                <dd className="text-muted-foreground">{courseLabel(entitlement.course_id)}</dd>
              </div>
              <div className="flex flex-wrap items-center gap-x-2">
                <dt className="font-semibold text-foreground">{copy.entitlement.state}</dt>
                <dd>
                  <StatusBadge
                    tone={active ? "success" : "neutral"}
                    label={copy.entitlement.status[entitlement.state]}
                    labelTestID="entitlement-state"
                  />
                </dd>
              </div>
              <div className="flex flex-wrap gap-x-2" data-testid="entitlement-access-ends-at">
                <dt className="font-semibold text-foreground">{copy.entitlement.endsAt}</dt>
                <dd className="text-muted-foreground">
                  {formatDateTime(entitlement.access_ends_at, locale)}
                </dd>
              </div>
              <div className="flex flex-wrap gap-x-2">
                <dt className="font-semibold text-foreground">{copy.entitlement.originally}</dt>
                <dd className="text-muted-foreground">
                  {formatDateTime(entitlement.original_access_ends_at, locale)}
                </dd>
              </div>
              {entitlement.revoked_at ? (
                <div className="flex flex-wrap gap-x-2" data-testid="entitlement-revoked-at">
                  <dt className="font-semibold text-foreground">{copy.entitlement.revokedOn}</dt>
                  <dd className="text-muted-foreground">
                    {formatDateTime(entitlement.revoked_at, locale)}
                  </dd>
                </div>
              ) : null}
              <div className="flex flex-wrap gap-x-2">
                <dt className="font-semibold text-foreground">{copy.entitlement.source}</dt>
                <dd className="text-muted-foreground">
                  {/* Two grant sources exist on the contract, and both have words. An unrecognised
                      one degrades to the Course rather than to the enum. */}
                  {copy.entitlement.grantSource[
                    entitlement.grant_source as keyof typeof copy.entitlement.grantSource
                  ] ?? courseLabel(entitlement.course_id)}
                </dd>
              </div>
            </dl>

            {detailNotice ? (
              <div data-testid="entitlement-notice" data-tone={detailNotice.tone}>
                <Alert tone={detailNotice.tone} title={detailNotice.text} />
              </div>
            ) : null}

            {active ? (
              <>
                <form
                  onSubmit={handleAdjustExpiry}
                  className="space-y-4 rounded-lg border border-border p-4"
                  data-testid="entitlement-expiry-form"
                >
                  <div>
                    <h3 className="font-display text-base font-bold text-foreground">
                      {copy.entitlement.adjustTitle}
                    </h3>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {copy.entitlement.adjustLead}
                    </p>
                  </div>
                  <Field htmlFor="entitlement-expiry-date" label={copy.entitlement.adjustDate}>
                    <Input
                      id="entitlement-expiry-date"
                      type="date"
                      required
                      value={adjustDate}
                      onChange={(event) => setAdjustDate(event.target.value)}
                    />
                  </Field>
                  <Field
                    htmlFor="entitlement-expiry-reason"
                    label={copy.entitlement.adjustReason}
                    hint={copy.entitlement.adjustReasonHint}
                  >
                    <Input
                      id="entitlement-expiry-reason"
                      required
                      value={adjustReason}
                      onChange={(event) => setAdjustReason(event.target.value)}
                      placeholder={copy.entitlement.adjustReasonPlaceholder}
                    />
                  </Field>
                  <Field
                    htmlFor="entitlement-expiry-reference"
                    label={copy.entitlement.supportReference}
                    hint={copy.entitlement.supportReferenceHint}
                  >
                    <Input
                      id="entitlement-expiry-reference"
                      value={adjustSupportRef}
                      onChange={(event) => setAdjustSupportRef(event.target.value)}
                    />
                  </Field>
                  <Button
                    type="submit"
                    data-testid="save-entitlement-expiry"
                    disabled={detailBusy || !adjustDate || adjustReason.trim() === ""}
                  >
                    {detailBusy ? copy.entitlement.adjustSaving : copy.entitlement.adjustSubmit}
                  </Button>
                </form>

                <div
                  className="space-y-4 rounded-lg border border-destructive/25 p-4"
                  data-testid="entitlement-revoke-form"
                >
                  <div>
                    <h3 className="font-display text-base font-bold text-foreground">
                      {copy.entitlement.revokeTitle}
                    </h3>
                    <p className="mt-1 text-sm text-muted-foreground">
                      {copy.entitlement.revokeLead}
                    </p>
                  </div>
                  <Field
                    htmlFor="entitlement-revoke-reason"
                    label={copy.entitlement.revokeReason}
                    hint={copy.entitlement.revokeReasonHint}
                  >
                    <Input
                      id="entitlement-revoke-reason"
                      required
                      value={revokeReason}
                      onChange={(event) => setRevokeReason(event.target.value)}
                      placeholder={copy.entitlement.revokeReasonPlaceholder}
                    />
                  </Field>
                  <Field
                    htmlFor="entitlement-revoke-reference"
                    label={copy.entitlement.supportReference}
                    hint={copy.entitlement.supportReferenceHint}
                  >
                    <Input
                      id="entitlement-revoke-reference"
                      value={revokeSupportRef}
                      onChange={(event) => setRevokeSupportRef(event.target.value)}
                    />
                  </Field>
                  {/* Ending access is irreversible, so it is confirmed rather than gated behind a
                      checkbox the reader ticks in the same breath as pressing the button. */}
                  <Button
                    type="button"
                    variant="destructive"
                    data-testid="revoke-entitlement"
                    disabled={detailBusy || revokeReason.trim() === ""}
                    onClick={() => setPending({ kind: "revoke" })}
                  >
                    {detailBusy ? copy.entitlement.revoking : copy.entitlement.revokeSubmit}
                  </Button>
                </div>
              </>
            ) : (
              <div data-testid="entitlement-terminal">
                <Alert
                  tone="info"
                  title={
                    entitlement.state === "REVOKED"
                      ? copy.entitlement.terminalRevoked
                      : copy.entitlement.terminalExpired
                  }
                />
              </div>
            )}

            <section aria-labelledby="entitlement-history-heading">
              <h3
                id="entitlement-history-heading"
                className="font-display text-base font-bold text-foreground"
              >
                {copy.entitlement.historyTitle}
              </h3>
              {detail.adjustments.length === 0 ? (
                <p className="mt-2 text-sm text-muted-foreground">
                  {copy.entitlement.historyEmpty}
                </p>
              ) : (
                <ul className="mt-3 space-y-2">
                  {detail.adjustments.map((adjustment) => (
                    <li
                      key={adjustment.id}
                      className="rounded-md border border-border p-3 text-sm text-muted-foreground"
                    >
                      <p>
                        {copy.entitlement.historyWhen}:{" "}
                        {formatDateTime(adjustment.adjusted_at, locale)}
                      </p>
                      <p>
                        {copy.entitlement.historyReason}: {adjustment.reason}
                      </p>
                      <p>
                        {copy.entitlement.historyNewEnd}:{" "}
                        {formatDateTime(adjustment.new_access_ends_at, locale)}
                      </p>
                    </li>
                  ))}
                </ul>
              )}
            </section>
            </div>
          ) : null}
        </SheetContent>
      </Sheet>

      {pending?.kind === "revoke" ? (
        <ConfirmDialog
          open
          onOpenChange={(next) => {
            if (!next && !detailBusy) setPending(null);
          }}
          title={copy.entitlement.revokeConfirmTitle}
          body={copy.entitlement.revokeConfirmBody}
          confirmLabel={copy.entitlement.revokeConfirmAccept}
          cancelLabel={copy.entitlement.keep}
          tone="destructive"
          busy={detailBusy}
          onConfirm={() => void handleRevokeEntitlement()}
          testID="confirm-revoke-entitlement"
        />
        ) : null}
      </WorkspacePage>
    </main>
  );
}
