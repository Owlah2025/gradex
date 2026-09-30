import assert from "node:assert/strict";
import test from "node:test";

import { announcementFieldLength, validateAnnouncementDraft } from "./announcement-validation";

test("announcement validation reports each field without hiding the other", () => {
  assert.deepEqual(validateAnnouncementDraft("", ""), { title: "REQUIRED", body: "REQUIRED" });
  assert.deepEqual(validateAnnouncementDraft("Title", "Message"), {});
  assert.equal(announcementFieldLength("مقرر"), 4);
});

test("announcement validation uses the server limits in Unicode characters", () => {
  assert.equal(validateAnnouncementDraft("x".repeat(140), "y".repeat(4000)).title, undefined);
  assert.equal(validateAnnouncementDraft("x".repeat(141), "y".repeat(4001)).title, "TOO_LONG");
  assert.equal(validateAnnouncementDraft("x".repeat(141), "y".repeat(4001)).body, "TOO_LONG");
});
