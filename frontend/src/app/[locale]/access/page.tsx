"use client";

import { useEffect, useState, useCallback, useRef } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import Link from "next/link";
import { useLocale } from "@/lib/i18n/locale-provider";
import { localeFromPath } from "@/lib/i18n/locale-path";
import { safeReturnTo } from "@/lib/identity/return-to";
import {
  captureTokenFromFragment,
  releaseCourseAccessInvitationContext,
  releaseFragmentToken,
  restoreCourseAccessInvitationContext,
  scrubTokenFragment,
  retainCourseAccessInvitationContext,
} from "@/lib/identity/validation";
import {
  StudentCourseAccessInvitation,
  StudentCourseAccessHistoryItem,
  getStudentCourseAccessInvitation,
  acceptStudentCourseAccessInvitation,
  getStudentCourseAccessHistory,
} from "@/lib/api/access";
import { ProblemError } from "@/lib/api/problem";
import { AccessRecord } from "@/components/access/access-records";
import { byStudentPriority } from "@/components/access/access-state";

/**
 * Student Course access.
 *
 * Two surfaces, deliberately separate:
 *
 *  - the **invitation action** at the top, which needs the one-time fragment token and only appears
 *    when the Student arrived from an invitation link; and
 *  - the **persistent record list**, which is plain authenticated Student data from
 *    `GET /me/course-access`. That endpoint derives the Student from the session and takes no
 *    identifier, so this page is fully usable after the invitation link has been consumed — which
 *    is the whole point: a Student who accepted last week can come back and see what happened.
 */
export default function StudentCourseAccessPage() {
  const { locale, dir, t } = useLocale();
  const labels = t.access;
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  // The LocaleProvider restores its language after mount. Invitation return
  // paths are security- and workflow-critical, so obtain a locale-addressable
  // `/[locale]/access` choice synchronously from the URL instead of allowing
  // the provider's Arabic default to win the first unauthenticated redirect.
  const routeLocale = localeFromPath(pathname) ?? locale;

  const invitationId =
    searchParams.get("invitation_id") || searchParams.get("id");
  const token = useRef<string | null>(null);

  const [activeInvitation, setActiveInvitation] =
    useState<StudentCourseAccessInvitation | null>(null);
  const [historyItems, setHistoryItems] = useState<
    StudentCourseAccessHistoryItem[]
  >([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [submitting, setSubmitting] = useState<boolean>(false);
  const [historyError, setHistoryError] = useState<string | null>(null);
  const [invitationError, setInvitationError] = useState<string | null>(null);
  const [accepted, setAccepted] = useState<boolean>(false);

  useEffect(() => {
    if (!invitationId) return;
    const captureInvitationToken = () => {
      const captured = captureTokenFromFragment("COURSE_ACCESS_INVITATION");
      if (captured) {
        token.current = captured;
        retainCourseAccessInvitationContext(invitationId, captured);
      } else {
        token.current = restoreCourseAccessInvitationContext(invitationId);
      }
      scrubTokenFragment();
    };
    captureInvitationToken();
    window.addEventListener("hashchange", captureInvitationToken);
    return () =>
      window.removeEventListener("hashchange", captureInvitationToken);
  }, [invitationId]);

  const fetchStudentData = useCallback(async () => {
    setLoading(true);
    setHistoryError(null);

    if (invitationId) {
      try {
        setActiveInvitation(
          await getStudentCourseAccessInvitation(invitationId, routeLocale),
        );
      } catch (cause: unknown) {
        setInvitationError(
          cause instanceof ProblemError && cause.problem.status === 404
            ? labels.invitation.notFound
            : labels.invitation.failed,
        );
      }
    }

    try {
      const history = await getStudentCourseAccessHistory(routeLocale);
      setHistoryItems(history?.items ?? []);
    } catch (cause: unknown) {
      // An expired session is recoverable: send the Student to sign in and bring them back here.
      if (cause instanceof ProblemError && cause.problem.status === 401) {
        const accessPath = invitationId
          ? `/${routeLocale}/access?invitation_id=${encodeURIComponent(invitationId)}`
          : `/${routeLocale}/access`;
        const target = safeReturnTo(accessPath) || `/${routeLocale}/access`;
        router.push(`/login?returnTo=${encodeURIComponent(target)}`);
        return;
      }
      setHistoryError(labels.failed);
    } finally {
      setLoading(false);
    }
  }, [invitationId, labels, routeLocale, router]);

  useEffect(() => {
    void fetchStudentData();
  }, [fetchStudentData]);

  const handleAccept = async () => {
    if (!invitationId || !token.current) {
      setInvitationError(labels.invitation.missingToken);
      return;
    }
    setSubmitting(true);
    setInvitationError(null);
    try {
      const updated = await acceptStudentCourseAccessInvitation(
        invitationId,
        token.current,
        routeLocale,
      );
      if (!updated) {
        throw new Error("Invitation acceptance returned no invitation state.");
      }
      token.current = null;
      releaseFragmentToken("COURSE_ACCESS_INVITATION");
      releaseCourseAccessInvitationContext();
      setActiveInvitation(updated);
      setAccepted(true);
      // Only a server-confirmed APPROVED result reaches this branch. Standard
      // invitations still return PENDING_ADMIN_APPROVAL; a purchase-backed one
      // has atomically created its entitlement and can go straight to Course
      // Home without carrying the fragment token anywhere.
      if (updated.state === "APPROVED") {
        router.replace(`/${routeLocale}/learn/courses/${updated.course_id}`);
        return;
      }
      await fetchStudentData();
    } catch (cause: unknown) {
      if (cause instanceof ProblemError) {
        if (cause.problem.status === 410) {
          token.current = null;
          releaseFragmentToken("COURSE_ACCESS_INVITATION");
          releaseCourseAccessInvitationContext();
          setInvitationError(labels.invitation.expired);
        } else if (cause.problem.status === 409) {
          setInvitationError(labels.invitation.wrongState);
        } else if (cause.problem.status === 404) {
          token.current = null;
          releaseFragmentToken("COURSE_ACCESS_INVITATION");
          releaseCourseAccessInvitationContext();
          setInvitationError(labels.invitation.notFound);
        } else {
          setInvitationError(labels.invitation.failed);
        }
      } else {
        setInvitationError(labels.invitation.failed);
      }
    } finally {
      setSubmitting(false);
    }
  };

  const awaitingAcceptance =
    activeInvitation?.state === "PENDING_STUDENT_ACCEPTANCE";
  const records = [...historyItems].sort(byStudentPriority);

  return (
    <main
      id="main"
      dir={dir}
      className="mx-auto min-h-screen max-w-3xl px-5 py-10 sm:px-6"
    >
      <header className="mb-8">
        <h1 className="font-display text-3xl font-bold text-foreground">
          {labels.title}
        </h1>
        <p className="mt-2 text-muted-foreground">{labels.intro}</p>
      </header>

      {/* The token-dependent half. Present only when the Student followed an invitation link. */}
      {invitationId ? (
        <section
          data-testid="access-invitation-panel"
          aria-labelledby="access-invitation-heading"
          className="mb-8 rounded-lg border border-border bg-card p-5"
        >
          <h2
            id="access-invitation-heading"
            className="font-display text-lg font-bold text-foreground"
          >
            {labels.invitation.heading}
          </h2>

          {invitationError ? (
            <p
              role="alert"
              data-testid="access-invitation-error"
              className="mt-3 text-sm font-medium text-destructive"
            >
              {invitationError}
            </p>
          ) : null}

          {accepted ? (
            <div
              role="status"
              data-testid="access-invitation-accepted"
              className="mt-3"
            >
              <p className="font-semibold text-foreground">
                {labels.invitation.acceptedTitle}
              </p>
              <p className="mt-1 leading-6 text-muted-foreground">
                {labels.invitation.acceptedBody}
              </p>
            </div>
          ) : awaitingAcceptance ? (
            <div className="mt-3">
              {/* Acceptance is not a grant. Said before the control, not after it. */}
              <p className="leading-6 text-muted-foreground">
                {labels.invitation.acceptNote}
              </p>
              <button
                type="button"
                data-testid="accept-invitation"
                onClick={handleAccept}
                disabled={submitting}
                className="mt-4 rounded-md bg-primary px-5 py-2 font-semibold text-primary-foreground disabled:opacity-50"
              >
                {submitting
                  ? labels.invitation.accepting
                  : labels.invitation.accept}
              </button>
            </div>
          ) : null}
        </section>
      ) : null}

      {loading ? (
        <p
          aria-live="polite"
          data-testid="access-loading"
          className="text-muted-foreground"
        >
          {labels.loading}
        </p>
      ) : historyError ? (
        <div role="alert" data-testid="access-error">
          <p className="text-sm font-medium text-destructive">{historyError}</p>
          <button
            type="button"
            onClick={() => void fetchStudentData()}
            className="mt-3 rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent"
          >
            {labels.retry}
          </button>
        </div>
      ) : records.length === 0 ? (
        <section
          data-testid="access-empty"
          className="rounded-lg border border-dashed border-border bg-card px-6 py-12 text-center"
        >
          <h2 className="font-display text-xl font-bold text-foreground">
            {labels.emptyTitle}
          </h2>
          <p className="mx-auto mt-2 max-w-md text-muted-foreground">
            {labels.emptyBody}
          </p>
          <Link
            href={`/${locale}/catalog`}
            className="mt-6 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent"
          >
            {labels.emptyAction}
          </Link>
        </section>
      ) : (
        <section
          aria-label={labels.title}
          data-testid="access-records"
          className="space-y-4"
        >
          {records.map((item) => (
            <AccessRecord
              key={item.course_id}
              item={item}
              labels={labels}
              locale={locale}
            />
          ))}
        </section>
      )}
    </main>
  );
}
