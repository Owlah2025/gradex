import assert from "node:assert/strict";
import test from "node:test";
import type { CourseLifecycleSummary } from "../../lib/api/catalog";
import type { ReviewQueueItem } from "../../lib/api/review";
import {
  buildDirectory,
  courseStatusView,
  filterCounts,
  matchesFilter,
} from "./course-status";

function summary(overrides: Partial<CourseLifecycleSummary> & { id: string }): CourseLifecycleSummary {
  return {
    title_ar: "دورة",
    title_en: "Course",
    owner_display_name: "Instructor One",
    lifecycle: "DRAFT",
    updated_at: "2026-08-26T00:00:00Z",
    ...overrides,
  };
}

function queueItem(overrides: Partial<ReviewQueueItem> & { course_id: string }): ReviewQueueItem {
  return {
    owner_account_id: "a0000000-0000-0000-0000-000000000003",
    revision_id: `${overrides.course_id}-rev`,
    revision_number: 1,
    title_ar: "دورة مُرسلة",
    title_en: "Submitted Course",
    submitted_at: "2026-08-26T00:00:00Z",
    course_lifecycle: "PENDING_REVIEW",
    is_first_publish: true,
    ...overrides,
  };
}

test("a pending decision outside the bounded directory page still reaches Needs review", () => {
  // The lifecycle directory is capped by the server and ordered by recency, so a Course awaiting a
  // decision drops out of it as soon as enough other Courses are touched. The review queue is the
  // authority on pending decisions and is not bounded by that page.
  const page = Array.from({ length: 50 }, (_, index) => summary({ id: `listed-${index}` }));
  const stranded = queueItem({ course_id: "outside-the-page" });

  const rows = buildDirectory(page, [stranded]);

  const needsReview = rows.filter((row) => matchesFilter(row, "NEEDS_REVIEW"));
  assert.equal(needsReview.length, 1, "the queue entry must produce a row even when unlisted");
  assert.equal(needsReview[0].id, "outside-the-page");
  assert.equal(needsReview[0].fromQueueOnly, true);
  assert.equal(needsReview[0].pendingReview?.revision_id, "outside-the-page-rev");
});

test("a queue-only row states what the queue carries and leaves the rest absent", () => {
  const [row] = buildDirectory([], [queueItem({ course_id: "c1" })]);

  // Titles and lifecycle are read from the queue entry, so they are real values.
  assert.equal(row.titleEn, "Submitted Course");
  assert.equal(row.lifecycle, "PENDING_REVIEW");
  // The queue carries neither of these. They stay absent rather than being filled with a
  // placeholder that would read to an Admin as data.
  assert.equal(row.ownerDisplayName, "");
  assert.equal(row.updatedAt, null);
});

test("a course listed in both reads is joined, not duplicated", () => {
  const rows = buildDirectory(
    [summary({ id: "c1", lifecycle: "PUBLISHED" })],
    [queueItem({ course_id: "c1", course_lifecycle: "PUBLISHED" })],
  );

  assert.equal(rows.length, 1, "one Course must not appear twice");
  assert.equal(rows[0].fromQueueOnly, false, "the directory row is richer and wins");
  assert.equal(rows[0].ownerDisplayName, "Instructor One");
  assert.equal(courseStatusView(rows[0]).needsReview, true);
});

test("a published course with a pending revision is review work, and its lifecycle is unchanged", () => {
  // The case that makes lifecycle the wrong source for the queue: the revision is PENDING_REVIEW
  // while the Course itself stays PUBLISHED.
  const rows = buildDirectory(
    [summary({ id: "c1", lifecycle: "PUBLISHED" })],
    [queueItem({ course_id: "c1", course_lifecycle: "PUBLISHED", is_first_publish: false })],
  );

  const view = courseStatusView(rows[0]);
  assert.equal(view.needsReview, true);
  assert.equal(view.awaiting, "ADMIN");
  assert.equal(view.action, "REVIEW");
  assert.equal(view.state, "PUBLISHED", "presentation must not restate the Course's lifecycle");
});

test("a draft is discoverable but is never counted as review work", () => {
  const rows = buildDirectory([summary({ id: "c1", lifecycle: "DRAFT" })], []);
  const view = courseStatusView(rows[0]);

  assert.equal(view.needsReview, false);
  assert.equal(view.awaiting, "INSTRUCTOR", "an Admin configures nothing before submission");
  assert.equal(matchesFilter(rows[0], "NEEDS_REVIEW"), false);
  assert.equal(matchesFilter(rows[0], "DRAFT"), true);
  assert.equal(matchesFilter(rows[0], "ALL"), true);
});

test("filter counts describe the combined set, including unlisted pending decisions", () => {
  const counts = filterCounts(
    buildDirectory(
      [summary({ id: "d1", lifecycle: "DRAFT" }), summary({ id: "p1", lifecycle: "PUBLISHED" })],
      [queueItem({ course_id: "stranded" })],
    ),
  );

  assert.equal(counts.NEEDS_REVIEW, 1);
  assert.equal(counts.DRAFT, 1);
  assert.equal(counts.PUBLISHED, 1);
  assert.equal(counts.ALL, 3);
});

test("suspension and retirement are reported as their own facts", () => {
  const rows = buildDirectory(
    [
      summary({
        id: "c1",
        lifecycle: "PUBLISHED",
        access_suspended_at: "2026-08-26T00:00:00Z",
        retired_at: "2026-08-26T00:00:00Z",
      }),
    ],
    [],
  );
  const view = courseStatusView(rows[0]);

  assert.equal(view.accessSuspended, true);
  assert.equal(view.retired, true);
  assert.equal(matchesFilter(rows[0], "WITHDRAWN"), true, "a retired Course is withdrawn");
});
