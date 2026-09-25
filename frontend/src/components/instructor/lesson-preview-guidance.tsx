"use client";

import { ArrowRight, Clapperboard } from "lucide-react";
import { useAuthoringAdvance } from "./authoring-workflow";

/**
 * The primary public-preview workflow, stated where an Instructor looks for it.
 *
 * # WHY THIS PANEL EXISTS
 *
 * Public preview used to mean one separate video uploaded against the course —
 * a trailer with no relationship to anything a student would actually receive.
 * It is now a permission on a Lesson: the Instructor picks lessons visitors may
 * watch, and those real lessons are what appear on the course page.
 *
 * That is a better offer on both sides. The Instructor stops producing a second
 * artefact, and the visitor judges the course by the teaching rather than by
 * marketing material. But it is only true if the authoring surface says so.
 * Leaving the old uploader here as the obvious control would keep teaching the
 * old model, and every preview created through it would be more legacy data.
 *
 * So this panel is not a hint next to the uploader. On a course with no legacy
 * preview it is the ONLY preview surface, and the uploader is not rendered at
 * all. Where a legacy preview already exists it stays manageable below, under a
 * label that says what it is.
 *
 * It writes nothing. The Lesson permission is set in the curriculum, against the
 * lesson it belongs to, which is the only place that decision makes sense.
 */
export function LessonPreviewGuidance({
  copy,
}: {
  copy: {
    lessonFirstTitle: string;
    lessonFirstDescription: string;
    lessonFirstAction: string;
  };
}) {
  // Rendered inside the authoring disclosure, so advancing past PREVIEW is the
  // same intentional step the section's own Continue control performs — it opens
  // Curriculum rather than jumping the Instructor somewhere unrelated.
  const advance = useAuthoringAdvance();
  return (
    <section
      data-testid="lesson-preview-guidance"
      aria-labelledby="lesson-preview-guidance-title"
      className="rounded-lg border border-border bg-muted/40 p-4"
    >
      <div className="flex items-start gap-3">
        {/* Decorative: the heading beside it already names the panel, so a second
            announcement would only repeat it. Not mirrored in Arabic — a clapper
            board has a fixed orientation and is not a directional glyph. */}
        <Clapperboard aria-hidden className="mt-0.5 size-5 shrink-0 text-primary" />
        <div className="space-y-2">
          <h3
            id="lesson-preview-guidance-title"
            className="font-display text-sm font-bold text-foreground"
          >
            {copy.lessonFirstTitle}
          </h3>
          <p className="text-sm text-muted-foreground">{copy.lessonFirstDescription}</p>
          <button
            type="button"
            onClick={() => advance("PREVIEW")}
            data-testid="lesson-preview-guidance-curriculum"
            className="inline-flex items-center gap-1.5 rounded-md px-1 py-1 text-sm font-semibold text-primary underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
          >
            {copy.lessonFirstAction}
            {/* Mirrored in Arabic: an arrow meaning "onward" must point in the
                reading direction, or it reads as "back". */}
            <ArrowRight aria-hidden className="size-4 rtl:-scale-x-100" />
          </button>
        </div>
      </div>
    </section>
  );
}
