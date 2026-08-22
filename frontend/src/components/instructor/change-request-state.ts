import type { CourseRevisionWire } from "@/lib/api/catalog";

/**
 * Revision states that mean the Admin returned the Course to its Instructor.
 *
 * `CHANGES_REQUESTED` is the first-publish path: `catalog/review.go` moves both the revision and
 * the Course lifecycle. `REJECTED` is the published-Course revision path (FR-052): only the
 * revision moves, and the live Course keeps serving. To the Instructor both mean the same thing —
 * work came back with a reason — so both raise the notice.
 */
const RETURNED_STATES = new Set(["CHANGES_REQUESTED", "REJECTED"]);

/**
 * Whether the Instructor should be told the Course was returned for changes.
 *
 * Deliberately keyed on `state` alone. The server retains `review_reason` on the revision row
 * across a resubmission — `submit` only sets `state = 'PENDING_REVIEW'` and never clears the
 * reason — so a surface that keyed on the presence of a reason would keep telling the Instructor
 * to fix something they have already fixed and resubmitted.
 */
export function isReturnedForChanges(
  revision: Pick<CourseRevisionWire, "state" | "review_reason"> | null | undefined,
): boolean {
  return revision?.state !== undefined && RETURNED_STATES.has(revision.state);
}
