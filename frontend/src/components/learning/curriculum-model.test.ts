import assert from "node:assert/strict";
import { test } from "node:test";

import {
  courseCurriculum,
  courseIsComplete,
  curriculumFullyOpenLimit,
  initiallyOpenSections,
  lessonState,
} from "./curriculum-model";
import type { CourseHomeSection } from "../../lib/api/learning";

/**
 * The Student's view of a Course's contents.
 *
 * These assertions exist because every figure on the learning screens is supposed to be the
 * server's. The model is the one place that could quietly start having opinions — deriving a course
 * percentage of its own, or calling a Lesson "started" because it was opened — so it is the place
 * the rules are pinned down.
 */

function material(path: string) {
  return { title: "Notes", file_type: "PDF", size_bytes: 10, download_authorization_path: path };
}

function lesson(
  id: string,
  title: string,
  progress: { position_seconds: number; completed: boolean },
  resources = 0,
  labs = 0,
) {
  return {
    lesson_id: id,
    title,
    progress,
    resources: Array.from({ length: resources }, (_, i) => material(`/r/${id}/${i}`)),
    lab_materials: Array.from({ length: labs }, (_, i) => material(`/l/${id}/${i}`)),
  };
}

const SECTIONS: CourseHomeSection[] = [
  {
    section_id: "s1",
    title: "Section 1",
    lessons: [
      lesson("l1", "Lesson 1", { position_seconds: 30, completed: true }, 1, 1),
      lesson("l2", "Lesson 2", { position_seconds: 5, completed: false }),
    ],
  },
  {
    section_id: "s2",
    title: "Section 2",
    lessons: [lesson("l3", "Lesson 3", { position_seconds: 0, completed: false })],
  },
];

test("completion is the server's flag and nothing else", () => {
  assert.equal(lessonState({ position_seconds: 0, completed: true }), "completed");
  // A completed Lesson stays completed regardless of where the position sits.
  assert.equal(lessonState({ position_seconds: 900, completed: true }), "completed");
});

test("a Lesson counts as started only when the server has persisted a position", () => {
  assert.equal(lessonState({ position_seconds: 0, completed: false }), "not-started");
  assert.equal(lessonState({ position_seconds: 0.5, completed: false }), "in-progress");
});

test("the curriculum preserves the server's section and lesson order exactly", () => {
  const curriculum = courseCurriculum(SECTIONS);
  assert.deepEqual(
    curriculum.map((section) => section.sectionID),
    ["s1", "s2"],
  );
  assert.deepEqual(
    curriculum[0].lessons.map((entry) => entry.lessonID),
    ["l1", "l2"],
  );
});

test("section counts are arithmetic over the server's completion flags", () => {
  const [first, second] = courseCurriculum(SECTIONS);
  assert.deepEqual(
    { completed: first.completedLessons, total: first.totalLessons },
    { completed: 1, total: 2 },
  );
  assert.deepEqual(
    { completed: second.completedLessons, total: second.totalLessons },
    { completed: 0, total: 1 },
  );
});

test("a curriculum row counts materials without carrying them", () => {
  const [first] = courseCurriculum(SECTIONS);
  assert.equal(first.lessons[0].materialCount, 2);
  assert.equal(first.lessons[1].materialCount, 0);
  // The narrowed row is exactly four fields: nothing here can carry an authorization path.
  assert.deepEqual(Object.keys(first.lessons[0]).sort(), [
    "lessonID",
    "materialCount",
    "state",
    "title",
  ]);
});

test("the model publishes no course percentage of its own", () => {
  const curriculum = courseCurriculum(SECTIONS);
  for (const section of curriculum) {
    assert.equal("percent" in section, false);
  }
});

test("a short Course opens whole", () => {
  const curriculum = courseCurriculum(SECTIONS);
  assert.deepEqual(initiallyOpenSections(curriculum), ["s1", "s2"]);
});

test("a long Course opens only where the Student is standing", () => {
  const many: CourseHomeSection[] = Array.from({ length: curriculumFullyOpenLimit + 3 }, (_, index) => ({
    section_id: `s${index}`,
    title: `Section ${index}`,
    lessons: [lesson(`l${index}`, `Lesson ${index}`, { position_seconds: 0, completed: false })],
  }));
  const curriculum = courseCurriculum(many);
  assert.deepEqual(initiallyOpenSections(curriculum, "l7"), ["s7"]);
  // With nowhere in particular to be, the first section is the honest default.
  assert.deepEqual(initiallyOpenSections(curriculum), ["s0"]);
  // A pointer to a Lesson this Course does not contain must not open nothing at all.
  assert.deepEqual(initiallyOpenSections(curriculum, "absent"), ["s0"]);
});

test("no sections means no open sections, rather than a crash", () => {
  assert.deepEqual(initiallyOpenSections([]), []);
  assert.deepEqual(courseCurriculum([]), []);
});

test("course completion is the server's own counts, and an empty Course is not complete", () => {
  assert.equal(courseIsComplete({ completed_lessons: 3, total_lessons: 3 }), true);
  assert.equal(courseIsComplete({ completed_lessons: 2, total_lessons: 3 }), false);
  assert.equal(courseIsComplete({ completed_lessons: 0, total_lessons: 0 }), false);
});
