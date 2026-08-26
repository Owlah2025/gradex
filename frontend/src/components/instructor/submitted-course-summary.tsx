"use client";

import type { CourseRevisionWire } from "@/lib/api/catalog";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";

type SubmittedLabels = Dictionary["instructor"]["submitted"];

/**
 * What a course looks like while an administrator holds it.
 *
 * The studio had no such state. The editable region was gated on whether a revision existed, not
 * on whether it could be edited, and a submitted revision still exists — so a course in review
 * rendered the full authoring form, every input live, with a Submit button under it. Every write
 * that form could issue would be refused by the server, and the instructor had no way to know that
 * before trying. A grey pill overhead read "In review"; nothing else on the screen agreed with it.
 *
 * What is shown instead is the shape of what was sent, drawn from the revision graph the server
 * returns. There is no submission timestamp: the owned-course payload does not carry one, and a
 * fabricated "submitted 2 hours ago" would be worse than saying nothing. There is no revision
 * identifier either — the instructor submitted a course, not a row.
 */
export function SubmittedCourseSummary({
  revision,
  labels,
}: {
  revision: CourseRevisionWire;
  labels: SubmittedLabels;
}) {
  const sections = revision.sections ?? [];
  const lessons = sections.reduce((total, section) => total + (section.lessons?.length ?? 0), 0);
  const hasPreview = Boolean(revision.preview_asset_version_id);

  return (
    <section
      className="rounded-lg border border-border bg-card p-5"
      aria-labelledby="submitted-title"
      data-testid="submitted-course-summary"
    >
      <h3 id="submitted-title" className="font-display text-base font-bold text-foreground">
        {labels.title}
      </h3>
      <p className="mt-1 text-sm leading-6 text-muted-foreground">{labels.body}</p>

      <p className="mt-4 font-display text-xs font-bold uppercase tracking-wide text-muted-foreground">
        {labels.whatWasSent}
      </p>
      <dl className="mt-2 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3">
        <Fact label={labels.sections} value={String(sections.length)} testID="submitted-sections" />
        <Fact label={labels.lessons} value={String(lessons)} testID="submitted-lessons" />
        <Fact
          label={labels.preview}
          value={hasPreview ? labels.previewYes : labels.previewNo}
          testID="submitted-preview"
        />
      </dl>
    </section>
  );
}

function Fact({ label, value, testID }: { label: string; value: string; testID: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 font-display text-lg font-bold text-foreground" data-testid={testID}>
        <bdi>{value}</bdi>
      </dd>
    </div>
  );
}
