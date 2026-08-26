"use client";

import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { RevisionWorkflow } from "./revision-workflow";
import { Button } from "@/components/ui/button";

type RevisionLabels = Dictionary["instructor"]["revision"];

/**
 * What the studio offers a Course that has no editable revision open.
 *
 * A published Course used to end here with a flat "no editable revision" sentence and no action,
 * which made a published Course a dead end for its own Instructor: the candidate-creation endpoint
 * existed but nothing in the product called it.
 */
export function RevisionWorkflowPanel({
  workflow,
  busy,
  labels,
  onStart,
}: {
  workflow: RevisionWorkflow;
  busy: boolean;
  labels: RevisionLabels;
  onStart: () => void;
}) {
  if (workflow === "START_REVISION") {
    return (
      <section
        data-testid="start-revision-panel"
        aria-labelledby="start-revision-title"
        className="rounded-lg border border-border bg-card p-4 text-sm"
      >
        <h3 id="start-revision-title" className="font-display text-base font-bold text-foreground">
          {labels.startTitle}
        </h3>
        <p className="mt-2 leading-6 text-muted-foreground">{labels.startBody}</p>
        <Button
          type="button"
          size="sm"
          data-testid="start-revision"
          onClick={onStart}
          disabled={busy}
          className="mt-4"
        >
          {busy ? labels.starting : labels.startAction}
        </Button>
      </section>
    );
  }

  if (workflow === "CANDIDATE_IN_REVIEW") {
    return (
      <section
        data-testid="revision-in-review-panel"
        aria-labelledby="revision-in-review-title"
        className="rounded-lg border border-border bg-card p-4 text-sm"
      >
        <h3 id="revision-in-review-title" className="font-display text-base font-bold text-foreground">
          {labels.inReviewTitle}
        </h3>
        <p className="mt-2 leading-6 text-muted-foreground">{labels.inReviewBody}</p>
      </section>
    );
  }

  return (
    <p className="text-sm italic text-muted-foreground" data-testid="no-editable-revision">
      {labels.unavailable}
    </p>
  );
}

/**
 * Shown while editing a candidate that sits behind a live revision.
 *
 * The Instructor must not believe these edits are already reaching Students — they do not, until
 * the revision is submitted and an Admin approves it.
 */
export function EditingPublishedNotice({ labels }: { labels: RevisionLabels }) {
  return (
    <section
      data-testid="editing-published-notice"
      aria-labelledby="editing-published-title"
      className="rounded-lg border border-border bg-muted/50 p-4 text-sm"
    >
      <h3 id="editing-published-title" className="font-display text-base font-bold text-foreground">
        {labels.editingPublishedTitle}
      </h3>
      <p className="mt-2 leading-6 text-muted-foreground">{labels.editingPublishedBody}</p>
    </section>
  );
}
