import assert from "node:assert/strict";
import { test } from "node:test";
import { courseReportTargets, lessonReportTargets, reportTargetScope } from "./report-targets";
import { reportLabels } from "./report-labels";
import { en } from "../../lib/i18n/dictionaries/en";

/**
 * T066 surface evidence.
 *
 * The single rule under test: a report action exists exactly when the rendered read model carried a
 * context for that target, and never because the page happens to show something related.
 */

const CONTEXT = "grc1.opaque-course-context";

test("Course Home offers the Course target only when an active read issued a context", () => {
  assert.deepEqual(courseReportTargets({ learning_status: "active", report_context: CONTEXT }), [
    { kind: "course", context: CONTEXT },
  ]);
});

test("Course Home offers nothing without a context", () => {
  // Retained-expired: the Course still reads, and carries no context (D-065).
  assert.deepEqual(courseReportTargets({ learning_status: "expired", report_context: undefined }), []);
  // An active read that carried no context is still not reportable.
  assert.deepEqual(courseReportTargets({ learning_status: "active", report_context: undefined }), []);
  // A context on an expired read would be a server defect; the client still refuses to offer it.
  assert.deepEqual(courseReportTargets({ learning_status: "expired", report_context: CONTEXT }), []);
});

test("a Lesson offers exactly the kinds it received contexts for", () => {
  const all = lessonReportTargets({
    learning_status: "active",
    report_contexts: { lesson: "l", video: "v", resource: "r", lab_material: "m" },
  });
  assert.deepEqual(all, [
    { kind: "lesson", context: "l" },
    { kind: "video", context: "v" },
    { kind: "resource", context: "r" },
    { kind: "lab_material", context: "m" },
  ]);
});

test("a Lesson without a video context offers no video action", () => {
  const targets = lessonReportTargets({
    learning_status: "active",
    report_contexts: { lesson: "l", resource: "r" },
  });
  assert.deepEqual(targets.map((target) => target.kind), ["lesson", "resource"]);
});

test("a Lesson without material contexts offers no material actions", () => {
  const targets = lessonReportTargets({
    learning_status: "active",
    report_contexts: { lesson: "l", video: "v" },
  });
  assert.deepEqual(targets.map((target) => target.kind), ["lesson", "video"]);
});

test("only the Lab Material context produces the Lab Material action", () => {
  const targets = lessonReportTargets({
    learning_status: "active",
    report_contexts: { lesson: "l", lab_material: "m" },
  });
  assert.deepEqual(targets.map((target) => target.kind), ["lesson", "lab_material"]);
});

test("expired and unavailable Lesson reads offer no actions at all", () => {
  assert.deepEqual(lessonReportTargets({ learning_status: "expired", report_contexts: undefined }), []);
  assert.deepEqual(lessonReportTargets({ learning_status: "active", report_contexts: undefined }), []);
  assert.deepEqual(
    lessonReportTargets({ learning_status: "expired", report_contexts: { lesson: "l", video: "v" } }),
    [],
  );
});

test("an empty context string is not a context", () => {
  // Absence is omission, never an empty string — a blank value must not become an action.
  assert.deepEqual(courseReportTargets({ learning_status: "active", report_context: "" }), []);
  assert.deepEqual(
    lessonReportTargets({ learning_status: "active", report_contexts: { lesson: "", video: "v" } }).map((t) => t.kind),
    ["video"],
  );
});

test("target order is stable so the interface does not reshuffle between reads", () => {
  const first = lessonReportTargets({
    learning_status: "active",
    report_contexts: { lesson: "l", lab_material: "m", resource: "r", video: "v" },
  });
  const second = lessonReportTargets({
    learning_status: "active",
    report_contexts: { video: "v", lesson: "l", resource: "r", lab_material: "m" },
  });
  assert.deepEqual(first, second);
});

test("scope distinguishes every target a dialog could belong to", () => {
  const courseScope = reportTargetScope("course-1", null, "course");
  assert.notEqual(courseScope, reportTargetScope("course-2", null, "course"));
  assert.notEqual(reportTargetScope("course-1", "lesson-a", "lesson"), reportTargetScope("course-1", "lesson-b", "lesson"));
  assert.notEqual(reportTargetScope("course-1", "lesson-a", "video"), reportTargetScope("course-1", "lesson-a", "lesson"));
  assert.equal(courseScope, reportTargetScope("course-1", null, "course"));
});

test("the client payload carries report copy only, never unrelated learning strings", () => {
  // A client component's props are serialized into the page payload. Handing the dialog the whole
  // learning dictionary put "Active access" into the markup of an expired Lesson, which the
  // retained-expired E2E rightly refuses. The subset is what keeps that from recurring.
  const picked = reportLabels(en.learning);
  const keys = Object.keys(picked);
  assert.ok(keys.length > 0);
  for (const key of keys) {
    assert.ok(key.startsWith("report"), `${key} is not report copy`);
  }
  for (const value of Object.values(picked)) {
    assert.notEqual(value, en.learning.active, "active-access copy reached the report payload");
    assert.notEqual(value, en.learning.expired, "expired copy reached the report payload");
  }
  // Every report key in the dictionary is carried; a new one cannot be silently dropped.
  const dictionaryReportKeys = Object.keys(en.learning).filter((key) => key.startsWith("report")).sort();
  assert.deepEqual(keys.sort(), dictionaryReportKeys);
});
