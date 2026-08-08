import assert from "node:assert/strict";
import test from "node:test";
import {
  parseProgressSnapshot,
  parseLearningStateSnapshot,
  requireNoProgressRow,
  requireProgressRow,
  type ProgressSnapshot,
} from "./e2e-progress";

const foundSnapshot: ProgressSnapshot = {
  found: true,
  max_position_seconds: 45.5,
  position_seconds: 42.25,
  completed: false,
  completed_at: "",
  asset_version_id: "",
  updated_at: "2026-08-02T12:00:00Z",
};

test("parses a found snapshot with position, completion, and Asset Version binding", () => {
  const parsed = parseProgressSnapshot(
    JSON.stringify({
      found: true,
      max_position_seconds: 300,
      position_seconds: 300,
      completed: true,
      completed_at: "2026-08-02T12:00:00Z",
      asset_version_id: "60000000-0000-0000-0000-000000000001",
      updated_at: "2026-08-02T12:00:01Z",
    })
  );
  assert.equal(parsed.found, true);
  assert.equal(parsed.max_position_seconds, 300);
  assert.equal(parsed.completed, true);
  assert.equal(parsed.asset_version_id, "60000000-0000-0000-0000-000000000001");
});

test("parses an explicit absent snapshot without inventing a zero row", () => {
  const parsed = parseProgressSnapshot(JSON.stringify({ found: false }));
  assert.equal(parsed.found, false);
  assert.equal(parsed.position_seconds, 0);
});

test("rejects empty helper output rather than treating it as absence", () => {
  assert.throws(() => parseProgressSnapshot("   "), /did not run/);
});

test("rejects non-JSON helper output", () => {
  assert.throws(() => parseProgressSnapshot("lesson_progress does not exist"), /non-JSON/);
});

test("rejects a snapshot with no boolean found field", () => {
  assert.throws(() => parseProgressSnapshot(JSON.stringify({ position_seconds: 12 })), /unusable snapshot/);
});

test("requireProgressRow returns the row when the Progress row exists", () => {
  assert.equal(requireProgressRow(foundSnapshot, "active Student, Lesson 1").position_seconds, 42.25);
});

// This is the guard for the defect that made the previous PostgreSQL evidence a no-op: an
// expected row reporting found:false must fail the assertion, not pass as an unchanged zero.
test("requireProgressRow throws when an expected row reports found:false", () => {
  const missing = parseProgressSnapshot(JSON.stringify({ found: false }));
  assert.throws(
    () => requireProgressRow(missing, "active Student, Lesson 1"),
    /Expected a persisted Progress row for active Student, Lesson 1/
  );
});

test("requireProgressRow refuses to treat a zero position as evidence of persistence", () => {
  const zeroed = parseProgressSnapshot(JSON.stringify({ found: false, position_seconds: 0, completed: false }));
  assert.throws(() => requireProgressRow(zeroed, "any Student"), /never evidence of unchanged state/);
});

test("requireNoProgressRow throws when a row unexpectedly exists", () => {
  assert.throws(() => requireNoProgressRow(foundSnapshot, "unenrolled Student"), /Expected no Progress row/);
});

test("requireNoProgressRow accepts genuine absence", () => {
  assert.doesNotThrow(() => requireNoProgressRow(parseProgressSnapshot(JSON.stringify({ found: false })), "unenrolled Student"));
});

const absentLearningState = {
  entitlement: { found: false, count: 0 },
  enrollment: { found: false, count: 0 },
  progress: [],
  material_kinds: {},
  video_asset_version_state: "READY",
};

test("parses an explicit zero-grant learning state", () => {
  const parsed = parseLearningStateSnapshot(JSON.stringify(absentLearningState));
  assert.equal(parsed.entitlement.count, 0);
  assert.equal(parsed.enrollment.count, 0);
  assert.deepEqual(parsed.progress, []);
});

test("rejects missing learning-state helper output", () => {
  assert.throws(() => parseLearningStateSnapshot(""), /did not run/);
});

test("rejects a found Entitlement without Invitation provenance", () => {
  assert.throws(
    () => parseLearningStateSnapshot(JSON.stringify({
      ...absentLearningState,
      entitlement: { found: true, count: 1, id: "entitlement-1", grant_source: "MANUAL_INVITATION" },
    })),
    /missing identity or grant provenance/
  );
});

test("rejects a found Enrollment without its identity", () => {
  assert.throws(
    () => parseLearningStateSnapshot(JSON.stringify({
      ...absentLearningState,
      enrollment: { found: true, count: 1 },
    })),
    /missing its identity/
  );
});
