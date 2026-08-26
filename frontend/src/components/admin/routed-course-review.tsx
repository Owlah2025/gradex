"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { listReviewQueue, type ReviewQueueItem } from "@/lib/api/review";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { SubmittedRevisionInspector } from "./submitted-revision-inspector";

/**
 * The review workspace for one Course, at its own address.
 *
 * The workspace itself already existed, but only as `useState` inside the queue screen: there was no
 * URL for reviewing a particular Course, so a review could not be linked, bookmarked, reloaded or
 * returned to with the browser's Back button, and a refresh mid-review discarded the whole context.
 * Giving it a route is what lets the Courses directory hand an Admin straight to the right Course
 * rather than asking them to find it again in a queue.
 *
 * The Course is addressed by the identifier already in the path. Resolving *which submitted
 * revision* a decision applies to stays the server's answer: the queue is read and the entry for
 * this Course is used. A Course with no entry is not an error — it means no decision is pending, and
 * that is reported as a state with a way back, never as a failure.
 */
export function RoutedCourseReview({ courseID }: { courseID: string }) {
  const { locale, dir, t } = useLocale();
  const copy = t.adminReview;
  const router = useRouter();

  const [item, setItem] = useState<ReviewQueueItem | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "not-pending" | "failed">("loading");
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [decided, setDecided] = useState(false);

  const coursesHref = `/${locale}/admin/courses`;

  const load = useCallback(async () => {
    setState("loading");
    setError("");
    try {
      const queue = await listReviewQueue(locale);
      const pending = queue
        .filter((entry) => entry.course_id === courseID)
        // At most one candidate is open per Course; if that ever changes, a decision belongs to the
        // newest submitted revision rather than to whichever row arrived first.
        .sort((a, b) => b.revision_number - a.revision_number)[0];
      if (!pending) {
        setItem(null);
        setState("not-pending");
        return;
      }
      setItem(pending);
      setState("ready");
    } catch (cause) {
      setError(describeApiError(cause, locale));
      setState("failed");
    }
  }, [courseID, locale]);

  useEffect(() => {
    void load();
  }, [load, attempt]);

  return (
    <div dir={dir} className="mx-auto max-w-container px-5 py-8 sm:px-6">
      {/* A route back that does not depend on browser history, so the workspace is never a dead end
          when it was opened from a link or a reload. */}
      <nav aria-label={copy.breadcrumb} className="mb-6">
        <Link
          href={coursesHref}
          data-testid="review-back-to-courses"
          className="text-sm font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {copy.backToCourses}
        </Link>
      </nav>

      {state === "loading" && (
        <p className="text-muted-foreground" aria-live="polite" data-testid="review-loading">
          {copy.loading}
        </p>
      )}

      {state === "not-pending" && (
        <div data-testid="review-not-pending">
          <Alert tone="info" title={copy.notPending.title}>
            <p className="mb-3">{copy.notPending.body}</p>
            <Button asChild variant="outline" size="sm">
              <Link href={coursesHref}>{copy.backToCourses}</Link>
            </Button>
          </Alert>
        </div>
      )}

      {state === "failed" && (
        <div data-testid="review-load-failed">
          <Alert tone="error" title={copy.loadFailed}>
            <p className="mb-3">{error}</p>
            <Button variant="outline" size="sm" onClick={() => setAttempt((value) => value + 1)}>
              {copy.retry}
            </Button>
          </Alert>
        </div>
      )}

      {/* A recorded decision is confirmed here rather than by navigating away from it.
          Redirecting on success destroyed the very message that told the Admin what happened, and
          left them guessing whether the approval had landed. The Course leaves the queue either
          way; the directory re-reads both the queue and the lifecycle directory when it mounts, so
          returning shows the new state from the server rather than from anything held here. */}
      {decided && (
        <div className="mb-5" data-testid="review-decision-recorded">
          <Alert tone="success" title={copy.reviewedNotice}>
            <Button asChild variant="outline" size="sm">
              <Link href={coursesHref}>{copy.backToCourses}</Link>
            </Button>
          </Alert>
        </div>
      )}

      {state === "ready" && item && (
        <SubmittedRevisionInspector
          key={item.revision_id}
          item={item}
          // Close means "leave this Course", which is a navigation now that the workspace is a
          // route rather than a panel that could simply be unmounted.
          onClose={() => router.push(coursesHref)}
          onReviewed={async () => {
            setDecided(true);
          }}
        />
      )}
    </div>
  );
}
