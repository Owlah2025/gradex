import assert from "node:assert/strict";
import test from "node:test";

import { editsPublishedCourse, revisionWorkflow } from "./revision-workflow";

const liveRevision = { id: "rev-a", state: "APPROVED", title_ar: "أ", title_en: "A", sections: [] };

test("a DRAFT candidate is editable", () => {
  assert.equal(
    revisionWorkflow({ id: "c1", editable_revision: { ...liveRevision, id: "rev-b", state: "DRAFT" } }),
    "EDIT_CANDIDATE",
  );
});

test("a CHANGES_REQUESTED candidate is editable", () => {
  assert.equal(
    revisionWorkflow({ id: "c1", editable_revision: { ...liveRevision, id: "rev-b", state: "CHANGES_REQUESTED" } }),
    "EDIT_CANDIDATE",
  );
});

test("a PENDING_REVIEW candidate is in review, never editable and never a second revision", () => {
  // The backend returns the existing candidate rather than cloning another, so offering
  // "start a revision" here would promise something that cannot happen.
  assert.equal(
    revisionWorkflow({ id: "c1", editable_revision: { ...liveRevision, id: "rev-b", state: "PENDING_REVIEW" } }),
    "CANDIDATE_IN_REVIEW",
  );
});

test("a published Course with no candidate offers a new revision", () => {
  // The studio list payload carries `live_revision_id` only — never the expanded graph — so this is
  // the shape the action must actually work from.
  assert.equal(
    revisionWorkflow({ id: "c1", lifecycle: "PUBLISHED", live_revision_id: "rev-a" }),
    "START_REVISION",
  );
  // The detail payload expands it; both shapes must behave identically.
  assert.equal(
    revisionWorkflow({ id: "c1", lifecycle: "PUBLISHED", live_revision: liveRevision }),
    "START_REVISION",
  );
});

test("a Course with neither candidate nor live revision offers nothing", () => {
  // `CreateCandidate` has nothing to clone, so the action must not be shown.
  assert.equal(revisionWorkflow({ id: "c1", lifecycle: "DRAFT" }), "UNAVAILABLE");
});

test("an unrecognised candidate state is never treated as editable", () => {
  assert.equal(
    revisionWorkflow({ id: "c1", editable_revision: { ...liveRevision, id: "rev-b", state: "SUPERSEDED" } }),
    "UNAVAILABLE",
  );
});

test("a missing course offers nothing", () => {
  assert.equal(revisionWorkflow(undefined), "UNAVAILABLE");
  assert.equal(revisionWorkflow(null), "UNAVAILABLE");
});

test("editing a candidate behind a live revision warns; a first draft does not", () => {
  assert.equal(
    editsPublishedCourse({
      id: "c1",
      live_revision_id: "rev-a",
      editable_revision: { ...liveRevision, id: "rev-b", state: "DRAFT" },
    }),
    true,
  );
  assert.equal(
    editsPublishedCourse({ id: "c1", editable_revision: { ...liveRevision, id: "rev-b", state: "DRAFT" } }),
    false,
  );
});
