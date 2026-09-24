import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";

import { hasLessonPreviews } from "./course-detail-presentation";
import type { PublicCourseDetail } from "@/lib/api/public-catalog";

/**
 * Guards over the anonymous Lesson preview surfaces.
 *
 * These are the regressions that would not fail a build and would not be obvious in review: a
 * storage URL rendered where an application route belongs, a preview requested on render rather
 * than on activation, an error message that tells a visitor which authorization link failed, or the
 * legacy course hero rendered for a Course whose preview is now a set of free lessons.
 */

function frontendRoot(): string {
  return process.cwd().endsWith("/frontend") ? process.cwd() : path.join(process.cwd(), "frontend");
}

function readSource(relativePath: string): string {
  const full = path.join(frontendRoot(), relativePath);
  assert.ok(fs.existsSync(full), `${relativePath} is missing; this detector would pass vacuously`);
  return fs.readFileSync(full, "utf8");
}

function withoutComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/\/\/[^\n]*/g, " ");
}

function course(sections: PublicCourseDetail["sections"]): PublicCourseDetail {
  return {
    id: "c1",
    slug: "c1",
    title: "Course",
    instructor_display_name: "Instructor",
    has_preview: true,
    description: "",
    sections,
  };
}

test("hasLessonPreviews reads the projection rather than the derived has_preview flag", () => {
  // has_preview is true in all three, because it is derived from BOTH sources. Only the projection
  // can tell the two models apart, which is the whole reason this helper exists.
  assert.equal(hasLessonPreviews(course([])), false);
  assert.equal(
    hasLessonPreviews(course([{ title: "S", position: 0, lesson_count: 3 }])),
    false,
    "a section with lessons but none previewable must not read as a Lesson-preview Course",
  );
  assert.equal(
    hasLessonPreviews(
      course([
        { title: "S", position: 0, lesson_count: 3 },
        {
          title: "T",
          position: 1,
          lesson_count: 2,
          lessons: [{ id: "l1", title: "Free", position: 0 }],
        },
      ]),
    ),
    true,
  );
});

test("the public page shows the legacy hero only when there are no free lessons", () => {
  const detail = readSource("src/components/catalog/course-detail.tsx");
  // Rendering CoursePreview on has_preview alone would mint a legacy request for a Course whose
  // preview is now a set of free lessons, and that request is guaranteed to fail.
  assert.match(detail, /state\.course\.has_preview && !hasLessonPreviews\(state\.course\)/);
});

test("the lesson preview requests nothing until a visitor activates it", () => {
  const preview = readSource("src/components/catalog/lesson-preview.tsx");
  // No effect hook at all: a visitor who scrolls past a course with six free lessons mints no
  // capability. The request belongs to the click handler.
  assert.ok(
    !/useEffect/.test(preview),
    "lesson-preview must not request an authorization on render",
  );
  assert.match(preview, /onClick=\{open\}/);
  assert.match(preview, /await getLessonPreview\(/);
});

test("the lesson preview plays an application manifest route, never a storage URL", () => {
  const preview = readSource("src/components/catalog/lesson-preview.tsx");
  assert.match(preview, /authorization\.manifest_url/);
  // The anonymous path must never reach the original uploaded object or a presigned URL.
  assert.ok(!/\burl\b\s*:/.test(preview) || !/https?:\/\//.test(preview));
  assert.ok(!/\.url\b/.test(preview), "lesson-preview must not read a signed object URL");
});

test("a refused preview says one thing, whatever the server refused", () => {
  const preview = readSource("src/components/catalog/lesson-preview.tsx");
  // The server deliberately does not say which link of the chain failed. Branching on the problem
  // code here would reconstruct exactly what it withheld.
  //
  // Comments are stripped first: prose explaining the rule is not a violation of it.
  const code = withoutComments(preview);
  assert.match(code, /\} catch \{/);
  assert.ok(
    !/ProblemError|\bcode\b\s*===|\bstatus\b\s*===/.test(code),
    "lesson-preview must not distinguish refusal reasons",
  );
  assert.match(code, /copy\.lessonPreviewFailed/);
});

test("both players share one HLS attachment and teardown", () => {
  const shared = readSource("src/components/media/protected-hls-player.tsx");
  const review = readSource("src/components/admin/review-lesson-preview.tsx");
  const preview = readSource("src/components/catalog/lesson-preview.tsx");
  // Only the shared player may construct Hls. Duplicating the fatal-error and teardown behaviour is
  // how a dead instance stays attached leaking a worker and a network loop on one surface only.
  assert.match(shared, /new Hls\(\)/);
  assert.match(shared, /hls\?\.destroy\(\)/);
  assert.match(shared, /video\.removeAttribute\("src"\)/);
  for (const [name, source] of [
    ["review-lesson-preview", review],
    ["lesson-preview", preview],
  ] as const) {
    assert.ok(!/new Hls\(/.test(source), `${name} must not construct its own Hls instance`);
    assert.match(source, /ProtectedHLSPlayer/);
  }
});

test("the free-lesson controls carry their lesson in the accessible name", () => {
  const preview = readSource("src/components/catalog/lesson-preview.tsx");
  // Several previews can sit on one page, so "Play this lesson" alone would give a screen-reader
  // user a list of identical controls.
  assert.match(preview, /aria-label=\{`\$\{copy\.lessonPreviewPlay\}: \$\{lesson\.title\}`\}/);
  // Closing returns focus to the control that opened the player rather than dropping the reader at
  // the top of the document.
  assert.match(preview, /triggerRef\.current\?\.focus\(\)/);
});

test("the outline still reports the whole section count, not just its free lessons", () => {
  const curriculum = readSource("src/components/catalog/course-curriculum.tsx");
  // lesson_count is the count of every lesson in the section. Swapping it for the previewable list
  // length would tell a visitor the course is far smaller than it is.
  assert.match(curriculum, /\{section\.lesson_count\} \{lessonsUnit\}/);
  assert.match(curriculum, /section\.lessons && section\.lessons\.length > 0/);
  assert.match(curriculum, /copy\.lessonPreviewBadge/);
});
