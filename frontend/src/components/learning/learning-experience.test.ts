import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import {
  courseCurriculum,
  courseIsStarted,
  resumeLessonID,
} from "./curriculum-model";
import type { CourseHomeSection } from "../../lib/api/learning";

/**
 * The Course Learning Experience: where a Student lands, and what the screen is allowed to claim.
 *
 * Two kinds of assertion live here, and the split is deliberate.
 *
 * The first is behaviour that is pure arithmetic over the server's own read model — which Lesson a
 * re-entering Student is offered — and is asserted by calling the function.
 *
 * The second is structural, and is asserted against the source text: that the resource control is a
 * sibling of the Lesson link rather than nested inside it, and that the tab set names no feature
 * this product does not have. Neither is checkable without a DOM, and both are exactly the kind of
 * regression that reintroduces itself during a refactor — a `<button>` slipping back inside an
 * `<a>` navigates on every click, and a plausible-looking "Q&A" tab promises a backend that does
 * not exist. The repository already asserts source structure this way (see
 * `progress-confirmation.test.ts`), so this follows that existing convention rather than pulling a
 * renderer into the unit suite.
 */

function material(path: string) {
  return { title: "Notes", file_type: "PDF", size_bytes: 10, download_authorization_path: path };
}

function lesson(
  id: string,
  progress: { position_seconds: number; completed: boolean },
  resources = 0,
) {
  return {
    lesson_id: id,
    title: id.toUpperCase(),
    progress,
    resources: Array.from({ length: resources }, (_, i) => material(`/r/${id}/${i}`)),
    lab_materials: [],
  };
}

function sections(...groups: Array<ReturnType<typeof lesson>[]>): CourseHomeSection[] {
  return groups.map((lessons, index) => ({
    section_id: `s${index + 1}`,
    title: `Section ${index + 1}`,
    lessons,
  }));
}

const done = { position_seconds: 90, completed: true };
const partway = { position_seconds: 12, completed: false };
const untouched = { position_seconds: 0, completed: false };

test("a returning Student is offered the Lesson they were part-way through", () => {
  const model = courseCurriculum(
    sections([lesson("l1", done), lesson("l2", partway)], [lesson("l3", untouched)]),
  );
  assert.equal(resumeLessonID(model), "l2");
});

test("with nothing started, the first unfinished Lesson is offered — across a section boundary", () => {
  const model = courseCurriculum(sections([lesson("l1", done)], [lesson("l2", untouched)]));
  // The answer comes from the server's section order, not from the array of lessons in section 1.
  assert.equal(resumeLessonID(model), "l2");
});

test("a finished Course reopens at its beginning rather than past its end", () => {
  const model = courseCurriculum(sections([lesson("l1", done), lesson("l2", done)]));
  assert.equal(resumeLessonID(model), "l1");
});

test("a Course with no Lessons offers nothing rather than an invented identifier", () => {
  assert.equal(resumeLessonID([]), null);
  assert.equal(resumeLessonID(courseCurriculum(sections([]))), null);
});

test("started is the server's flags, so an untouched Course reads as not started", () => {
  assert.equal(courseIsStarted(courseCurriculum(sections([lesson("l1", untouched)]))), false);
  assert.equal(courseIsStarted(courseCurriculum(sections([lesson("l1", partway)]))), true);
  assert.equal(courseIsStarted(courseCurriculum(sections([lesson("l1", done)]))), true);
});

function source(file: string): string {
  const root = process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
  return fs.readFileSync(path.join(root, "src/components/learning", file), "utf8");
}

test("the resource control is a sibling of the Lesson link, never nested inside it", () => {
  const curriculum = source("course-curriculum.tsx");
  const link = curriculum.slice(curriculum.indexOf("<Link"), curriculum.indexOf("</Link>"));
  // Everything the row's resource control is made of must be outside the anchor. A control inside
  // it would navigate to the Lesson on every activation, which is the whole defect.
  assert.ok(!link.includes("{resources}"), "the resource subtree must not be inside the Lesson link");
  assert.ok(
    curriculum.indexOf("{resources}") > curriculum.indexOf("</Link>"),
    "the resource subtree must be placed after the Lesson link closes",
  );
});

test("the Lesson row still publishes the identity and state the progress suites read", () => {
  const curriculum = source("course-curriculum.tsx");
  assert.match(curriculum, /data-lesson-id=\{lesson\.lessonID\}/);
  assert.match(curriculum, /data-lesson-state=\{lesson\.state\}/);
});

test("the tab set names no feature this product does not have", () => {
  const page = fs.readFileSync(
    path.join(
      process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend"),
      "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx",
    ),
    "utf8",
  );
  for (const absent of ['value: "qa"', 'value: "notes"', 'value: "announcements"', 'value: "reviews"']) {
    assert.ok(!page.includes(absent), `${absent} names a feature Gradex does not implement`);
  }
  for (const present of ['value: "overview"', 'value: "resources"', 'value: "report"']) {
    assert.ok(page.includes(present), `${present} is missing from the Lesson tabs`);
  }
});

test("previous/next is rendered under the player and never over it", () => {
  const page = fs.readFileSync(
    path.join(
      process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend"),
      "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx",
    ),
    "utf8",
  );
  assert.ok(
    page.indexOf("<LessonPlayer") < page.indexOf("<LessonNavigation"),
    "navigation must follow the player in the markup",
  );
  const navigation = source("learning-views.tsx");
  const block = navigation.slice(navigation.indexOf("export function LessonNavigation"));
  assert.ok(!/absolute|fixed|inset-0/.test(block.slice(0, 2500)), "navigation must not be positioned over the player");
});

test("navigation uses the server's pointers rather than recomputing an order", () => {
  const navigation = source("learning-views.tsx");
  const block = navigation.slice(navigation.indexOf("export function LessonNavigation"));
  assert.match(block, /navigation\.previous_lesson_id/);
  assert.match(block, /navigation\.next_lesson_id/);
  assert.ok(!/\.sort\(|\.indexOf\(|findIndex/.test(block.slice(0, 3000)), "order must not be derived on the client");
});
