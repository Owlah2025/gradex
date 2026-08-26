"use client";

import { useState } from "react";
import { PlayCircle } from "lucide-react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { ErrorState } from "@/components/common/error-state";
import { Prose } from "@/components/ui/typography";
import { getPublicCoursePreview } from "@/lib/api/public-catalog";

/**
 * The one openly published excerpt of a Course.
 *
 * Presentation only. The URL is issued by `GET /api/v1/media/courses/{id}/preview`, which resolves
 * the approved live revision server-side and returns an expiry-bounded link; nothing here decides
 * what may be watched, and no protected lesson is reachable through it. The public projection
 * exposes a single course-level `has_preview` flag rather than a per-lesson one, so this is one
 * preview per Course — there is no "free lessons" set to mark up, and pretending otherwise would
 * promise access to content the contract never offers.
 *
 * The request is made on click rather than on render so an expiry-bounded URL is not minted for
 * every visitor who scrolls past.
 */
export function CoursePreview({
  courseID,
  locale,
  copy,
  watchLabel,
  failureLabel,
  retryLabel,
}: {
  courseID: string;
  locale: "ar" | "en";
  copy: Dictionary["courseDetail"];
  /** The catalogue's own words, shared with the list. */
  watchLabel: string;
  failureLabel: string;
  retryLabel: string;
}) {
  const [preview, setPreview] = useState<{ url: string } | null>(null);
  const [failed, setFailed] = useState(false);

  function openPreview() {
    setFailed(false);
    getPublicCoursePreview(courseID, locale)
      .then((issued) => setPreview({ url: issued.url }))
      .catch(() => setFailed(true));
  }

  return (
    <section
      className="mt-10"
      aria-labelledby="public-preview-heading"
      data-testid="course-preview"
    >
      <h2
        id="public-preview-heading"
        className="font-display text-2xl font-bold text-foreground"
      >
        {copy.previewHeading}
      </h2>
      <Prose className="mt-2 max-w-2xl text-[15.5px]">{copy.previewLead}</Prose>

      {preview ? (
        <div className="mt-5" data-testid="public-preview-surface">
          <video
            controls
            autoPlay
            preload="metadata"
            src={preview.url}
            className="w-full max-w-3xl rounded-lg bg-gx-navy"
            data-testid="public-preview-player"
          >
            {failureLabel}
          </video>
        </div>
      ) : (
        <Button
          type="button"
          variant="outline"
          onClick={openPreview}
          data-testid="watch-public-preview"
          className="mt-5"
        >
          <PlayCircle aria-hidden />
          {watchLabel}
        </Button>
      )}

      {failed ? (
        <ErrorState
          className="mt-4 max-w-xl"
          testID="public-preview-error"
          title={failureLabel}
          retryLabel={retryLabel}
          onRetry={openPreview}
        />
      ) : null}
    </section>
  );
}
