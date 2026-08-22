import assert from "node:assert/strict";
import test from "node:test";

import { isReturnedForChanges } from "./change-request-state";

test("a first-publish change request raises the notice", () => {
  assert.equal(isReturnedForChanges({ state: "CHANGES_REQUESTED" }), true);
});

test("a rejected revision of a published Course raises the same notice", () => {
  // FR-052: only the revision moves, the live Course keeps serving. The Instructor still has to
  // read a reason and resubmit, so the surface is identical.
  assert.equal(isReturnedForChanges({ state: "REJECTED" }), true);
});

test("a resubmitted revision does NOT keep showing the resolved change request", () => {
  // The server retains `review_reason` on the row after `submit` sets PENDING_REVIEW. Keying on
  // the reason instead of the state would tell the Instructor to fix work they already resubmitted.
  assert.equal(
    isReturnedForChanges({ state: "PENDING_REVIEW", review_reason: "Please update lesson 2" }),
    false,
  );
});

test("a draft carrying a stale reason from an earlier review shows no notice", () => {
  assert.equal(
    isReturnedForChanges({ state: "DRAFT", review_reason: "Please update lesson 2" }),
    false,
  );
});

test("approved and superseded revisions show no notice", () => {
  assert.equal(isReturnedForChanges({ state: "APPROVED" }), false);
  assert.equal(isReturnedForChanges({ state: "SUPERSEDED" }), false);
});

test("a missing revision or a revision with no state shows no notice", () => {
  assert.equal(isReturnedForChanges(undefined), false);
  assert.equal(isReturnedForChanges(null), false);
  assert.equal(isReturnedForChanges({}), false);
});

test("an unrecognised state is never treated as a change request", () => {
  assert.equal(isReturnedForChanges({ state: "SOMETHING_NEW" }), false);
});
