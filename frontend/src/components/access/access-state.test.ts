import assert from "node:assert/strict";
import test from "node:test";

import type { StudentCourseAccessHistoryItem } from "@/lib/api/access";
import {
  byStudentPriority,
  canOpenCourse,
  rejectionReason,
  studentAccessState,
} from "./access-state";

function item(
  over: Partial<StudentCourseAccessHistoryItem> = {},
): StudentCourseAccessHistoryItem {
  return {
    course_id: "c-1",
    course_title: "Course",
    has_active_access: false,
    ...over,
  };
}

function invitation(state: string, over: Record<string, unknown> = {}) {
  return {
    id: "i-1",
    course_id: "c-1",
    state,
    created_at: "2026-08-01T00:00:00Z",
    ...over,
  } as StudentCourseAccessHistoryItem["invitation"];
}

test("an active entitlement is ACTIVE regardless of the invitation record", () => {
  assert.equal(studentAccessState(item({ has_active_access: true })), "ACTIVE");
  assert.equal(
    studentAccessState(item({ has_active_access: true, invitation: invitation("APPROVED") })),
    "ACTIVE",
  );
});

test("an approved invitation with no active entitlement is ACCESS_ENDED, not ACTIVE", () => {
  // Reading the invitation alone would tell the Student they still hold a Course they cannot open.
  assert.equal(studentAccessState(item({ invitation: invitation("APPROVED") })), "ACCESS_ENDED");
});

test("acceptance and approval are distinct states", () => {
  assert.equal(
    studentAccessState(item({ invitation: invitation("PENDING_STUDENT_ACCEPTANCE") })),
    "ACTION_REQUIRED",
  );
  assert.equal(
    studentAccessState(item({ invitation: invitation("PENDING_ADMIN_APPROVAL") })),
    "AWAITING_APPROVAL",
  );
});

test("refusal states map through", () => {
  assert.equal(studentAccessState(item({ invitation: invitation("REJECTED") })), "REJECTED");
  assert.equal(studentAccessState(item({ invitation: invitation("CANCELLED") })), "CANCELLED");
});

test("a record with no invitation and no access is UNKNOWN, never guessed", () => {
  assert.equal(studentAccessState(item()), "UNKNOWN");
  assert.equal(studentAccessState(item({ invitation: invitation("SOMETHING_NEW") })), "UNKNOWN");
});

test("only an active record offers a route into the Course", () => {
  assert.equal(canOpenCourse("ACTIVE"), true);
  for (const state of ["ACTION_REQUIRED", "AWAITING_APPROVAL", "ACCESS_ENDED", "REJECTED", "CANCELLED", "UNKNOWN"] as const) {
    assert.equal(canOpenCourse(state), false, `${state} must not offer Go to course`);
  }
});

test("a decision reason is shown only for a rejection", () => {
  assert.equal(
    rejectionReason(item({ invitation: invitation("REJECTED", { decision_reason: "Payment not confirmed" }) })),
    "Payment not confirmed",
  );
  assert.equal(rejectionReason(item({ invitation: invitation("REJECTED") })), null);
  assert.equal(rejectionReason(item({ invitation: invitation("REJECTED", { decision_reason: "   " }) })), null);
  // Never leak an Admin note against a non-refusal.
  assert.equal(
    rejectionReason(item({ has_active_access: true, invitation: invitation("APPROVED", { decision_reason: "note" }) })),
    null,
  );
  assert.equal(
    rejectionReason(item({ invitation: invitation("CANCELLED", { decision_reason: "note" }) })),
    null,
  );
});

test("what needs the Student comes first, then usable access, then history", () => {
  const sorted = [
    item({ course_id: "d", invitation: invitation("CANCELLED") }),
    item({ course_id: "c", has_active_access: true }),
    item({ course_id: "b", invitation: invitation("PENDING_ADMIN_APPROVAL") }),
    item({ course_id: "a", invitation: invitation("PENDING_STUDENT_ACCEPTANCE") }),
  ].sort(byStudentPriority);

  assert.deepEqual(
    sorted.map((entry) => studentAccessState(entry)),
    ["ACTION_REQUIRED", "ACTIVE", "AWAITING_APPROVAL", "CANCELLED"],
  );
});
