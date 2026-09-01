import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { progressConfirmation } from "./progress-contract";
import {
  progressSnapshot,
  publishProgressConfirmation,
  resetProgressStoreForTest,
  subscribeToProgress,
} from "./progress-store";

test("a progress confirmation is read out of the response, not assumed", () => {
  const confirmation = progressConfirmation("lesson-1", {
    lesson_progress: { position_seconds: 327, completed: false },
    course_progress: { completed_lessons: 3, total_lessons: 10, percent: 30 },
  });
  assert.deepEqual(confirmation, {
    lessonID: "lesson-1",
    lesson: { position_seconds: 327, completed: false },
    course: { completed_lessons: 3, total_lessons: 10, percent: 30 },
  });
});

test("a write that returns no aggregate still confirms the Lesson", () => {
  // The confirming read can fail after the write has already committed. Absent
  // must mean "no new aggregate", never "zero" — rendering zero would tell the
  // Student they had lost progress they still have.
  const confirmation = progressConfirmation("lesson-1", {
    lesson_progress: { position_seconds: 12, completed: true },
  });
  assert.equal(confirmation?.course, null);
  assert.equal(confirmation?.lesson.completed, true);
});

test("an unusable response body is no confirmation at all", () => {
  for (const body of [
    null,
    "204",
    {},
    { lesson_progress: null },
    { lesson_progress: { position_seconds: "12", completed: false } },
    { lesson_progress: { position_seconds: Number.NaN, completed: false } },
    { lesson_progress: { position_seconds: 12, completed: "yes" } },
  ]) {
    assert.equal(progressConfirmation("lesson-1", body), null, JSON.stringify(body));
  }
  // A malformed aggregate does not discard a valid Lesson confirmation.
  const partial = progressConfirmation("lesson-1", {
    lesson_progress: { position_seconds: 12, completed: false },
    course_progress: { completed_lessons: 3, total_lessons: "10", percent: 30 },
  });
  assert.equal(partial?.course, null);
  assert.equal(partial?.lesson.position_seconds, 12);
});

test("the store notifies only when something a reader can see has moved", () => {
  resetProgressStoreForTest();
  const confirmation = {
    lessonID: "lesson-1",
    lesson: { position_seconds: 12, completed: false },
    course: { completed_lessons: 0, total_lessons: 4, percent: 0 },
  };
  let notifications = 0;
  const unsubscribe = subscribeToProgress(() => {
    notifications += 1;
  });
  try {
    publishProgressConfirmation(confirmation);
    assert.equal(notifications, 1);
    assert.deepEqual(progressSnapshot().lessons["lesson-1"], confirmation.lesson);

    // Publishing the identical state again must not notify. Subscribers compare
    // snapshots by identity, so replacing the snapshot on every ordinary
    // fifteen-second tick would re-render every progress surface in the page
    // whether or not anything visible had moved.
    const before = progressSnapshot();
    publishProgressConfirmation({ ...confirmation });
    assert.equal(notifications, 1, "an unchanged confirmation notified subscribers");
    assert.equal(progressSnapshot(), before, "an unchanged confirmation replaced the snapshot");

    publishProgressConfirmation({
      ...confirmation,
      lesson: { position_seconds: 54, completed: true },
      course: { completed_lessons: 1, total_lessons: 4, percent: 25 },
    });
    assert.equal(notifications, 2);
    assert.equal(progressSnapshot().course?.percent, 25);
  } finally {
    unsubscribe();
    resetProgressStoreForTest();
  }
});

test("a confirmation without an aggregate keeps the one already known", () => {
  resetProgressStoreForTest();
  publishProgressConfirmation({
    lessonID: "lesson-1",
    lesson: { position_seconds: 12, completed: false },
    course: { completed_lessons: 1, total_lessons: 4, percent: 25 },
  });
  // The confirming read failed on this write. The Course percentage must hold
  // at what the server last said rather than disappearing.
  publishProgressConfirmation({
    lessonID: "lesson-1",
    lesson: { position_seconds: 20, completed: false },
    course: null,
  });
  assert.equal(progressSnapshot().course?.percent, 25);
  assert.equal(progressSnapshot().lessons["lesson-1"].position_seconds, 20);
  resetProgressStoreForTest();
});

/**
 * Source with its comments removed, so prose about what a module deliberately
 * does not do cannot be read as the module doing it.
 */
function executableSource(relative: string): string {
  const root = process.cwd().endsWith("/frontend")
    ? process.cwd()
    : path.join(process.cwd(), "frontend");
  return fs
    .readFileSync(path.join(root, "src", relative), "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/.*$/gm, "$1");
}

test("the visible Lesson state follows the server's confirmation, not a reload", () => {
  const badge = executableSource("components/learning/lesson-progress-state.tsx");
  assert.match(badge, /useConfirmedLessonProgress\(lessonID, initial\)/);
  assert.match(badge, /data-testid="lesson-state"/);
  assert.match(badge, /data-lesson-state=\{state\}/);
  // The state changes without a navigation, so it is announced rather than
  // silently replaced under a screen reader that has already read it.
  assert.match(badge, /aria-live="polite"/);
  // Completion is the server's. Nothing here decides it and there is no
  // control that could claim it.
  assert.ok(!/onClick/.test(badge), "the Lesson state carries a control");
  assert.ok(!/completed:\s*true/.test(badge), "the badge asserts its own completion");
});

test("a confirmation can only add completion, never take it away", () => {
  const store = executableSource("components/learning/use-progress-store.ts");
  // Completion is write-once server-side. A confirmation arriving without it —
  // after a rewind, say — must not un-complete what the page already knew.
  assert.match(store, /completed: live\.completed \|\| initial\.completed/);
  // The server-rendered value is the floor, not a default to be discarded.
  assert.match(store, /if \(!live\) return initial/);
  // Nothing has been confirmed during a server render, and returning the live
  // module value would let one request's progress reach another's HTML.
  assert.match(store, /serverProgressSnapshot/);
});

test("the Course contents follow the same confirmation and keep their counter honest", () => {
  const panel = executableSource("components/learning/lesson-curriculum-panel.tsx");
  assert.match(panel, /useConfirmedLessonProgress\(currentLessonID/);
  assert.match(panel, /withConfirmedLesson\(sections, currentLessonID, lessonState\(live\)\)/);
  // Updating the row without the section counter would leave "2 of 5" beside
  // three ticks, which is worse than the staleness it fixes.
  assert.match(panel, /completedLessons: lessons\.filter\(\(lesson\) => lesson\.state === "completed"\)\.length/);
  // Only the Lesson being watched can have news; the rest of the list stays
  // exactly as the server described it.
  assert.match(panel, /if \(index === -1 \|\| section\.lessons\[index\]\.state === state\) return section/);
});

test("nothing in the progress path reloads the page or refreshes the route", () => {
  for (const relative of [
    "components/learning/progress-reporter.ts",
    "components/learning/progress-reporter-controller.ts",
    "components/learning/progress-store.ts",
    "components/learning/use-progress-store.ts",
    "components/learning/lesson-progress-state.tsx",
    "components/learning/lesson-curriculum-panel.tsx",
  ]) {
    const source = executableSource(relative);
    assert.ok(!/location\.reload/.test(source), `${relative} reloads the page`);
    assert.ok(!/router\.refresh/.test(source), `${relative} refreshes the route`);
  }
});

test("the reporting cadence is unchanged and the end of a Lesson does not wait for it", () => {
  const lifecycle = executableSource("components/learning/progress-reporter-lifecycle.ts");
  assert.match(lifecycle, /setInterval\(report, progressReportIntervalMilliseconds\)/);
  for (const event of ["pause", "seeked", "ended", "visibilitychange", "pagehide"]) {
    assert.ok(lifecycle.includes(`"${event}"`), `the reporter dropped ${event}`);
  }
  // No per-second polling crept in alongside the interval.
  assert.ok(!/1_?000\)/.test(lifecycle), "the reporter schedules a one-second timer");
});
