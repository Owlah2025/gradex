import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";

import { ar } from "../../lib/i18n/dictionaries/ar";
import { en } from "../../lib/i18n/dictionaries/en";

/**
 * UX-F — what the Student surfaces are not allowed to become.
 *
 * These are source assertions on the shipped screens, in the same style as the S5 payload contract,
 * and they guard the decisions that a later edit could undo silently: an invented progress figure,
 * a completion control the server does not honour, a raw enum on its way to a Student, or a stock
 * palette creeping back into a surface that has semantic tokens.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function shipped(relativePath: string): string {
  return fs.readFileSync(path.join(frontendRoot(), relativePath), "utf8");
}

/**
 * The source with its comments removed.
 *
 * Several of these assertions are about what a screen *does*, and the modules deliberately explain
 * in prose why they do not do it — `learning-views.tsx` says outright that it exists to keep
 * `report_context` out of the payload. Matching that sentence would make the documentation of a
 * rule the evidence for breaking it.
 */
function code(relativePath: string): string {
  return shipped(relativePath)
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^\s*\/\/.*$/gm, "");
}

const DASHBOARD = "src/app/[locale]/learn/dashboard/page.tsx";
const COURSE_HOME = "src/app/[locale]/learn/courses/[courseId]/page.tsx";
const LESSON = "src/app/[locale]/learn/courses/[courseId]/lessons/[lessonId]/page.tsx";
const VIEWS = "src/components/learning/learning-views.tsx";
const CURRICULUM = "src/components/learning/course-curriculum.tsx";
const SHELL = "src/components/learning/learning-shell.tsx";
const PANEL = "src/components/learning/lesson-curriculum-panel.tsx";
const PLAYER = "src/components/learning/lesson-player.tsx";

const STUDENT_SOURCES = [DASHBOARD, COURSE_HOME, LESSON, VIEWS, CURRICULUM, SHELL, PANEL, PLAYER];

// --- Progress and completion are the server's -----------------------------

test("no Student surface computes a progress percentage of its own", () => {
  for (const file of STUDENT_SOURCES) {
    const source = shipped(file);
    // The only percentage on these screens is `progress.percent`, formatted. Any arithmetic
    // producing one — completed/total * 100 — would be a second opinion about the same fact.
    assert.equal(
      /completed_lessons\s*\/\s*|\/\s*total_lessons|\*\s*100/.test(source),
      false,
      `${file} derives a progress percentage instead of rendering the server's`,
    );
  }
});

test("completion is never claimed by a control the product does not have", () => {
  // Completion is reached by watching; the server writes it and never unwrites it. A button that
  // said otherwise would report a state the server has not recorded.
  for (const file of STUDENT_SOURCES) {
    const source = shipped(file).toLowerCase();
    assert.equal(
      /markcomplete|mark_complete|mark complete|markascomplete/.test(source),
      false,
      `${file} offers a completion control the contract does not support`,
    );
  }
  const dictionaries = JSON.stringify({ en: en.learning, ar: ar.learning }).toLowerCase();
  assert.equal(/mark complete|mark as complete/.test(dictionaries), false);
});

test("the Student surfaces invent no metric the contract cannot supply", () => {
  const copy = JSON.stringify({ en: en.learning, ar: en.learning }).toLowerCase();
  for (const invented of ["streak", "certificate", "badge", "time remaining", "minutes left"]) {
    assert.equal(copy.includes(invented), false, `learning copy promises "${invented}"`);
  }
});

// --- Nothing technical reaches the Student --------------------------------

test("no Student surface renders an identifier as reading matter", () => {
  for (const file of STUDENT_SOURCES) {
    const source = shipped(file);
    // Identifiers build links and keys. They are never the content of a rendered element, which is
    // what `>{...id}<` would be.
    assert.equal(
      /\>\{\s*[a-zA-Z.]*(course_id|lesson_id|section_id|asset_version_id|entitlement_id)\s*\}\</.test(
        source,
      ),
      false,
      `${file} renders an identifier as text`,
    );
  }

  // The player is the one component that legitimately holds a playback authorization — it must, to
  // load the media at all. Everywhere else the fields simply have no business existing.
  for (const file of STUDENT_SOURCES.filter((entry) => entry !== PLAYER)) {
    assert.equal(
      /(asset_version_id|report_context|manifest_url|playback_session)/.test(code(file)),
      false,
      `${file} handles a protected media field`,
    );
  }

  // And the player, which does hold one, never puts any part of it on the screen.
  const player = code(PLAYER);
  assert.equal(
    /\{\s*playback\.(manifest_url|asset_version_id|playback_session|expires_at)\s*\}/.test(player),
    false,
    "the player renders part of its playback authorization",
  );
});

test("no raw server enumeration is used as Student copy", () => {
  // Wire values the learning contract actually carries. `learning_status` is compared against them
  // to choose copy; none of them may ever be the copy.
  for (const file of STUDENT_SOURCES) {
    const source = shipped(file);
    assert.equal(
      /\>\s*\{?\s*["'](active|expired|ACTIVE|ACCESS_ENDED|AWAITING_APPROVAL|ENTITLEMENT_NOT_FOUND|ASSET_VERSION_UNAVAILABLE)["']\s*\}?\s*\</.test(
        source,
      ),
      false,
      `${file} renders a wire value as Student copy`,
    );
  }
});

// --- The design system, not a stock palette -------------------------------

test("the touched Student surfaces carry no stock Tailwind palette", () => {
  const stock =
    /\b(?:bg|text|border|ring|from|to|via)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b/;
  for (const file of STUDENT_SOURCES) {
    const source = shipped(file);
    const offender = source.match(stock);
    assert.equal(offender, null, `${file} uses the stock palette class ${offender?.[0]}`);
  }
});

test("every Student screen sits in the learning frame and sets its own direction", () => {
  for (const file of [DASHBOARD, COURSE_HOME, LESSON]) {
    const source = shipped(file);
    assert.ok(source.includes("<LearningShell"), `${file} does not use the learning frame`);
    assert.ok(
      source.includes('dir={locale === "ar" ? "rtl" : "ltr"}'),
      `${file} does not set the reading direction from the route locale`,
    );
    // Both the loaded and the failed render belong in the frame, or a failed read drops the
    // Student out of the product entirely.
    assert.ok(
      (source.match(/<LearningShell/g) ?? []).length >= 2,
      `${file} leaves its failure state outside the learning frame`,
    );
  }
});

// --- Both languages say the same things -----------------------------------

test("every new Student string exists in both languages and is real copy", () => {
  const added = [
    "myCourses",
    "learningNavigation",
    "courseContents",
    "closeCourseContents",
    "backToCourse",
    "currentLessonLabel",
    "lessonNotStarted",
    "lessonInProgress",
    "files",
    "activeDetail",
    "expiredDetail",
    "completionAutomatic",
    "courseCompleteTitle",
    "courseCompleteBody",
    "loadingCourses",
    "loadingCourse",
    "loadingLesson",
    "emptyAction",
  ] as const;
  for (const key of added) {
    assert.equal(typeof en.learning[key], "string", `English learning.${key}`);
    assert.equal(typeof ar.learning[key], "string", `Arabic learning.${key}`);
    assert.notEqual(en.learning[key].trim(), "", `English learning.${key} is empty`);
    assert.notEqual(ar.learning[key].trim(), "", `Arabic learning.${key} is empty`);
    // Arabic that is byte-identical to English is untranslated copy, not a translation.
    assert.notEqual(ar.learning[key], en.learning[key], `learning.${key} was never translated`);
  }
});

test("the access states are described, not named", () => {
  // The badge says what the state is called; the sentence beside it says what it means. Both halves
  // are required, in both languages, or the state is carried by tone alone.
  for (const dictionary of [en, ar]) {
    assert.ok(dictionary.learning.activeDetail.length > dictionary.learning.active.length);
    assert.ok(dictionary.learning.expiredDetail.length > dictionary.learning.expired.length);
  }
});
