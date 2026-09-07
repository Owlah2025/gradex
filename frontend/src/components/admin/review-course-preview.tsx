"use client";

import { useRef, useState } from "react";
import { Play } from "lucide-react";
import { describeApiError } from "@/lib/api/api-error";
import { previewAdminCoursePreview } from "@/lib/api/review";
import { issuedPreviewMatches } from "./review-preview-identity";
import { ErrorState } from "@/components/common/error-state";

/**
 * The candidate revision's own public preview, played by the Admin reviewing it.
 *
 * # WHY THIS EXISTS
 *
 * Before this, an Admin reviewing a submitted Course could read that a public preview was attached
 * and could not watch it: the only route that signs a preview is the public one, which correctly
 * requires the live, `APPROVED` revision. So the one asset every visitor would see first was the
 * one asset the reviewer had to approve unseen.
 *
 * # WHAT IT REFUSES
 *
 * The response names the Course, the revision and the Asset Version it signed, and all three are
 * compared against what this screen is displaying before any media element is created. Any
 * disagreement is a failure and nothing is mounted — the same defensive rule the Lesson preview
 * applies, and the reason it exists: an Admin must never approve one revision while watching
 * another's media.
 *
 * The expected Asset Version is a required prop rather than something read back out of the
 * response, because a response compared against itself proves nothing.
 *
 * The URL itself is expiry-bounded and is minted on activation, never on render.
 */
export function ReviewCoursePreview({
  courseID,
  revisionID,
  previewAssetVersionID,
  locale,
  csrf,
  labels,
}: {
  courseID: string;
  revisionID: string;
  /** The Asset Version this revision says it owns, read from the graph the inspector rendered. */
  previewAssetVersionID: string;
  locale: "ar" | "en";
  /** Read at click time by the caller, which owns the session. */
  csrf: () => string | null;
  labels: {
    watch: string;
    loading: string;
    failed: string;
    mismatch: string;
    retry: string;
  };
}) {
  const [url, setURL] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);

  const open = async () => {
    if (loading) return;
    const token = csrf();
    if (!token) return;
    setError("");
    setLoading(true);
    try {
      const issued = await previewAdminCoursePreview({ courseID, revisionID, locale, csrf: token });
      if (!issuedPreviewMatches(issued, { courseID, revisionID, previewAssetVersionID })) {
        throw new Error(labels.mismatch);
      }
      setURL(issued.url);
    } catch (cause) {
      setError(describeApiError(cause, locale) || labels.failed);
    } finally {
      setLoading(false);
    }
  };

  if (url) {
    return (
      <video
        controls
        autoPlay
        preload="metadata"
        src={url}
        onError={() => {
          setURL(null);
          setError(labels.failed);
        }}
        data-testid="review-course-preview-player"
        aria-label={labels.watch}
        className="aspect-video w-full rounded-lg bg-gx-navy"
      />
    );
  }

  return (
    <div>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => void open()}
        disabled={loading}
        aria-busy={loading}
        aria-label={labels.watch}
        data-testid="watch-review-course-preview"
        className="group flex aspect-video w-full items-center justify-center gap-3 rounded-lg border border-border bg-gx-navy text-gx-ink-50 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-progress"
      >
        <span className="flex size-14 items-center justify-center rounded-full bg-gx-ink-50/95 text-gx-navy transition-transform duration-base ease-out-brand group-hover:scale-105">
          <Play aria-hidden className="size-6 fill-current ps-0.5" />
        </span>
        <span className="font-display text-[15px] font-bold">
          {loading ? labels.loading : labels.watch}
        </span>
      </button>
      {error ? (
        <ErrorState
          className="mt-3"
          testID="review-course-preview-error"
          title={labels.failed}
          detail={error}
          retryLabel={labels.retry}
          onRetry={() => {
            void open();
            triggerRef.current?.focus();
          }}
        />
      ) : null}
    </div>
  );
}
