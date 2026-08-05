import assert from "node:assert/strict";
import { test } from "node:test";
import { ProblemError } from "../../lib/api/problem";
import { learningReportReasons } from "../../lib/api/learning";
import { ar } from "../../lib/i18n/dictionaries/ar";
import { en } from "../../lib/i18n/dictionaries/en";
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
} from "./report-dialog-state";
import {
  reportFailureMessage,
  reportFieldErrorMessage,
  reportReasonLabel,
  reportTargetActionLabel,
  reportTargetLabel,
} from "./report-labels";
import type { ReportTargetKind } from "./report-targets";

/** A problem body in the exact shape the server sends. */
function problem(status: number, code: string): ProblemError {
  return new ProblemError({
    type: `https://api.gradex.com/problems/${code.toLowerCase().replace(/_/g, "-")}`,
    title: "Refused",
    status,
    code,
  });
}

test("the reason set is exactly the server's closed enumeration", () => {
  assert.deepEqual([...learningReportReasons], [
    "broken_unavailable",
    "inaccurate",
    "inappropriate",
    "suspected_copyright_violation",
    "other",
  ]);
});

test("a reason must be chosen before the form can be submitted", () => {
  const empty = initialReportFormState();
  assert.equal(empty.reason, "");
  assert.equal(reportFieldError(empty), "reasonRequired");
  assert.equal(canSubmitReport(empty, "editing"), false);
});

test("every reason except other submits without an explanation", () => {
  for (const reason of learningReportReasons) {
    if (reason === "other") continue;
    const state = { reason, explanation: "" };
    assert.equal(explanationIsRequired(reason), false, reason);
    assert.equal(reportFieldError(state), null, reason);
    assert.equal(canSubmitReport(state, "editing"), true, reason);
  }
});

test("other requires a non-blank explanation, matching the database constraint", () => {
  assert.equal(explanationIsRequired("other"), true);
  assert.equal(reportFieldError({ reason: "other", explanation: "" }), "explanationRequired");
  // Whitespace is not an explanation: the server trims before checking, so the form does too.
  assert.equal(reportFieldError({ reason: "other", explanation: "   \n\t " }), "explanationRequired");
  assert.equal(reportFieldError({ reason: "other", explanation: "the audio is missing" }), null);
});

test("Arabic and Unicode explanations satisfy the requirement", () => {
  assert.equal(reportFieldError({ reason: "other", explanation: "الصوت مفقود" }), null);
  assert.equal(reportFieldError({ reason: "other", explanation: "🎧 لا يوجد صوت" }), null);
});

test("no second submission is possible while one is in flight", () => {
  const ready = { reason: "inaccurate" as const, explanation: "" };
  assert.equal(canSubmitReport(ready, "editing"), true);
  assert.equal(canSubmitReport(ready, "submitting"), false);
  assert.equal(canSubmitReport(ready, "acknowledged"), false);
});

test("each server outcome maps to one generic classification", () => {
  assert.equal(classifyReportFailure(problem(409, "STATE_CONFLICT")), "duplicate");
  assert.equal(classifyReportFailure(problem(429, "RATE_LIMITED")), "throttled");
  assert.equal(classifyReportFailure(problem(404, "NOT_FOUND")), "unavailable");
  for (const status of [400, 413, 415, 422]) {
    assert.equal(classifyReportFailure(problem(status, "INVALID")), "invalid", String(status));
  }
  assert.equal(classifyReportFailure(problem(500, "INTERNAL")), "unexpected");
  assert.equal(classifyReportFailure(new TypeError("Failed to fetch")), "unexpected");
  assert.equal(classifyReportFailure(undefined), "unexpected");
});

test("the uniform refusal is one outcome regardless of its cause", () => {
  // The server answers 404 for an expired context, ended access, a foreign session, and a removed
  // target alike. Every one of them must reach the same branch and the same sentence.
  const causes = ["NOT_FOUND", "NOT_FOUND", "NOT_FOUND"].map(() => problem(404, "NOT_FOUND"));
  const classified = new Set(causes.map(classifyReportFailure));
  assert.deepEqual([...classified], ["unavailable"]);
});

test("only fixable failures keep the Student's typing and the submit control", () => {
  assert.equal(failurePreservesInput("invalid"), true);
  assert.equal(failurePreservesInput("unexpected"), true);
  assert.equal(failurePreservesInput("duplicate"), false);
  assert.equal(failurePreservesInput("throttled"), false);
  assert.equal(failureAllowsRetry("duplicate"), false);
  assert.equal(failureAllowsRetry("throttled"), false);
  assert.equal(failureAllowsRetry("unexpected"), true);
});

test("a response for another target is stale and cannot update the page", () => {
  assert.equal(isStaleReportScope("c1 l-a lesson", "c1 l-b lesson"), true);
  assert.equal(isStaleReportScope("c1 l-a lesson", "c1 l-a video"), true);
  assert.equal(isStaleReportScope("c1  course", "c2  course"), true);
  assert.equal(isStaleReportScope("c1 l-a lesson", "c1 l-a lesson"), false);
});

test("every reason, target, failure, and field error has localized copy in both languages", () => {
  const kinds: ReportTargetKind[] = ["course", "lesson", "video", "resource", "lab_material"];
  const failures: ReportFailure[] = ["duplicate", "throttled", "unavailable", "invalid", "unexpected"];

  for (const labels of [en.learning, ar.learning]) {
    for (const reason of learningReportReasons) {
      const label = reportReasonLabel(reason, labels);
      assert.ok(label.length > 0, reason);
      // A wire value must never be shown: no snake_case identifier reaches the interface.
      assert.ok(!label.includes(reason), `${reason} was rendered as its wire value`);
      assert.ok(!/_/.test(label), `${reason} label looks like an enum: ${label}`);
    }
    for (const kind of kinds) {
      assert.ok(reportTargetLabel(kind, labels).length > 0, kind);
      assert.ok(!reportTargetLabel(kind, labels).includes("_"), kind);
      assert.ok(reportTargetActionLabel(kind, labels).length > 0, kind);
    }
    for (const failure of failures) {
      assert.ok(reportFailureMessage(failure, labels).length > 0, failure);
    }
    assert.ok(reportFieldErrorMessage("reasonRequired", labels).length > 0);
    assert.ok(reportFieldErrorMessage("explanationRequired", labels).length > 0);
  }
});

test("Arabic report copy is authored, not an English fallback", () => {
  const arabic = /[؀-ۿ]/;
  const reportKeys = Object.keys(en.learning).filter((key) => key.startsWith("report"));
  assert.ok(reportKeys.length > 0);
  for (const key of reportKeys as Array<keyof typeof en.learning>) {
    assert.notEqual(ar.learning[key], en.learning[key], `${String(key)} is untranslated`);
    assert.ok(arabic.test(ar.learning[key]), `${String(key)} is not Arabic: ${ar.learning[key]}`);
  }
});

test("no message describes a cause, a queue, or another report", () => {
  // "attempts" alone is ordinary English in "too many report attempts"; what may never appear is a
  // count, a policy, or a cause.
  const forbidden = [
    "entitlement", "enrollment", "session", "revision", "version", "expired",
    "queue", "position", "moderat", "review", "priority", "remaining",
    "attempts left", "attempts used", "attempts remaining", "quota",
    "5 ", "per hour", "an hour", "replaced", "no longer exists", "already reported by",
  ];
  const reportKeys = Object.keys(en.learning).filter((key) => key.startsWith("report"));
  for (const key of reportKeys as Array<keyof typeof en.learning>) {
    const english = en.learning[key].toLowerCase();
    for (const term of forbidden) {
      assert.ok(!english.includes(term), `${String(key)} discloses "${term}": ${en.learning[key]}`);
    }
  }
});
