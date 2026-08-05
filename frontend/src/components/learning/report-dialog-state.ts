// Relative rather than aliased: these are runtime values, and the node:test build resolves modules
// without the "@/" path mapping. Type-only imports may keep the alias because they are erased.
import { ProblemError } from "../../lib/api/problem";
import { isLearningReportReason, type LearningReportReason } from "../../lib/api/learning";

/**
 * The Report Content dialog's behaviour, separated from its markup so it can be reasoned about and
 * tested directly — the same split lesson-state and player-controls-model use.
 *
 * Two properties matter most here and are why this is a module rather than scattered handlers:
 * every server outcome collapses to one of a few *generic* messages, and no branch may describe why
 * a submission was refused. The server deliberately answers `404` for an expired context, an ended
 * Entitlement, a foreign session, and a removed target alike; re-deriving those causes in the UI
 * would rebuild in the browser exactly the oracle the server refuses to be.
 */

export type ReportFormState = {
  reason: LearningReportReason | "";
  explanation: string;
};

export type ReportSubmissionPhase = "editing" | "submitting" | "acknowledged";

/** The generic outcomes the dialog can display. Each maps to one localized string. */
export type ReportFailure =
  | "duplicate"
  | "throttled"
  | "unavailable"
  | "invalid"
  | "unexpected";

export type ReportFieldError = "reasonRequired" | "explanationRequired";

export function initialReportFormState(): ReportFormState {
  return { reason: "", explanation: "" };
}

/**
 * explanationIsRequired mirrors the server's `rep_other_needs_explanation` constraint: `other` is
 * the reason that means "none of the above", so it is the one that has to say what it means.
 */
export function explanationIsRequired(reason: ReportFormState["reason"]): boolean {
  return reason === "other";
}

/**
 * reportFieldError reports the one field a Student still has to complete, or null.
 *
 * Whitespace is not an explanation — the server trims before checking, so the form does too, and a
 * Student is told before the request rather than after a refusal.
 */
export function reportFieldError(state: ReportFormState): ReportFieldError | null {
  if (state.reason === "" || !isLearningReportReason(state.reason)) return "reasonRequired";
  if (explanationIsRequired(state.reason) && state.explanation.trim() === "") return "explanationRequired";
  return null;
}

/**
 * canSubmitReport gates the submit control. One in-flight submission per dialog: a second request
 * would either duplicate the Student's own open report or spend another of their five attempts.
 */
export function canSubmitReport(state: ReportFormState, phase: ReportSubmissionPhase): boolean {
  return phase === "editing" && reportFieldError(state) === null;
}

/**
 * classifyReportFailure maps a rejected submission to one generic outcome.
 *
 * The server's own boundaries are preserved exactly: `409` is the Student's own open report, `429`
 * is the throttle, and `404` is the single uniform refusal whose cause is deliberately unknowable.
 * Anything else — a transport failure, an unreadable body, a `500` — is "unexpected", which keeps
 * the Student's typing so they can retry deliberately.
 */
export function classifyReportFailure(error: unknown): ReportFailure {
  if (!(error instanceof ProblemError)) return "unexpected";
  switch (error.problem.status) {
    case 409:
      return "duplicate";
    case 429:
      return "throttled";
    case 404:
      return "unavailable";
    case 400:
    case 413:
    case 415:
    case 422:
      return "invalid";
    default:
      return "unexpected";
  }
}

/**
 * failurePreservesInput decides whether the form keeps what the Student wrote.
 *
 * A refusal they can act on by editing, or one that may simply have been bad luck, keeps their
 * words. A duplicate and a throttle are not fixed by editing, so the form stops inviting it.
 */
export function failurePreservesInput(failure: ReportFailure): boolean {
  return failure === "invalid" || failure === "unexpected";
}

/**
 * failureAllowsRetry decides whether the submit control stays usable.
 *
 * Nothing here retries on its own — this only says whether a *Student* may press submit again.
 */
export function failureAllowsRetry(failure: ReportFailure): boolean {
  return failure === "invalid" || failure === "unexpected";
}

/**
 * isStaleReportScope is the navigation and late-response boundary.
 *
 * A response that arrives after the page moved on belongs to a target that is no longer displayed,
 * and a dialog opened for one target must never submit against another. Comparing the scope
 * captured at open time against the scope now displayed answers both.
 */
export function isStaleReportScope(openedScope: string, currentScope: string): boolean {
  return openedScope !== currentScope;
}
