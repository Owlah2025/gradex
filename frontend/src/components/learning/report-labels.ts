import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { LearningReportReason } from "@/lib/api/learning";
import type { ReportFailure, ReportFieldError } from "./report-dialog-state";
import type { ReportTargetKind } from "./report-targets";

/**
 * The one place a wire value becomes display text.
 *
 * Backend enumerations are identifiers, not copy: `lab_material` and `suspected_copyright_violation`
 * are what the database stores and what the request sends, and neither is ever shown to a Student in
 * either language. Keeping the mapping here means a new reason cannot reach the interface as raw
 * English snake_case, because there would be no key for it.
 */

type LearningLabels = Dictionary["learning"];

/**
 * The report copy, and only the report copy.
 *
 * The dialog is a client component, so whatever it receives as props is serialized into the page's
 * payload. Handing it the whole learning dictionary would put unrelated strings — "Active access"
 * among them — into the markup of pages that must not contain them, which is how an expired Lesson
 * would start carrying active-state copy it never displays. The subset is built explicitly so that
 * cannot happen by accident.
 */
export type ReportLabels = Pick<
  LearningLabels,
  | "reportAction"
  | "reportCourseAction"
  | "reportLessonAction"
  | "reportVideoAction"
  | "reportResourceAction"
  | "reportLabMaterialAction"
  | "reportDialogTitle"
  | "reportDialogDescription"
  | "reportTargetLabel"
  | "reportTargetCourse"
  | "reportTargetLesson"
  | "reportTargetVideo"
  | "reportTargetResource"
  | "reportTargetLabMaterial"
  | "reportReasonLabel"
  | "reportReasonPlaceholder"
  | "reportReasonBrokenUnavailable"
  | "reportReasonInaccurate"
  | "reportReasonInappropriate"
  | "reportReasonSuspectedCopyrightViolation"
  | "reportReasonOther"
  | "reportExplanationLabel"
  | "reportExplanationOptional"
  | "reportExplanationRequired"
  | "reportReasonRequiredError"
  | "reportExplanationRequiredError"
  | "reportSubmit"
  | "reportSubmitting"
  | "reportCancel"
  | "reportClose"
  | "reportSuccessTitle"
  | "reportSuccessBody"
  | "reportDone"
  | "reportDuplicate"
  | "reportThrottled"
  | "reportUnavailable"
  | "reportInvalid"
  | "reportUnexpected"
>;

/** reportLabels picks exactly the report copy from a learning dictionary. */
export function reportLabels(labels: LearningLabels): ReportLabels {
  return {
    reportAction: labels.reportAction,
    reportCourseAction: labels.reportCourseAction,
    reportLessonAction: labels.reportLessonAction,
    reportVideoAction: labels.reportVideoAction,
    reportResourceAction: labels.reportResourceAction,
    reportLabMaterialAction: labels.reportLabMaterialAction,
    reportDialogTitle: labels.reportDialogTitle,
    reportDialogDescription: labels.reportDialogDescription,
    reportTargetLabel: labels.reportTargetLabel,
    reportTargetCourse: labels.reportTargetCourse,
    reportTargetLesson: labels.reportTargetLesson,
    reportTargetVideo: labels.reportTargetVideo,
    reportTargetResource: labels.reportTargetResource,
    reportTargetLabMaterial: labels.reportTargetLabMaterial,
    reportReasonLabel: labels.reportReasonLabel,
    reportReasonPlaceholder: labels.reportReasonPlaceholder,
    reportReasonBrokenUnavailable: labels.reportReasonBrokenUnavailable,
    reportReasonInaccurate: labels.reportReasonInaccurate,
    reportReasonInappropriate: labels.reportReasonInappropriate,
    reportReasonSuspectedCopyrightViolation: labels.reportReasonSuspectedCopyrightViolation,
    reportReasonOther: labels.reportReasonOther,
    reportExplanationLabel: labels.reportExplanationLabel,
    reportExplanationOptional: labels.reportExplanationOptional,
    reportExplanationRequired: labels.reportExplanationRequired,
    reportReasonRequiredError: labels.reportReasonRequiredError,
    reportExplanationRequiredError: labels.reportExplanationRequiredError,
    reportSubmit: labels.reportSubmit,
    reportSubmitting: labels.reportSubmitting,
    reportCancel: labels.reportCancel,
    reportClose: labels.reportClose,
    reportSuccessTitle: labels.reportSuccessTitle,
    reportSuccessBody: labels.reportSuccessBody,
    reportDone: labels.reportDone,
    reportDuplicate: labels.reportDuplicate,
    reportThrottled: labels.reportThrottled,
    reportUnavailable: labels.reportUnavailable,
    reportInvalid: labels.reportInvalid,
    reportUnexpected: labels.reportUnexpected,
  };
}

const targetLabelKeys: Record<ReportTargetKind, keyof ReportLabels> = {
  course: "reportTargetCourse",
  lesson: "reportTargetLesson",
  video: "reportTargetVideo",
  resource: "reportTargetResource",
  lab_material: "reportTargetLabMaterial",
};

const targetActionKeys: Record<ReportTargetKind, keyof ReportLabels> = {
  course: "reportCourseAction",
  lesson: "reportLessonAction",
  video: "reportVideoAction",
  resource: "reportResourceAction",
  lab_material: "reportLabMaterialAction",
};

const reasonLabelKeys: Record<LearningReportReason, keyof ReportLabels> = {
  broken_unavailable: "reportReasonBrokenUnavailable",
  inaccurate: "reportReasonInaccurate",
  inappropriate: "reportReasonInappropriate",
  suspected_copyright_violation: "reportReasonSuspectedCopyrightViolation",
  other: "reportReasonOther",
};

/**
 * failureMessageKeys maps each generic outcome to its message. Every one of them describes what the
 * Student can do, never why the server refused: the uniform `404` covers an expired context, ended
 * access, a foreign session, and a removed target, and this copy must not distinguish them.
 */
const failureMessageKeys: Record<ReportFailure, keyof ReportLabels> = {
  duplicate: "reportDuplicate",
  throttled: "reportThrottled",
  unavailable: "reportUnavailable",
  invalid: "reportInvalid",
  unexpected: "reportUnexpected",
};

const fieldErrorKeys: Record<ReportFieldError, keyof ReportLabels> = {
  reasonRequired: "reportReasonRequiredError",
  explanationRequired: "reportExplanationRequiredError",
};

export function reportTargetLabel(kind: ReportTargetKind, labels: ReportLabels): string {
  return labels[targetLabelKeys[kind]];
}

export function reportTargetActionLabel(kind: ReportTargetKind, labels: ReportLabels): string {
  return labels[targetActionKeys[kind]];
}

export function reportReasonLabel(reason: LearningReportReason, labels: ReportLabels): string {
  return labels[reasonLabelKeys[reason]];
}

export function reportFailureMessage(failure: ReportFailure, labels: ReportLabels): string {
  return labels[failureMessageKeys[failure]];
}

export function reportFieldErrorMessage(error: ReportFieldError, labels: ReportLabels): string {
  return labels[fieldErrorKeys[error]];
}
