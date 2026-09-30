import assert from "node:assert/strict";
import test from "node:test";

import { canManageCourseAnnouncements } from "./dashboard-course-actions";

const course = (published: boolean) => ({
  course_id: "course-1",
  title_ar: "مقرر",
  title_en: "Course",
  lifecycle: published ? "PUBLISHED" : "DRAFT",
  published_revision: published ? { id: "revision-1", revision_number: 1, state: "APPROVED" } : undefined,
  enrollments: 0,
  learning_active_students_7d: 0,
  average_progress: 0,
  completions: 0,
  media_processing_failures: 0,
});

test("announcement management is available only after a course has served a published revision", () => {
  assert.equal(canManageCourseAnnouncements(course(false)), false);
  assert.equal(canManageCourseAnnouncements(course(true)), true);
});
