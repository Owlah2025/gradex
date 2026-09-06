"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, CircleDashed } from "lucide-react";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { CourseWire } from "@/lib/api/authoring";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { submissionReadiness, type ReadinessRequirement } from "./submission-readiness";
import type { PublicationMode } from "./revision-workflow";

type SubmissionLabels = Dictionary["instructor"]["submission"];

/** Beyond this, the list of named offenders stops being a help and starts being the problem. */
const MAX_NAMED_OFFENDERS = 3;

/**
 * Whether this course can be submitted, and what stands in the way.
 *
 * What this replaces was a green button and a sentence saying the server validates completeness.
 * Pressing it on an incomplete course returned the server's violation list rendered as codes and
 * database keys, so the workflow was: press, read an enum, guess which lesson, fix it, press again.
 * A twelve-lesson course with no videos took twelve rounds of that.
 *
 * The checklist is derived from the same rules `catalog/validation.go` applies, and names the
 * sections and lessons that fail by the titles the Instructor wrote. The server stays
 * authoritative — three of its checks depend on state a client cannot see, and its refusal is
 * still shown when it comes — but nothing that *is* knowable in the browser waits for a round trip
 * to be said.
 *
 * The price is named here on purpose. It is the one thing on this screen an Instructor might
 * reasonably think they are blocked on, and they are not: `SubmitCourse` never reads a price, and
 * the admin sets it during review.
 *
 * Submission is confirmed because it genuinely closes editing — the revision moves to
 * `PENDING_REVIEW` and the studio stops accepting changes until a decision lands. That is a
 * different act from saving, and the old surface made them look identical.
 */
export function SubmissionPanel({
  course,
  labels,
  mode,
  busy,
  rejection,
  onSubmit,
}: {
  course: CourseWire;
  labels: SubmissionLabels;
  /**
   * Which act this panel performs. A course that has never been live is
   * submitted for review; one that has been live is published by its own
   * instructor. Only the words and the confirmation differ — the checklist,
   * the readiness rules, and the server's authority over all of it do not.
   */
  mode: PublicationMode;
  busy: boolean;
  /** The server's own refusal, already translated. */
  rejection: { reasons: string[]; detail?: string | null } | null;
  onSubmit: () => void;
}) {
  const { locale } = useLocale();
  const [confirming, setConfirming] = useState(false);
  const rejectionRef = useRef<HTMLDivElement | null>(null);
  const publishing = mode === "SUBSEQUENT_PUBLICATION";
  const copy = publishing
    ? {
        title: labels.publishTitle,
        leadReady: labels.publishLeadReady,
        leadIncomplete: labels.publishLeadIncomplete,
        action: labels.publishAction,
        pending: labels.publishing,
        serverNote: labels.publishServerNote,
        confirmTitle: labels.publishConfirmTitle,
        confirmBody: labels.publishConfirmBody,
        confirmAccept: labels.publishConfirmAccept,
        rejectedTitle: labels.publishRejectedTitle,
      }
    : {
        title: labels.title,
        leadReady: labels.leadReady,
        leadIncomplete: labels.leadIncomplete,
        action: labels.submitAction,
        pending: labels.submitting,
        serverNote: labels.serverNote,
        confirmTitle: labels.confirmTitle,
        confirmBody: labels.confirmBody,
        confirmAccept: labels.confirmAccept,
        rejectedTitle: labels.rejectedTitle,
      };

  /*
    The rejection is brought to the click. The founder's manual test pressed Submit near the
    bottom of a long page, saw nothing change, and read the click as a no-op — the server's reason
    had rendered outside the viewport. Focusing rather than only scrolling means a keyboard or
    screen-reader user lands on it too.
  */
  useEffect(() => {
    if (!rejection) return;
    const frame = window.requestAnimationFrame(() => {
      rejectionRef.current?.scrollIntoView({ block: "center" });
      rejectionRef.current?.focus();
    });
    return () => window.cancelAnimationFrame(frame);
  }, [rejection]);

  const readiness = useMemo(
    () =>
      submissionReadiness(course, locale, (kind, position) =>
        kind === "section"
          ? `${labels.untitledSection} ${position}`
          : `${labels.untitledLesson} ${position}`,
      ),
    [course, locale, labels],
  );

  return (
    <section
      className="rounded-lg border border-border bg-card p-5"
      aria-labelledby="submission-title"
      data-testid="submission-panel"
      data-publication-mode={mode}
      data-submission-ready={readiness.ready ? "true" : "false"}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h3 id="submission-title" className="font-display text-base font-bold text-foreground">
          {copy.title}
        </h3>
        {/*
          A count, not a percentage. Requirements are neither equally sized nor equally weighted,
          and "80% complete" on a course with no video anywhere would overstate how close it is.
        */}
        <p className="text-xs font-semibold text-muted-foreground" data-testid="readiness-count">
          {readiness.metCount}/{readiness.totalCount} {labels.progress}
        </p>
      </div>
      <p className="mt-1 text-sm leading-6 text-muted-foreground">
        {readiness.ready ? copy.leadReady : copy.leadIncomplete}
      </p>
      {publishing ? null : (
        /* Said once, plainly, on the only course it applies to: the first
           publication is the administrator's decision, and nothing on this
           screen changes that. */
        <p className="mt-2 text-sm leading-6 text-muted-foreground" data-testid="first-publication-note">
          {labels.firstPublicationNote}
        </p>
      )}

      <ul className="mt-4 space-y-2.5" data-testid="readiness-checklist">
        {readiness.requirements.map((requirement) => (
          <RequirementRow
            key={requirement.key}
            requirement={requirement}
            labels={labels}
          />
        ))}
      </ul>

      {publishing ? null : (
        <p className="mt-4 text-sm text-muted-foreground" data-testid="submission-price-note">
          {labels.adminOwnsPrice}
        </p>
      )}

      <div className="mt-5 flex flex-wrap items-center gap-3 border-t border-border pt-4">
        <Button
          type="button"
          disabled={busy}
          onClick={() => setConfirming(true)}
          data-testid="submit-for-review"
        >
          {busy ? copy.pending : copy.action}
        </Button>
        <p className="text-xs text-muted-foreground">{copy.serverNote}</p>
      </div>

      {rejection ? (
        /* Reported beside the control that produced it, never only at the top of the page. */
        <div
          ref={rejectionRef}
          role="alert"
          tabIndex={-1}
          data-testid="submit-error"
          className="mt-4 rounded-lg border border-destructive/30 bg-destructive/5 p-4"
        >
          <p className="font-display text-sm font-bold text-foreground">{copy.rejectedTitle}</p>
          {rejection.reasons.length > 0 ? (
            <ul className="mt-2 space-y-1 text-sm leading-6 text-foreground">
              {rejection.reasons.map((reason) => (
                <li key={reason}>{reason}</li>
              ))}
            </ul>
          ) : null}
          {rejection.detail ? (
            /* An unmapped code is shown rather than swallowed: a refusal with no reason at all is
               worse than one carrying the server's own words. */
            <p className="mt-2 text-sm leading-6 text-muted-foreground" data-testid="submit-error-detail">
              {rejection.detail}
            </p>
          ) : null}
        </div>
      ) : null}

      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={copy.confirmTitle}
        body={copy.confirmBody}
        confirmLabel={copy.confirmAccept}
        cancelLabel={labels.confirmCancel}
        tone="default"
        busy={busy}
        onConfirm={() => {
          setConfirming(false);
          onSubmit();
        }}
        testID="submit-confirm"
      />
    </section>
  );
}

function RequirementRow({
  requirement,
  labels,
}: {
  requirement: ReadinessRequirement;
  labels: SubmissionLabels;
}) {
  const named = requirement.offenders.slice(0, MAX_NAMED_OFFENDERS);
  const remaining = requirement.offenders.length - named.length;

  return (
    <li
      className="flex gap-2.5"
      data-testid={`requirement-${requirement.key}`}
      data-met={requirement.met ? "true" : "false"}
    >
      {/* The icon repeats what the text already says; it is not the only carrier of either state. */}
      {requirement.met ? (
        <Check className="mt-0.5 size-4 shrink-0 text-gx-success" aria-hidden />
      ) : (
        <CircleDashed className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
      )}
      <div className="min-w-0">
        <p
          className={
            requirement.met
              ? "text-sm text-muted-foreground line-through decoration-1"
              : "text-sm font-medium text-foreground"
          }
        >
          {labels.requirement[requirement.key]}
        </p>
        {named.length > 0 ? (
          <p className="mt-0.5 text-xs text-muted-foreground">
            {labels.offenders}{" "}
            <bdi>{named.join("، ")}</bdi>
            {remaining > 0 ? ` (+${remaining} ${labels.offenderMore})` : ""}
          </p>
        ) : null}
      </div>
    </li>
  );
}
