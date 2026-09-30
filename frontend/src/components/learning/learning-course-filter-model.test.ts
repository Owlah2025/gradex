import assert from "node:assert/strict";
import { test } from "node:test";
import {
  initialLearningCourseFilter,
  matchesLearningCourseFilter,
  type LearningCourseFilterCard,
} from "./learning-course-filter-model";

function card(overrides: Partial<LearningCourseFilterCard> = {}): LearningCourseFilterCard {
  return { completed: false, learningStatus: "active", ...overrides };
}

test("the course filter opens on active unfinished learning when available", () => {
  assert.equal(
    initialLearningCourseFilter([card({ learningStatus: "expired" }), card()]),
    "in-progress",
  );
});

test("the course filter opens on completed when no active course remains unfinished", () => {
  assert.equal(initialLearningCourseFilter([card({ completed: true })]), "completed");
});

test("the course filter opens on all when only expired unfinished courses remain", () => {
  assert.equal(initialLearningCourseFilter([card({ learningStatus: "expired" })]), "all");
});

test("expired unfinished courses are excluded from in-progress but retained in all", () => {
  const expired = card({ learningStatus: "expired" });
  assert.equal(matchesLearningCourseFilter(expired, "in-progress"), false);
  assert.equal(matchesLearningCourseFilter(expired, "all"), true);
});

test("completed courses remain in the completed view regardless of access state", () => {
  const completed = card({ completed: true, learningStatus: "expired" });
  assert.equal(matchesLearningCourseFilter(completed, "completed"), true);
  assert.equal(matchesLearningCourseFilter(completed, "in-progress"), false);
});
