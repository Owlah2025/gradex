import assert from "node:assert/strict";
import { test } from "node:test";
import { hasPendingAccess, pendingAccessSummary } from "./pending-access-summary";
import type { StudentCourseAccessHistoryItem } from "../../lib/api/access";

/**
 * MVP-F15 / ST-08 — the Dashboard's pending Course-access summary.
 *
 * The two pending states are decided by different actors and must never be merged: one is waiting
 * on the Student, the other on an Admin.
 */

function item(
  state: string | null,
  hasActiveAccess = false,
): StudentCourseAccessHistoryItem {
  return {
    course_id: "c0000000-0000-0000-0000-000000000001",
    course_title: "A Course",
    has_active_access: hasActiveAccess,
    invitation: state ? ({ state } as StudentCourseAccessHistoryItem["invitation"]) : null,
  };
}

test("an unaccepted invitation counts as Student action required", () => {
  assert.deepEqual(pendingAccessSummary([item("PENDING_STUDENT_ACCEPTANCE")]), {
    actionRequired: 1,
    awaitingApproval: 0,
  });
});

test("an accepted invitation with no decision counts as awaiting Admin", () => {
  assert.deepEqual(pendingAccessSummary([item("PENDING_ADMIN_APPROVAL")]), {
    actionRequired: 0,
    awaitingApproval: 1,
  });
});

test("the two pending states are counted separately, never merged", () => {
  const summary = pendingAccessSummary([
    item("PENDING_STUDENT_ACCEPTANCE"),
    item("PENDING_ADMIN_APPROVAL"),
    item("PENDING_ADMIN_APPROVAL"),
  ]);
  assert.deepEqual(summary, { actionRequired: 1, awaitingApproval: 2 });
});

test("settled records never appear as pending", () => {
  const settled = [
    item("APPROVED", true), // active access
    item("APPROVED"), // access ended
    item("REJECTED"),
    item("CANCELLED"),
    item(null), // unclassifiable
  ];
  assert.deepEqual(pendingAccessSummary(settled), { actionRequired: 0, awaitingApproval: 0 });
  assert.equal(hasPendingAccess(pendingAccessSummary(settled)), false);
});

test("an active entitlement outranks its own invitation state", () => {
  // F12's rule: `has_active_access` wins. A Student who can already open the Course is not
  // waiting for anything, whatever the invitation record still says.
  assert.deepEqual(pendingAccessSummary([item("PENDING_ADMIN_APPROVAL", true)]), {
    actionRequired: 0,
    awaitingApproval: 0,
  });
});

test("absent or empty history yields nothing to show", () => {
  assert.deepEqual(pendingAccessSummary(null), { actionRequired: 0, awaitingApproval: 0 });
  assert.deepEqual(pendingAccessSummary(undefined), { actionRequired: 0, awaitingApproval: 0 });
  assert.equal(hasPendingAccess(pendingAccessSummary([])), false);
});

test("any pending record makes the summary worth rendering", () => {
  assert.equal(hasPendingAccess(pendingAccessSummary([item("PENDING_STUDENT_ACCEPTANCE")])), true);
  assert.equal(hasPendingAccess(pendingAccessSummary([item("PENDING_ADMIN_APPROVAL")])), true);
});
