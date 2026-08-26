import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  accessLabels,
  learningStatusLabel,
  materialsLabels,
  navigationLabels,
  curriculumLabels,
  progressLabels,
  unavailableLabels,
} from "./learning-label-sets";
import { en } from "../../lib/i18n/dictionaries/en";

/**
 * T7 — Student learning payload contract (D-065, GAP-03, GAP-04).
 *
 * The defect these guard: a component handed more than it renders publishes more than it renders.
 * In development React emits a server-component owner stack carrying every **server** component's
 * props into the page, so the rule `report-labels.ts` already stated for client components applies
 * one level up. Handing `CourseOutline` the whole `CourseHome` put the opaque report context into
 * the markup; handing every view the whole dictionary put `"Active access"` onto expired pages.
 *
 * Behavioural assertions prove the narrowing is real at runtime — a `Pick` type alone would
 * type-check while still carrying every key. Structural assertions hold for the shipped pages, so a
 * later edit cannot quietly widen a prop back.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
}

function shipped(relativePath: string): string {
  return fs.readFileSync(path.join(frontendRoot(), relativePath), "utf8");
}

const COURSE_HOME = "src/app/[locale]/learn/courses/[courseId]/page.tsx";
const LESSON = "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx";
const DASHBOARD = "src/app/[locale]/learn/dashboard/page.tsx";
const VIEWS = "src/components/learning/learning-views.tsx";
const CURRICULUM = "src/components/learning/course-curriculum.tsx";
const CURRICULUM_MODEL = "src/components/learning/curriculum-model.ts";

// --- The narrowing is real at runtime, not merely a type ------------------

test("each label set carries only its own keys", () => {
  assert.deepEqual(Object.keys(progressLabels(en.learning)).sort(), [
    "completedLessons",
    "progress",
  ]);
  assert.deepEqual(Object.keys(accessLabels(en.learning)).sort(), ["accessUntil", "noExpiry"]);
  assert.deepEqual(Object.keys(unavailableLabels(en.learning)).sort(), [
    "unavailableBody",
    "unavailableTitle",
  ]);
  assert.equal(Object.keys(navigationLabels(en.learning)).length, 5);
  assert.equal(Object.keys(materialsLabels(en.learning)).length, 10);
  // The dictionary is far larger than any set built from it; if a set ever approached its size the
  // narrowing would have stopped being narrowing.
  assert.ok(Object.keys(en.learning).length > 40);
  assert.ok(Object.keys(curriculumLabels(en.learning)).length < Object.keys(en.learning).length / 2);
});

test("no label set carries status copy the page does not display", () => {
  for (const set of [
    progressLabels(en.learning),
    accessLabels(en.learning),
    unavailableLabels(en.learning),
    navigationLabels(en.learning),
    materialsLabels(en.learning),
    curriculumLabels(en.learning),
  ]) {
    const serialised = JSON.stringify(set);
    assert.equal(
      serialised.includes(en.learning.active),
      false,
      `a label set carries active-state copy: ${serialised.slice(0, 120)}`,
    );
  }
});

test("the status label is resolved on the server, never offered as a choice", () => {
  assert.equal(learningStatusLabel("expired", en.learning), en.learning.expired);
  assert.equal(learningStatusLabel("active", en.learning), en.learning.active);
  // Resolution returns a string, so an expired render cannot carry the active copy at all.
  assert.equal(typeof learningStatusLabel("expired", en.learning), "string");
});

// --- The shipped pages hand nothing wider than it renders -----------------

test("no learning page hands a component the whole learning dictionary", () => {
  for (const file of [COURSE_HOME, LESSON, DASHBOARD]) {
    const source = shipped(file);
    assert.equal(
      /labels=\{(dictionary\.learning|labels)\}/.test(source),
      false,
      `${file} passes an unnarrowed dictionary as labels`,
    );
  }
});

test("the status badge receives a resolved label, not a catalogue", () => {
  for (const file of [COURSE_HOME, LESSON, DASHBOARD]) {
    const source = shipped(file);
    if (!source.includes("LearningStatusBadge")) continue;
    assert.ok(
      source.includes("label={learningStatusLabel("),
      `${file} does not resolve the status label on the server`,
    );
  }
  const views = shipped(VIEWS);
  assert.ok(
    /export function LearningStatusBadge\(\{\s*status,\s*label,\s*detail,?\s*\}/.test(views),
    "LearningStatusBadge must take resolved strings, never a catalogue to choose from",
  );
  // The sentence beside the pill is resolved on the server for the same reason the pill's own text
  // is: an expired render must not carry the active wording it does not display.
  for (const file of [COURSE_HOME, LESSON, DASHBOARD]) {
    const source = shipped(file);
    if (!source.includes("LearningStatusBadge")) continue;
    assert.ok(
      source.includes("detail={learningStatusDetail("),
      `${file} does not resolve the status detail on the server`,
    );
  }
});

test("the contents and navigation take read-model slices, never the whole model", () => {
  const views = shipped(VIEWS);
  // The whole CourseHome is what carried report_context into the payload (GAP-03).
  assert.equal(
    /export function LessonNavigation\([\s\S]{0,400}lesson: LessonReadModel/.test(views),
    false,
    "LessonNavigation must not accept the whole LessonReadModel",
  );

  // The contents are now a client component, so an oversized prop would be serialised into the
  // page in *every* build mode rather than only in development. It therefore takes the narrowed
  // curriculum, and the narrowing happens on the server before the boundary is crossed.
  const curriculum = shipped(CURRICULUM);
  assert.equal(
    /CourseHome|report_context|LessonReadModel/.test(curriculum),
    false,
    "the contents component must not know the read model at all",
  );

  for (const file of [COURSE_HOME, LESSON]) {
    const source = shipped(file);
    assert.equal(
      /<CourseCurriculum[\s\S]{0,400}sections=\{course\.sections\}/.test(source),
      false,
      `${file} must not hand the raw read-model sections to the contents`,
    );
    assert.ok(
      source.includes("courseCurriculum("),
      `${file} must narrow the sections through courseCurriculum() first`,
    );
  }
});

test("the narrowed curriculum carries no report context and no material paths", () => {
  const model = shipped(CURRICULUM_MODEL);
  assert.equal(/report_context/.test(model), false);
  assert.equal(
    /download_authorization_path/.test(model),
    false,
    "a curriculum row counts materials; it must not carry their authorization paths",
  );
});

test("the Lesson page hands its child no dictionary at all", () => {
  const source = shipped(LESSON);
  // LessonContent resolves its own copy after the fetch, because the status label cannot be
  // resolved before the read model says which status it is.
  assert.equal(
    /<LessonContent[\s\S]{0,300}labels=/.test(source),
    false,
    "LessonContent must not receive a labels prop",
  );
  assert.equal(
    /<LessonContent[\s\S]{0,300}playerLabels=/.test(source),
    false,
    "LessonContent must not receive a playerLabels prop",
  );
});
