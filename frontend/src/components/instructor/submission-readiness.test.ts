import assert from "node:assert/strict";
import test from "node:test";

import type { OwnedCourseSummary } from "@/lib/api/catalog";
import { describeSubmissionRejection, submissionReadiness } from "./submission-readiness";

const position = (kind: "section" | "lesson", index: number) =>
  `${kind === "section" ? "Section" : "Lesson"} ${index}`;

const lesson = (id: string, title: string, video?: string) => ({
  id,
  title_ar: `${title} بالعربية`,
  title_en: title,
  position: 1,
  ...(video ? { video_asset_version_id: video } : {}),
});

const section = (id: string, title: string, lessons: ReturnType<typeof lesson>[]) => ({
  id,
  title_ar: `${title} بالعربية`,
  title_en: title,
  position: 1,
  lessons,
});

function academicCourse(overrides: Partial<OwnedCourseSummary> = {}): OwnedCourseSummary {
  return {
    id: "course-1",
    classification_model: "ACADEMIC_CATALOG",
    institution_id: "institution-1",
    subject_id: "subject-1",
    editable_revision: {
      id: "revision-1",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      sections: [section("s1", "Foundations", [lesson("l1", "Opening", "asset-1")])],
    },
    ...overrides,
  };
}

function keyed(course: OwnedCourseSummary) {
  const result = submissionReadiness(course, "en", position);
  return Object.fromEntries(result.requirements.map((r) => [r.key, r]));
}

test("a complete academic course is ready on every requirement the client can check", () => {
  const result = submissionReadiness(academicCourse(), "en", position);
  assert.equal(result.ready, true);
  assert.equal(result.metCount, result.totalCount);
});

test("an academic course is never asked for the legacy vocabulary", () => {
  // The server validates the two models by their own rules and never by both. A course held to
  // legacy requirements it must not populate would be unsubmittable by following the checklist.
  const requirements = keyed(academicCourse());
  assert.ok(requirements.ACADEMIC_INSTITUTION);
  assert.ok(requirements.ACADEMIC_SUBJECT);
  assert.equal(requirements.LEGACY_MAJOR, undefined);
  assert.equal(requirements.LEGACY_SUBJECT, undefined);
  assert.equal(requirements.LEGACY_STUDY_YEAR, undefined);
});

test("a legacy course keeps FR-010 and is never asked for a canonical subject", () => {
  const legacy: OwnedCourseSummary = {
    id: "course-2",
    classification_model: "LEGACY_TAXONOMY",
    editable_revision: {
      id: "revision-2",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      major_term_id: "major-1",
      subject_term_id: "subject-term-1",
      study_year: "YEAR_1",
      sections: [section("s1", "Foundations", [lesson("l1", "Opening", "asset-1")])],
    },
  };
  const requirements = keyed(legacy);
  assert.equal(requirements.LEGACY_MAJOR.met, true);
  assert.equal(requirements.LEGACY_SUBJECT.met, true);
  assert.equal(requirements.LEGACY_STUDY_YEAR.met, true);
  assert.equal(requirements.ACADEMIC_SUBJECT, undefined);
  assert.equal(submissionReadiness(legacy, "en", position).ready, true);
});

test("a missing university and subject are reported before the button is pressed", () => {
  const requirements = keyed(
    academicCourse({ institution_id: undefined, subject_id: undefined }),
  );
  assert.equal(requirements.ACADEMIC_INSTITUTION.met, false);
  assert.equal(requirements.ACADEMIC_SUBJECT.met, false);
});

test("the launch price is not a submission requirement", () => {
  // SubmitCourse never reads a price. An instructor who waits for one waits forever.
  const unpriced = academicCourse({ price_minor_units: null });
  assert.equal(submissionReadiness(unpriced, "en", position).ready, true);
  const keys = submissionReadiness(unpriced, "en", position).requirements.map((r) => r.key);
  assert.ok(!keys.some((key) => /PRICE/i.test(key)));
});

test("a course with no sections reports that, and does not also report empty sections", () => {
  const empty = academicCourse({
    editable_revision: {
      id: "revision-1",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      sections: [],
    },
  });
  const requirements = keyed(empty);
  assert.equal(requirements.SECTIONS.met, false);
  assert.deepEqual(requirements.SECTION_LESSONS.offenders, []);
});

test("empty sections are named by the title the instructor wrote, not by their id", () => {
  const course = academicCourse({
    editable_revision: {
      id: "revision-1",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      sections: [
        section("s1", "Foundations", [lesson("l1", "Opening", "asset-1")]),
        section("s2", "Kinetics", []),
      ],
    },
  });
  const requirements = keyed(course);
  assert.equal(requirements.SECTION_LESSONS.met, false);
  assert.deepEqual(requirements.SECTION_LESSONS.offenders, ["Kinetics"]);
  assert.ok(!requirements.SECTION_LESSONS.offenders.some((name) => name.includes("s2")));
});

test("lessons without a video are named, in the reader's language", () => {
  const course = academicCourse({
    editable_revision: {
      id: "revision-1",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      sections: [
        section("s1", "Foundations", [lesson("l1", "Opening", "asset-1"), lesson("l2", "Bonding")]),
      ],
    },
  });
  assert.deepEqual(keyed(course).LESSON_VIDEOS.offenders, ["Bonding"]);
  const arabic = submissionReadiness(course, "ar", position).requirements.find(
    (r) => r.key === "LESSON_VIDEOS",
  );
  assert.deepEqual(arabic?.offenders, ["Bonding بالعربية"]);
});

test("an untitled item falls back to its position rather than to an identifier", () => {
  const course = academicCourse({
    editable_revision: {
      id: "revision-1",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      sections: [section("s1", "", [])],
    },
  });
  assert.deepEqual(keyed(course).SECTION_LESSONS.offenders, ["Section 1"]);
});

test("a section full of lessons that have no video fails only the video requirement", () => {
  const course = academicCourse({
    editable_revision: {
      id: "revision-1",
      state: "DRAFT",
      title_ar: "دورة",
      title_en: "Course",
      sections: [section("s1", "Foundations", [lesson("l1", "Opening")])],
    },
  });
  const requirements = keyed(course);
  assert.equal(requirements.SECTIONS.met, true);
  assert.equal(requirements.SECTION_LESSONS.met, true);
  assert.equal(requirements.LESSON_VIDEOS.met, false);
});

/* ------------------------------------------------------------------ rejections */

const problem = (codes: string[]) => ({
  problem: { violations: codes.map((code) => ({ code, target: `lesson:${code}-uuid` })) },
});

test("the server's violation codes become sentences, and its targets are dropped", () => {
  const rejection = describeSubmissionRejection(
    problem(["LESSON_VIDEO_MISSING"]),
    (code) => (code === "LESSON_VIDEO_MISSING" ? "Every lesson needs a video." : undefined),
  );
  assert.deepEqual(rejection?.reasons, ["Every lesson needs a video."]);
  assert.equal(rejection?.hasUntranslated, false);
  // The target carried a `kind:uuid` string; nothing of it survives into the reader's copy.
  assert.ok(!rejection!.reasons.join(" ").includes("uuid"));
});

test("many objects failing one requirement is one thing to read", () => {
  const rejection = describeSubmissionRejection(
    problem(["LESSON_VIDEO_MISSING", "LESSON_VIDEO_MISSING", "LESSON_VIDEO_MISSING"]),
    () => "Every lesson needs a video.",
  );
  assert.equal(rejection?.reasons.length, 1);
});

test("distinct requirements stay distinct", () => {
  const rejection = describeSubmissionRejection(
    problem(["SECTION_EMPTY", "LESSON_VIDEO_MISSING"]),
    (code) => (code === "SECTION_EMPTY" ? "Sections need lessons." : "Lessons need videos."),
  );
  assert.deepEqual(rejection?.reasons, ["Sections need lessons.", "Lessons need videos."]);
});

test("an unrecognised code is flagged rather than swallowed", () => {
  // A new server rule must not leave the Instructor facing a refusal with no reason at all.
  const rejection = describeSubmissionRejection(problem(["SOMETHING_NEW"]), () => undefined);
  assert.equal(rejection?.hasUntranslated, true);
  assert.deepEqual(rejection?.reasons, []);
});

test("field errors are read as well as submission violations", () => {
  const rejection = describeSubmissionRejection(
    { problem: { errors: [{ code: "ACADEMIC_SUBJECT_MISSING" }] } },
    () => "This course needs a subject.",
  );
  assert.deepEqual(rejection?.reasons, ["This course needs a subject."]);
});

test("a failure that carries no violations is not a submission rejection", () => {
  assert.equal(describeSubmissionRejection(new Error("network down"), () => "x"), null);
  assert.equal(describeSubmissionRejection({ problem: { title: "Conflict" } }, () => "x"), null);
});
