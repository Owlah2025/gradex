import assert from "node:assert/strict";
import test from "node:test";
import { revisionThumbnailURL, THUMBNAIL_MAX_BYTES, validateThumbnailFile } from "./course-thumbnail";

test("thumbnail selection accepts bounded raster types and rejects empty, oversized and active content", () => {
  for (const type of ["image/jpeg", "image/png", "image/webp"]) {
    assert.equal(validateThumbnailFile({ type, size: THUMBNAIL_MAX_BYTES }), null);
  }
  assert.equal(validateThumbnailFile({ type: "image/svg+xml", size: 500 }), "type");
  assert.equal(validateThumbnailFile({ type: "image/jpeg", size: 0 }), "size");
  assert.equal(validateThumbnailFile({ type: "image/jpeg", size: THUMBNAIL_MAX_BYTES + 1 }), "size");
});

test("private cover URLs retain the exact course, revision, and immutable asset", () => {
  assert.equal(revisionThumbnailURL("course", "candidate", "asset"), "/api/v1/courses/course/revisions/candidate/thumbnails/asset/card");
  assert.equal(revisionThumbnailURL("course", "candidate", "asset", true), "/api/v1/admin/review/courses/course/revisions/candidate/thumbnails/asset/card");
});
