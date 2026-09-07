"use client";

import { useRef, useState } from "react";
import { Play } from "lucide-react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
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
 * # WHY A PICTURE AND NOT A BUTTON
 *
 * The preview used to be an outline button under a paragraph, which read as a link to somewhere
 * else rather than as a video that was already here. The frame with a play control on it is the
 * one shape everybody already reads as "this is a video, press it" — and it is the *same* control,
 * with the same single request behind it.
 *
 * The frame stays a real `button`: the picture is decorative and the accessible name is the words,
 * so `Enter`, `Space` and a screen reader all get the same control they got before. Nothing is
 * preloaded — the request is made on activation rather than on render, so an expiry-bounded URL is
 * not minted for every visitor who scrolls past, and the `<video>` element does not exist until
 * there is something for it to play.
 */
export function CoursePreview({
  courseID,
  locale,
  copy,
  watchLabel,
  failureLabel,
  retryLabel,
  posterURL,
}: {
  courseID: string;
  locale: "ar" | "en";
  copy: Dictionary["courseDetail"];
  /** The catalogue's own words, shared with the list. */
  watchLabel: string;
  failureLabel: string;
  retryLabel: string;
  /** The Course's own published thumbnail, when it has one. Decorative: it names nothing. */
  posterURL?: string | null;
}) {
  const [preview, setPreview] = useState<{ url: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [playbackFailed, setPlaybackFailed] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  function openPreview() {
    if (loading) return;
    setFailed(false);
    setPlaybackFailed(false);
    setLoading(true);
    getPublicCoursePreview(courseID, locale)
      .then((issued) => setPreview({ url: issued.url }))
      .catch(() => setFailed(true))
      .finally(() => setLoading(false));
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

      <div className="mt-5 max-w-3xl">
        {preview ? (
          <div data-testid="public-preview-surface">
            <video
              controls
              autoPlay
              preload="metadata"
              src={preview.url}
              poster={posterURL ?? undefined}
              onError={() => setPlaybackFailed(true)}
              className="aspect-video w-full rounded-lg bg-gx-navy"
              data-testid="public-preview-player"
            >
              {failureLabel}
            </video>
          </div>
        ) : (
          <button
            ref={triggerRef}
            type="button"
            onClick={openPreview}
            disabled={loading}
            aria-busy={loading}
            data-testid="watch-public-preview"
            // The accessible name is the words. The picture behind them is decorative and is
            // deliberately not part of the name — a thumbnail's file has nothing to say about
            // what pressing this does.
            aria-label={watchLabel}
            className="group relative flex aspect-video w-full items-center justify-center overflow-hidden rounded-lg border border-border bg-gx-navy transition-shadow focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-progress"
          >
            {posterURL ? (
              // A plain `img`: the URL is issued by the catalogue projection and is not a
              // configured image host, so the framework's optimizer has nothing to add here.
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={posterURL}
                alt=""
                aria-hidden
                className="absolute inset-0 size-full object-cover transition-transform duration-slow ease-out-brand group-hover:scale-[1.02]"
              />
            ) : null}
            {/* A wash rather than a plain dim: the play control and the words must clear contrast
                over whatever picture a Course happens to carry, including a bright one. */}
            <span
              aria-hidden
              className="absolute inset-0 bg-gradient-to-t from-gx-navy/85 via-gx-navy/45 to-gx-navy/25"
            />
            <span className="relative flex flex-col items-center gap-3">
              <span className="flex size-16 items-center justify-center rounded-full bg-gx-ink-50/95 text-gx-navy shadow-lg transition-transform duration-base ease-out-brand group-hover:scale-105 sm:size-20">
                <Play aria-hidden className="size-7 fill-current ps-0.5 sm:size-9" />
              </span>
              <span className="font-display text-[15px] font-bold text-gx-ink-50">
                {loading ? copy.previewLoading : watchLabel}
              </span>
            </span>
            {/* The word is what a screen reader is told; the busy state is what it is told about
                the wait. Neither depends on the animation. */}
            {loading ? (
              <span role="status" aria-live="polite" className="sr-only">
                {copy.previewLoading}
              </span>
            ) : null}
          </button>
        )}
      </div>

      {failed || playbackFailed ? (
        <ErrorState
          className="mt-4 max-w-xl"
          testID="public-preview-error"
          title={failureLabel}
          retryLabel={retryLabel}
          onRetry={() => {
            setPreview(null);
            setPlaybackFailed(false);
            openPreview();
            triggerRef.current?.focus();
          }}
        />
      ) : null}
    </section>
  );
}
