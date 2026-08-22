import type { OwnedCourseSummary } from "@/lib/api/catalog";

/**
 * What the studio can offer for one owned Course.
 *
 * Derived from the server's own revision states, never from the Course lifecycle alone. The
 * backend keeps at most one active candidate per Course — `CreateCandidate` returns the existing
 * `DRAFT`/`CHANGES_REQUESTED`/`PENDING_REVIEW` revision rather than cloning a second one
 * (`catalog/authoring.go:420`) — so the studio must mirror that instead of inventing its own rule.
 */
export type RevisionWorkflow =
  /** An editable candidate exists: DRAFT or CHANGES_REQUESTED. */
  | "EDIT_CANDIDATE"
  /** The candidate is with the Admin. Editing is closed until a decision lands. */
  | "CANDIDATE_IN_REVIEW"
  /** Published with no candidate — the Instructor may begin a new revision from the live one. */
  | "START_REVISION"
  /** No candidate and nothing live to clone. Nothing the Instructor can do here. */
  | "UNAVAILABLE";

const EDITABLE_STATES = new Set(["DRAFT", "CHANGES_REQUESTED"]);

export function revisionWorkflow(course: OwnedCourseSummary | null | undefined): RevisionWorkflow {
  if (!course) return "UNAVAILABLE";

  const candidateState = course.editable_revision?.state;
  if (candidateState !== undefined) {
    if (EDITABLE_STATES.has(candidateState)) return "EDIT_CANDIDATE";
    if (candidateState === "PENDING_REVIEW") return "CANDIDATE_IN_REVIEW";
    // Any other state on the active candidate is not something the studio may edit.
    return "UNAVAILABLE";
  }

  // No candidate. `CreateCandidate` clones the live revision, so a live revision is exactly the
  // precondition for offering the action — a Course that was never published has nothing to clone
  // and the server refuses (`catalog/authoring.go:441`).
  return hasLiveRevision(course) ? "START_REVISION" : "UNAVAILABLE";
}

/**
 * `ListOwnedCourses` returns `live_revision_id` but not the expanded `live_revision` graph — only
 * the detail read expands it. Reading the id keeps this correct on both payloads; reading the graph
 * would silently report "no published revision" for every Course in the studio list.
 */
function hasLiveRevision(course: OwnedCourseSummary): boolean {
  return Boolean(course.live_revision_id) || Boolean(course.live_revision?.id);
}

/**
 * Whether the candidate being edited is a revision of already-published content.
 *
 * Drives the notice that edits do not reach Students until the revision is reviewed and approved.
 * A first-publication draft has no live revision behind it, so it needs no such warning.
 */
export function editsPublishedCourse(course: OwnedCourseSummary | null | undefined): boolean {
  return Boolean(course) && hasLiveRevision(course!) && Boolean(course?.editable_revision?.id);
}
