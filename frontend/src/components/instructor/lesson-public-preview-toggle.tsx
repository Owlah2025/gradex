"use client";

import { useId, useState } from "react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { setLessonPublicPreview } from "@/lib/api/authoring";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";
import { isReadyState } from "@/lib/api/media-upload";

/**
 * The Instructor's decision to open one lesson to everyone.
 *
 * # WHY A CHECKBOX AND NOT A SWITCH
 *
 * This is a property of the lesson being saved with the rest of the revision, not a
 * live switch that turns something on the moment it is flipped: the flag takes
 * effect when an administrator approves the version. A checkbox is the control that
 * already means "this is how I want it saved", and it comes with a real label, real
 * `:checked` semantics, and keyboard behaviour nobody has to re-implement.
 *
 * # WHY THE VIDEO STATE IS SHOWN RATHER THAN BLOCKING
 *
 * A lesson with no video cannot be offered free, and the control says so and stays
 * disabled — the server refuses it too, so agreeing here saves a pointless round
 * trip. But a video that is still PROCESSING is the ordinary case while a course is
 * being built, and the control stays usable: the intent is saved now and the public
 * player fails closed until the video is ready. Requiring the Instructor to come
 * back and tick the box again after processing is exactly the step people forget,
 * and forgetting it means publishing a course without the free lesson they believed
 * they had offered. The reassurance line says what will happen instead of leaving
 * them guessing.
 */
export function LessonPublicPreviewToggle({
  courseID,
  revisionID,
  lessonID,
  allowPublicPreview,
  hasVideo,
  videoState,
  locale,
  labels,
  onChanged,
}: {
  courseID: string;
  revisionID: string;
  lessonID: string;
  allowPublicPreview: boolean;
  hasVideo: boolean;
  videoState?: string;
  locale: "ar" | "en";
  labels: Dictionary["instructor"]["curriculum"];
  onChanged: () => void | Promise<void>;
}) {
  const [allowed, setAllowed] = useState(allowPublicPreview);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const checkboxID = useId();
  const helpID = useId();

  const ready = isReadyState(videoState ?? "");

  async function change(next: boolean) {
    if (saving) return;
    setError(null);
    setSaving(true);
    // Optimistic only for the control's own appearance. It is reverted on failure,
    // and nothing else on the page is told the flag changed until the server agrees.
    setAllowed(next);
    const csrf = currentCSRFToken();
    if (!csrf) {
      // No session credential, so nothing may be mutated. Reverting rather than
      // sending an unauthenticated write keeps the control honest about state.
      setAllowed(!next);
      setSaving(false);
      setError(labels.publicPreviewFailed);
      return;
    }
    try {
      const updated = await setLessonPublicPreview({
        courseID,
        revisionID,
        lessonID,
        allow: next,
        locale,
        csrf,
      });
      setAllowed(Boolean(updated.allow_public_preview));
      await onChanged();
    } catch (cause) {
      setAllowed(!next);
      setError(describeApiError(cause, locale));
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="rounded-md border border-border bg-background px-3 py-2">
      <div className="flex items-start gap-2">
        <input
          id={checkboxID}
          type="checkbox"
          checked={allowed}
          disabled={saving || !hasVideo}
          aria-describedby={helpID}
          onChange={(event) => void change(event.target.checked)}
          data-testid="lesson-public-preview-toggle"
          className="mt-0.5 size-4 shrink-0 rounded border-border text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        />
        <label
          htmlFor={checkboxID}
          className="font-display text-sm font-bold leading-snug text-foreground"
        >
          {labels.publicPreviewLabel}
        </label>
      </div>

      <p
        id={helpID}
        className="mt-1 text-xs leading-relaxed text-muted-foreground"
      >
        {labels.publicPreviewHelp}
      </p>

      {!hasVideo ? (
        <p
          className="mt-1 text-xs text-muted-foreground"
          data-testid="lesson-public-preview-needs-video"
        >
          {labels.publicPreviewNeedsVideo}
        </p>
      ) : null}

      {/* Saved intent against a video that is not finished yet. Stated plainly so the
          Instructor does not read the absence of a public player as a failure. */}
      {hasVideo && allowed && !ready ? (
        <p
          className="mt-1 text-xs text-muted-foreground"
          data-testid="lesson-public-preview-processing"
        >
          {labels.publicPreviewProcessing}
        </p>
      ) : null}

      {error ? (
        <p
          role="alert"
          className="mt-1 text-xs text-destructive"
          data-testid="lesson-public-preview-error"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}
