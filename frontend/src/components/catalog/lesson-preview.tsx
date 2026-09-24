"use client";

import { useRef, useState } from "react";
import { Play, X } from "lucide-react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { PublicPreviewableLesson } from "@/lib/api/public-catalog";
import { getLessonPreview } from "@/lib/api/public-catalog";
import { ProtectedHLSPlayer } from "@/components/media/protected-hls-player";

/**
 * One free lesson, played where it sits in the outline.
 *
 * # WHY INLINE AND NOT A PAGE
 *
 * The visitor is reading the outline to decide whether the course is worth buying.
 * Sending them to a separate page to watch one lesson takes them out of that
 * comparison and makes coming back their problem. The player opens under the
 * lesson row it belongs to, so the answer to "what am I getting" stays next to the
 * question.
 *
 * # WHAT IS AND IS NOT DECIDED HERE
 *
 * Nothing about what may be watched. `POST .../preview-authorizations` re-proves
 * the Course's published state, the live revision, the lesson's membership of it,
 * the preview flag, the exact video asset version, retirement, READY state and the
 * canonical renditions — and re-proves all of it again on every manifest request.
 * A refusal is deliberately indistinguishable between "not previewable", "still
 * processing" and "does not exist", so this component cannot tell the visitor
 * which, and does not try.
 *
 * The manifest is an application route, not a storage URL. The original uploaded
 * file is never reachable from here.
 *
 * Nothing is requested on render. A visitor who scrolls past a course with six
 * free lessons mints no capability at all; the request happens on activation.
 */
export function LessonPreview({
  courseID,
  lesson,
  locale,
  copy,
}: {
  courseID: string;
  lesson: PublicPreviewableLesson;
  locale: "ar" | "en";
  copy: Dictionary["courseDetail"];
}) {
  const [manifestURL, setManifestURL] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  async function open() {
    if (loading) return;
    setFailed(false);
    setLoading(true);
    try {
      const authorization = await getLessonPreview(courseID, lesson.id, locale);
      setManifestURL(authorization.manifest_url);
    } catch {
      // Deliberately one message for every refusal. The server does not say which
      // link of the chain failed, and inventing a distinction here would leak what
      // it withheld.
      setFailed(true);
    } finally {
      setLoading(false);
    }
  }

  function close() {
    setManifestURL(null);
    // Focus goes back to the control that opened the player, so a keyboard user is
    // not dropped at the top of the document after closing.
    triggerRef.current?.focus();
  }

  if (manifestURL) {
    return (
      <div className="mt-3" data-testid="lesson-preview-player">
        <ProtectedHLSPlayer
          manifestURL={manifestURL}
          autoPlay
          label={`${copy.lessonPreviewHeading}: ${lesson.title}`}
          unavailableLabel={copy.lessonPreviewUnavailable}
          testID="lesson-preview-video"
          unavailableTestID="lesson-preview-unavailable"
        />
        <button
          type="button"
          onClick={close}
          data-testid="lesson-preview-close"
          className="mt-2 inline-flex items-center gap-1.5 rounded-md px-1 py-1 text-sm font-semibold text-muted-foreground underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        >
          <X aria-hidden className="size-4" />
          {copy.lessonPreviewClose}
        </button>
      </div>
    );
  }

  return (
    <div className="mt-2">
      <button
        ref={triggerRef}
        type="button"
        onClick={open}
        disabled={loading}
        data-testid="lesson-preview-play"
        // The lesson title is inside the accessible name, so a screen-reader user
        // moving between several previews on one page can tell the controls apart.
        aria-label={`${copy.lessonPreviewPlay}: ${lesson.title}`}
        className="inline-flex items-center gap-2 rounded-md px-1 py-1 font-display text-sm font-bold text-primary underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:opacity-60"
      >
        {/* Horizontally mirrored in Arabic: a play triangle points in the reading
            direction, and one that points at the margin reads as "back". */}
        <Play aria-hidden className="size-4 rtl:-scale-x-100" />
        {loading ? copy.lessonPreviewLoading : copy.lessonPreviewPlay}
      </button>
      {failed ? (
        <p
          role="alert"
          data-testid="lesson-preview-failed"
          className="mt-1 text-sm text-destructive"
        >
          {copy.lessonPreviewFailed}
        </p>
      ) : null}
    </div>
  );
}
