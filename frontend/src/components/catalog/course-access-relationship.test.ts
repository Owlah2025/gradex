import assert from "node:assert/strict";
import test from "node:test";

import type { StudentCourseAccessHistoryItem } from "@/lib/api/access";
import {
  courseAccessRelationship,
  offersAccessStatus,
  offersCourseEntry,
} from "./course-access-relationship";

const COURSE = "course-1";
const OTHER = "course-2";

function record(
  over: Partial<StudentCourseAccessHistoryItem> = {},
): StudentCourseAccessHistoryItem {
  return { course_id: COURSE, course_title: "Course", has_active_access: false, ...over };
}

function invitation(state: string) {
  return { id: "i-1", course_id: COURSE, state, created_at: "2026-08-01T00:00:00Z" } as
    StudentCourseAccessHistoryItem["invitation"];
}

test("an anonymous visitor is ANONYMOUS, never NO_ACCESS", () => {
  assert.equal(courseAccessRelationship({ status: "anonymous" }, COURSE), "ANONYMOUS");
});

test("a signed-in Student with no record for this Course is NO_ACCESS", () => {
  assert.equal(courseAccessRelationship({ status: "loaded", items: [] }, COURSE), "NO_ACCESS");
  // Records for other Courses must not leak into this Course's state.
  assert.equal(
    courseAccessRelationship({ status: "loaded", items: [record({ course_id: OTHER, has_active_access: true })] }, COURSE),
    "NO_ACCESS",
  );
});

test("a failed lookup is UNAVAILABLE, never NO_ACCESS", () => {
  // Guessing "no access" would hide a real entitlement behind a transient error.
  assert.equal(courseAccessRelationship({ status: "failed" }, COURSE), "UNAVAILABLE");
});

test("the ST-07 states carry through unchanged", () => {
  const cases: [string, string][] = [
    ["PENDING_STUDENT_ACCEPTANCE", "ACTION_REQUIRED"],
    ["PENDING_ADMIN_APPROVAL", "AWAITING_APPROVAL"],
    ["APPROVED", "ACCESS_ENDED"],
    ["REJECTED", "REJECTED"],
    ["CANCELLED", "CANCELLED"],
  ];
  for (const [wire, expected] of cases) {
    assert.equal(
      courseAccessRelationship({ status: "loaded", items: [record({ invitation: invitation(wire) })] }, COURSE),
      expected,
      `${wire} must resolve to ${expected}`,
    );
  }
});

test("an active entitlement outranks an older refused invitation", () => {
  assert.equal(
    courseAccessRelationship(
      { status: "loaded", items: [record({ has_active_access: true, invitation: invitation("REJECTED") })] },
      COURSE,
    ),
    "ACTIVE",
  );
});

test("with several rows for one Course, the row holding access decides", () => {
  // Ordering is never trusted: the row that decides whether the Course opens is the one that wins.
  assert.equal(
    courseAccessRelationship(
      {
        status: "loaded",
        items: [
          record({ invitation: invitation("REJECTED") }),
          record({ has_active_access: true, invitation: invitation("APPROVED") }),
        ],
      },
      COURSE,
    ),
    "ACTIVE",
  );
});

test("only an active entitlement offers entry to the Course", () => {
  assert.equal(offersCourseEntry("ACTIVE"), true);
  for (const state of ["ANONYMOUS", "NO_ACCESS", "ACTION_REQUIRED", "AWAITING_APPROVAL", "ACCESS_ENDED", "REJECTED", "CANCELLED", "UNAVAILABLE", "UNKNOWN"] as const) {
    assert.equal(offersCourseEntry(state), false, `${state} must not offer Course entry`);
  }
});

test("the access-status route is offered only where there is a record to inspect", () => {
  for (const state of ["ACTION_REQUIRED", "AWAITING_APPROVAL", "ACCESS_ENDED", "REJECTED", "CANCELLED"] as const) {
    assert.equal(offersAccessStatus(state), true, `${state} should link to access status`);
  }
  for (const state of ["ANONYMOUS", "NO_ACCESS", "ACTIVE", "UNAVAILABLE", "UNKNOWN"] as const) {
    assert.equal(offersAccessStatus(state), false, `${state} should not link to access status`);
  }
});
