import type { OwnedCourseSummary } from "@/lib/api/catalog";
// A value import, so it must resolve without the `@/` alias: the node test build emits CommonJS
// with no path mapping, and only `import type` is erased before it gets there.
import { isAcademicCourse } from "../../lib/api/catalog";

/**
 * What the server will actually check when this Course is submitted.
 *
 * Until now the studio's answer to "can I submit?" was a sentence under the button saying the
 * server validates completeness, and then the loop: press Submit, read a rejection, fix one thing,
 * press Submit again. The rejection arrived as `describeApiError` renders it —
 *
 *     Course submission incomplete: LESSON_VIDEO_MISSING · lesson:0f2c…-… | SECTION_EMPTY · section:9b1a…-…
 *
 * — which is a wire enum and a database key per problem, and says neither which lesson nor what to
 * do. An Instructor with a twelve-lesson course discovered their missing videos one press at a
 * time, each round trip naming a lesson by an identifier that appears nowhere on the screen.
 *
 * Every requirement below is read from `catalog/validation.go`, and only the ones a client can
 * honestly evaluate are listed. The two branches are the server's own: an Academic Catalog Course
 * is held to `validateAcademicIdentityForSubmission` and never asked for the legacy vocabulary; a
 * legacy Course keeps FR-010 exactly.
 *
 * Deliberately absent, because the client cannot know them:
 *
 *   ACADEMIC_SUBJECT_RETIRED / _UNAVAILABLE   subject dependency state
 *   ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE      programme-subset validity
 *   ASSET_VERSION_UNAVAILABLE                 whether an uploaded asset still resolves
 *
 * Those remain the server's to refuse, and its refusal is reported when it happens. This is not a
 * second validator — the server stays authoritative and a green checklist is not a promise. It is
 * the difference between finding out before pressing the button and finding out after.
 *
 * There is deliberately no percentage. Requirements are not equally weighted or equally sized, and
 * a course sitting at "80% complete" with no video on any lesson would be further from submittable
 * than the number implies.
 */
export type ReadinessKey =
  | "ACADEMIC_INSTITUTION"
  | "ACADEMIC_SUBJECT"
  | "LEGACY_MAJOR"
  | "LEGACY_SUBJECT"
  | "LEGACY_STUDY_YEAR"
  | "SECTIONS"
  | "SECTION_LESSONS"
  | "LESSON_VIDEOS";

export type ReadinessRequirement = {
  key: ReadinessKey;
  met: boolean;
  /**
   * The sections or lessons that fail this requirement, by title.
   *
   * Titles, never identifiers: naming the thing is the entire point, and the Instructor wrote
   * these words themselves. An untitled item is reported by its position instead.
   */
  offenders: string[];
};

export type SubmissionReadiness = {
  requirements: ReadinessRequirement[];
  /** Every client-checkable requirement is met. The server may still refuse. */
  ready: boolean;
  metCount: number;
  totalCount: number;
};

function titleOf(
  item: { title_ar?: string; title_en?: string },
  locale: "ar" | "en",
  fallback: string,
): string {
  const title = locale === "ar" ? item.title_ar : item.title_en;
  return title?.trim() || fallback;
}

export function submissionReadiness(
  course: OwnedCourseSummary,
  locale: "ar" | "en",
  /** "Section 3" / "Lesson 2" wording for items whose title is still blank. */
  positionLabel: (kind: "section" | "lesson", position: number) => string,
): SubmissionReadiness {
  const revision = course.editable_revision;
  const sections = revision?.sections ?? [];
  const requirements: ReadinessRequirement[] = [];

  if (isAcademicCourse(course)) {
    requirements.push({
      key: "ACADEMIC_INSTITUTION",
      met: Boolean(course.institution_id),
      offenders: [],
    });
    requirements.push({
      key: "ACADEMIC_SUBJECT",
      met: Boolean(course.subject_id),
      offenders: [],
    });
  } else {
    // FR-010, unchanged for every Course that has not been migrated by T5.
    requirements.push({
      key: "LEGACY_MAJOR",
      met: Boolean(revision?.major_term_id),
      offenders: [],
    });
    requirements.push({
      key: "LEGACY_SUBJECT",
      met: Boolean(revision?.subject_term_id),
      offenders: [],
    });
    requirements.push({
      key: "LEGACY_STUDY_YEAR",
      met: Boolean(revision?.study_year),
      offenders: [],
    });
  }

  requirements.push({
    key: "SECTIONS",
    met: sections.length > 0,
    offenders: [],
  });

  // Only meaningful once there is a section; with none, COURSE_EMPTY is the requirement to report
  // and repeating it as "no section has a lesson" would be two rows for one problem.
  const emptySections = sections
    .filter((section) => (section.lessons ?? []).length === 0)
    .map((section, index) => titleOf(section, locale, positionLabel("section", index + 1)));
  requirements.push({
    key: "SECTION_LESSONS",
    met: sections.length > 0 && emptySections.length === 0,
    offenders: emptySections,
  });

  const lessonsWithoutVideo = sections
    .flatMap((section) => section.lessons ?? [])
    .filter((lesson) => !lesson.video_asset_version_id)
    .map((lesson, index) => titleOf(lesson, locale, positionLabel("lesson", index + 1)));
  const anyLesson = sections.some((section) => (section.lessons ?? []).length > 0);
  requirements.push({
    key: "LESSON_VIDEOS",
    met: anyLesson && lessonsWithoutVideo.length === 0,
    offenders: lessonsWithoutVideo,
  });

  const metCount = requirements.filter((requirement) => requirement.met).length;
  return {
    requirements,
    ready: metCount === requirements.length,
    metCount,
    totalCount: requirements.length,
  };
}

/**
 * The server's submission codes, in the Instructor's language.
 *
 * `describeApiError` joins each violation's code, target and dimension with middle dots, which is
 * the right answer for a support log and the wrong one for the person holding the course. The
 * target is always `kind:uuid` and is dropped entirely: it names an object the Instructor has
 * never seen an identifier for, and the checklist above already names the same objects by title.
 *
 * A code with no mapping falls through to the server's own text rather than being swallowed. That
 * matters: an unrecognised code is usually a new rule, and hiding it would leave an Instructor
 * facing a refusal with no reason at all.
 */
export type SubmissionRejection = {
  /** One sentence per distinct requirement the server refused, deduplicated. */
  reasons: string[];
  /** True when at least one violation could not be translated and `detail` carries it. */
  hasUntranslated: boolean;
};

const VIOLATION_KEYS: Record<string, string> = {
  COURSE_EMPTY: "COURSE_EMPTY",
  SECTION_EMPTY: "SECTION_EMPTY",
  LESSON_VIDEO_MISSING: "LESSON_VIDEO_MISSING",
  ASSET_VERSION_UNAVAILABLE: "ASSET_VERSION_UNAVAILABLE",
  ACADEMIC_INSTITUTION_MISSING: "ACADEMIC_INSTITUTION_MISSING",
  ACADEMIC_SUBJECT_MISSING: "ACADEMIC_SUBJECT_MISSING",
  ACADEMIC_SUBJECT_UNAVAILABLE: "ACADEMIC_SUBJECT_UNAVAILABLE",
  ACADEMIC_SUBJECT_RETIRED: "ACADEMIC_SUBJECT_RETIRED",
  ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE: "ACADEMIC_AUDIENCE_TARGET_UNAVAILABLE",
  TAXONOMY_DIMENSION_MISSING: "TAXONOMY_DIMENSION_MISSING",
  TAXONOMY_TERM_UNAVAILABLE: "TAXONOMY_TERM_UNAVAILABLE",
};

export function describeSubmissionRejection(
  error: unknown,
  reasonFor: (code: string) => string | undefined,
): SubmissionRejection | null {
  const problem = (error as { problem?: Record<string, unknown> } | undefined)?.problem;
  if (!problem) return null;

  const raw = [
    ...((problem.violations as Array<{ code?: string }> | undefined) ?? []),
    ...((problem.errors as Array<{ code?: string }> | undefined) ?? []),
  ];
  if (raw.length === 0) return null;

  const reasons: string[] = [];
  let hasUntranslated = false;
  for (const violation of raw) {
    const code = violation.code;
    if (!code) continue;
    const known = VIOLATION_KEYS[code];
    const reason = known ? reasonFor(known) : undefined;
    if (!reason) {
      hasUntranslated = true;
      continue;
    }
    // One line per requirement, however many objects failed it: twelve lessons missing a video is
    // one thing to do, not twelve things to read.
    if (!reasons.includes(reason)) reasons.push(reason);
  }

  if (reasons.length === 0 && !hasUntranslated) return null;
  return { reasons, hasUntranslated };
}
