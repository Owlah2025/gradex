import type { AdminCoursePreview } from "@/lib/api/review";

/**
 * What an Admin asked to preview.
 *
 * All three parts are supplied by the caller from the revision it is displaying, never from the
 * response — comparing a response against itself proves nothing.
 */
export type ExpectedPreviewIdentity = {
  courseID: string;
  revisionID: string;
  previewAssetVersionID: string;
};

/**
 * Whether an issued preview is the one that was asked for, in full.
 *
 * # WHY ALL THREE, AND WHY THIS IS A FUNCTION
 *
 * The server is authoritative and already binds the Asset Version to the revision under review, so
 * this is defence in depth rather than the security boundary. It exists because an Admin's decision
 * is *about* one exact revision: approving revision N while watching revision M's media is the one
 * mistake this screen must never help someone make, and a client that checks two of the three
 * identifiers is a client that would not notice the third going wrong.
 *
 * The Lesson preview has applied that rule since it was written. The course preview claimed it in
 * its own documentation while checking only the Course and the revision, so the Asset Version — the
 * part that actually names the bytes about to be played — went unverified.
 *
 * It is a plain function so the comparison itself can be tested exhaustively without a browser, and
 * so no future edit to the component can quietly drop a field from the check.
 */
export function issuedPreviewMatches(
  issued: Pick<AdminCoursePreview, "course_id" | "revision_id" | "preview_asset_version_id" | "url">,
  expected: ExpectedPreviewIdentity,
): boolean {
  return (
    issued.course_id === expected.courseID &&
    issued.revision_id === expected.revisionID &&
    issued.preview_asset_version_id === expected.previewAssetVersionID &&
    // An empty URL is not a preview. Mounting a media element on one produces a decode error the
    // Admin would have to interpret, where a refusal says plainly that nothing was issued.
    typeof issued.url === "string" &&
    issued.url.length > 0
  );
}
