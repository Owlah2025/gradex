"use client";

import * as Dialog from "@radix-ui/react-dialog";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { learningReportReasons, submitLearningReport, type LearningReportReason } from "@/lib/api/learning";
import { currentCSRFToken } from "@/lib/identity/session";
import {
  canSubmitReport,
  classifyReportFailure,
  explanationIsRequired,
  failureAllowsRetry,
  failurePreservesInput,
  initialReportFormState,
  isStaleReportScope,
  reportFieldError,
  type ReportFailure,
  type ReportFormState,
  type ReportSubmissionPhase,
} from "./report-dialog-state";
import {
  reportFailureMessage,
  reportFieldErrorMessage,
  reportReasonLabel,
  reportTargetActionLabel,
  reportTargetLabel,
  type ReportLabels,
} from "./report-labels";
import type { ReportTarget } from "./report-targets";

/**
 * The Report Content dialog (T066), for one target on one rendered page.
 *
 * The encrypted context reaches this component as a prop and leaves it only inside the request
 * body. It is never rendered, never placed in an attribute, never persisted, and never decoded —
 * the component does not even read it, it passes it through.
 *
 * The dialog is scoped to the exact target it was opened for. `scope` changes whenever the Course,
 * the Lesson, or the kind changes, and that closes the dialog and discards any response still in
 * flight: a modal opened on Lesson A must not be submittable once Lesson B is the page, and A's
 * late answer must not paint over B.
 */

export function ReportContentDialog({
  target,
  scope,
  locale,
  labels,
}: {
  target: ReportTarget;
  scope: string;
  locale: "ar" | "en";
  labels: ReportLabels;
}) {
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<ReportFormState>(initialReportFormState);
  const [phase, setPhase] = useState<ReportSubmissionPhase>("editing");
  const [failure, setFailure] = useState<ReportFailure | null>(null);
  const [showFieldError, setShowFieldError] = useState(false);

  // The scope this dialog belongs to, captured so a response can be checked against the scope now
  // displayed rather than trusted because it arrived.
  const scopeRef = useRef(scope);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const fieldPrefix = useId();
  const reasonFieldID = `${fieldPrefix}-reason`;
  const explanationFieldID = `${fieldPrefix}-explanation`;
  const errorID = `${fieldPrefix}-error`;
  const statusID = `${fieldPrefix}-status`;

  const reset = useCallback(() => {
    setForm(initialReportFormState());
    setPhase("editing");
    setFailure(null);
    setShowFieldError(false);
  }, []);

  // Navigation boundary. A new Course, Lesson, or kind is a different dialog entirely.
  useEffect(() => {
    if (scopeRef.current === scope) return;
    scopeRef.current = scope;
    setOpen(false);
    reset();
  }, [scope, reset]);

  const fieldError = reportFieldError(form);
  const explanationRequired = explanationIsRequired(form.reason);
  const submittable = canSubmitReport(form, phase);

  const onOpenChange = useCallback(
    (next: boolean) => {
      // A submission in flight is not cancellable — the report may already have been recorded, and
      // closing here would leave the Student unsure whether it was.
      if (!next && phase === "submitting") return;
      setOpen(next);
      if (!next) reset();
    },
    [phase, reset],
  );

  const onSubmit = useCallback(
    async (event: React.FormEvent<HTMLFormElement>) => {
      event.preventDefault();
      if (!submittable) {
        setShowFieldError(true);
        return;
      }
      const submittedScope = scopeRef.current;
      setPhase("submitting");
      setFailure(null);
      try {
        await submitLearningReport(
          {
            report_context: target.context,
            reason: form.reason as LearningReportReason,
            explanation: form.explanation,
          },
          locale,
          currentCSRFToken(),
        );
        // A late success for a target that is no longer displayed changes nothing on screen.
        if (isStaleReportScope(submittedScope, scopeRef.current)) return;
        setPhase("acknowledged");
      } catch (error) {
        if (isStaleReportScope(submittedScope, scopeRef.current)) return;
        // The error is classified, never displayed: its message could carry server detail.
        const classified = classifyReportFailure(error);
        setFailure(classified);
        setPhase("editing");
        if (!failurePreservesInput(classified)) setForm(initialReportFormState());
      }
    },
    [submittable, target.context, form.reason, form.explanation, locale],
  );

  const acknowledged = phase === "acknowledged";
  const submitting = phase === "submitting";
  const retryable = failure === null || failureAllowsRetry(failure);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Trigger asChild>
        <button
          ref={triggerRef}
          type="button"
          className="inline-flex min-h-11 items-center rounded-md border border-border px-3 py-2 text-sm font-medium text-foreground/80 hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {labels.reportAction}
          <span className="sr-only"> — {reportTargetActionLabel(target.kind, labels)}</span>
        </button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/50" />
        <Dialog.Content
          dir={locale === "ar" ? "rtl" : "ltr"}
          aria-describedby={`${fieldPrefix}-description`}
          className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(32rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border border-border bg-card p-6 shadow-lg"
          onEscapeKeyDown={(event) => {
            if (submitting) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (submitting) event.preventDefault();
          }}
        >
          <Dialog.Title className="font-display text-2xl font-bold text-foreground">
            {labels.reportDialogTitle}
          </Dialog.Title>
          <p id={`${fieldPrefix}-description`} className="mt-2 text-sm text-foreground/80">
            {labels.reportDialogDescription}
          </p>
          <p className="mt-3 text-sm text-foreground/80">
            {labels.reportTargetLabel}: <span className="font-semibold text-foreground">{reportTargetLabel(target.kind, labels)}</span>
          </p>

          {acknowledged ? (
            <div className="mt-5 space-y-3">
              {/* The whole acknowledgement: received, and nothing about what happens next. */}
              <p role="status" className="font-semibold text-foreground">
                {labels.reportSuccessTitle}
              </p>
              <p className="text-sm text-foreground/80">{labels.reportSuccessBody}</p>
              <Dialog.Close asChild>
                <button
                  type="button"
                  className="inline-flex min-h-11 items-center rounded-md bg-primary px-4 py-2 font-semibold text-primary-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  {labels.reportDone}
                </button>
              </Dialog.Close>
            </div>
          ) : (
            <form onSubmit={onSubmit} noValidate className="mt-5 space-y-4">
              <div className="space-y-2">
                <label htmlFor={reasonFieldID} className="block text-sm font-semibold text-foreground">
                  {labels.reportReasonLabel}
                </label>
                <select
                  id={reasonFieldID}
                  name="reason"
                  required
                  value={form.reason}
                  disabled={submitting}
                  aria-invalid={showFieldError && fieldError === "reasonRequired"}
                  aria-describedby={showFieldError && fieldError === "reasonRequired" ? errorID : undefined}
                  onChange={(event) => {
                    setForm((current) => ({ ...current, reason: event.target.value as LearningReportReason | "" }));
                    setShowFieldError(false);
                  }}
                  className="min-h-11 w-full rounded-md border border-border bg-background px-3 py-2 text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  <option value="">{labels.reportReasonPlaceholder}</option>
                  {learningReportReasons.map((reason) => (
                    <option key={reason} value={reason}>
                      {reportReasonLabel(reason, labels)}
                    </option>
                  ))}
                </select>
              </div>

              <div className="space-y-2">
                <label htmlFor={explanationFieldID} className="block text-sm font-semibold text-foreground">
                  {labels.reportExplanationLabel}
                  <span className="ms-2 font-normal text-muted-foreground">
                    ({explanationRequired ? labels.reportExplanationRequired : labels.reportExplanationOptional})
                  </span>
                </label>
                <textarea
                  id={explanationFieldID}
                  name="explanation"
                  rows={4}
                  value={form.explanation}
                  disabled={submitting}
                  required={explanationRequired}
                  aria-invalid={showFieldError && fieldError === "explanationRequired"}
                  aria-describedby={showFieldError && fieldError === "explanationRequired" ? errorID : undefined}
                  onChange={(event) => {
                    setForm((current) => ({ ...current, explanation: event.target.value }));
                    setShowFieldError(false);
                  }}
                  className="w-full rounded-md border border-border bg-background px-3 py-2 text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                />
              </div>

              {showFieldError && fieldError ? (
                <p id={errorID} role="alert" className="text-sm font-medium text-destructive">
                  {reportFieldErrorMessage(fieldError, labels)}
                </p>
              ) : null}

              {/* One generic sentence per outcome. Never a cause, never the server's own words. */}
              <p id={statusID} role="status" aria-live="polite" className="text-sm text-foreground/80">
                {submitting ? labels.reportSubmitting : failure ? reportFailureMessage(failure, labels) : ""}
              </p>

              <div className="flex flex-wrap justify-end gap-3">
                <Dialog.Close asChild>
                  <button
                    type="button"
                    disabled={submitting}
                    className="inline-flex min-h-11 items-center rounded-md border border-border px-4 py-2 font-semibold text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-60"
                  >
                    {labels.reportCancel}
                  </button>
                </Dialog.Close>
                <button
                  type="submit"
                  disabled={submitting || !retryable}
                  aria-describedby={statusID}
                  className="inline-flex min-h-11 items-center rounded-md bg-primary px-4 py-2 font-semibold text-primary-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-60"
                >
                  {submitting ? labels.reportSubmitting : labels.reportSubmit}
                </button>
              </div>
            </form>
          )}

          <Dialog.Close asChild>
            <button
              type="button"
              aria-label={labels.reportClose}
              disabled={submitting}
              className="absolute end-4 top-4 inline-flex h-11 w-11 items-center justify-center rounded-md text-foreground/70 hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-60"
            >
              <span aria-hidden="true">×</span>
            </button>
          </Dialog.Close>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/**
 * ReportTargetActions renders one trigger per reportable target.
 *
 * Reporting is deliberately secondary: these are bordered text buttons in a labelled group beside
 * the content, never the page's primary action.
 */
export function ReportTargetActions({
  targets,
  scopePrefix,
  locale,
  labels,
}: {
  targets: ReportTarget[];
  scopePrefix: string;
  locale: "ar" | "en";
  labels: ReportLabels;
}) {
  if (targets.length === 0) return null;
  return (
    <ul aria-label={labels.reportAction} className="flex flex-wrap gap-2">
      {targets.map((target) => (
        <li key={target.kind}>
          <ReportContentDialog
            target={target}
            scope={`${scopePrefix} ${target.kind}`}
            locale={locale}
            labels={labels}
          />
        </li>
      ))}
    </ul>
  );
}
