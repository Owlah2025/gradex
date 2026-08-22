"use client";

import type { CourseRevisionWire } from "@/lib/api/catalog";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { isReturnedForChanges } from "./change-request-state";

/**
 * The standing notice an Instructor sees when a Course was returned for changes.
 *
 * It is deliberately a persistent region rather than a toast: the Instructor usually returns in a
 * later session, long after any transient message is gone, and the reason is the only thing that
 * tells them what to fix.
 *
 * Gated on `state`, never on the presence of `review_reason`. The server retains the reason on the
 * revision row after a resubmission, so rendering on the reason alone would keep showing a resolved
 * change request while the revision is already back in review.
 */
export function ChangeRequestNotice({
  revision,
  labels,
}: {
  revision: Pick<CourseRevisionWire, "state" | "review_reason"> | null | undefined;
  labels: Dictionary["instructor"]["changeRequest"];
}) {
  if (!isReturnedForChanges(revision)) return null;

  const reason = revision?.review_reason?.trim();

  return (
    <section
      data-testid="change-request-notice"
      data-revision-state={revision?.state}
      aria-labelledby="change-request-title"
      className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm"
    >
      <h3
        id="change-request-title"
        className="font-display text-base font-bold text-foreground"
      >
        {labels.title}
      </h3>

      <p className="mt-3 font-display text-xs font-bold uppercase tracking-wide text-muted-foreground">
        {labels.reasonLabel}
      </p>
      {reason ? (
        <p
          data-testid="change-request-reason"
          className="mt-1 whitespace-pre-wrap leading-6 text-foreground"
        >
          {reason}
        </p>
      ) : (
        <p data-testid="change-request-reason" className="mt-1 leading-6 text-muted-foreground">
          {labels.noReason}
        </p>
      )}

      <p className="mt-3 leading-6 text-muted-foreground">{labels.nextStep}</p>
    </section>
  );
}
