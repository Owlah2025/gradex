import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { issuedPreviewMatches } from "./review-preview-identity";

/**
 * The Admin candidate-preview identity guard.
 *
 * The server binds the Asset Version to the revision under review, so this is defence in depth. It
 * is asserted exhaustively anyway because the failure it prevents is an Admin approving one
 * revision while watching another's media, and because the guard previously *claimed* to check the
 * Asset Version while checking only two thirds of the identity.
 */

const EXPECTED = {
  courseID: "course-1",
  revisionID: "revision-1",
  previewAssetVersionID: "asset-1",
};

const ISSUED = {
  course_id: "course-1",
  revision_id: "revision-1",
  preview_asset_version_id: "asset-1",
  url: "https://storage.example/signed/preview.mp4",
};

test("the exact issuance the screen asked for is accepted", () => {
  assert.equal(issuedPreviewMatches(ISSUED, EXPECTED), true);
});

test("every identifier is load-bearing, including the Asset Version", () => {
  // One case per field. The Asset Version case is the one that regressed: with it unchecked, a
  // response naming the right Course and revision but a different Asset would have been mounted.
  for (const [field, wrong] of [
    ["course_id", "course-2"],
    ["revision_id", "revision-2"],
    ["preview_asset_version_id", "asset-2"],
  ] as const) {
    assert.equal(
      issuedPreviewMatches({ ...ISSUED, [field]: wrong }, EXPECTED),
      false,
      `${field} must be compared`,
    );
  }
});

test("an issuance with no URL is refused rather than mounted", () => {
  assert.equal(issuedPreviewMatches({ ...ISSUED, url: "" }, EXPECTED), false);
});

test("the component refuses through this guard rather than a hand-written comparison", () => {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
  const component = fs.readFileSync(
    path.join(root, "src/components/admin/review-course-preview.tsx"),
    "utf8",
  );
  // The guard must be the single decision point: an inline comparison beside it is how one of the
  // three fields fell out of the check in the first place.
  assert.match(component, /if \(!issuedPreviewMatches\(issued, \{ courseID, revisionID, previewAssetVersionID \}\)\)/);
  assert.ok(
    !/issued\.course_id !==|issued\.revision_id !==|issued\.preview_asset_version_id !==/.test(component),
    "the component must not re-implement the comparison inline",
  );
});
