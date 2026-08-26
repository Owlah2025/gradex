import type { PublicCourseDetail } from "@/lib/api/public-catalog";

/**
 * The data transforms behind public Course Details, kept out of the components that draw them.
 *
 * Every function here reads only fields `GET /api/v1/catalog/courses/{idOrSlug}` actually returns.
 * There is deliberately nothing that derives a rating, a student count, a course duration, a
 * last-updated date, or a learning outcome: the public contract carries none of those, and a
 * derived-looking value with no source is indistinguishable to a reader from a real one.
 */

/** How the page names one academic field. Already localized by the caller. */
export type AcademicFactKey = "university" | "major" | "subject" | "level";

export type AcademicFact = {
  key: AcademicFactKey;
  /** The student-facing name of the field — "University", never "taxonomy term". */
  label: string;
  /** The localized term the server returned. */
  value: string;
  /**
   * The Subject's official academic code, where the catalogue has one.
   *
   * Only Subject ever carries one: the projection selects `NULL` for the university and major
   * codes, so treating any other field as code-bearing would render an empty separator forever.
   */
  code?: string;
};

export type AcademicFactLabels = Record<AcademicFactKey, string>;

/**
 * The Course's place in a study plan, as a list of named fields.
 *
 * Absent fields are omitted rather than rendered blank. A public Course may legitimately have no
 * university, major, Subject or level attached, and "University: —" tells a visitor nothing except
 * that the page expected something it did not get.
 */
export function academicFacts(
  course: Pick<PublicCourseDetail, "university" | "major" | "subject" | "study_year">,
  labels: AcademicFactLabels,
): AcademicFact[] {
  const facts: AcademicFact[] = [];
  if (course.university)
    facts.push({ key: "university", label: labels.university, value: course.university.label });
  if (course.major) facts.push({ key: "major", label: labels.major, value: course.major.label });
  if (course.subject)
    facts.push({
      key: "subject",
      label: labels.subject,
      value: course.subject.label,
      code: course.subject.code,
    });
  if (course.study_year)
    facts.push({ key: "level", label: labels.level, value: course.study_year.label });
  return facts;
}

export type CurriculumTotals = { sections: number; lessons: number };

/**
 * How much course there is, counted from the outline the catalogue published.
 *
 * `lesson_count` per section is the deepest the public contract goes — there are no lesson titles,
 * no lesson types and no lesson durations on this endpoint — so this is the whole of what the page
 * may honestly say about size.
 */
export function curriculumTotals(
  sections: PublicCourseDetail["sections"],
): CurriculumTotals {
  return {
    sections: sections.length,
    lessons: sections.reduce((total, section) => total + section.lesson_count, 0),
  };
}

/**
 * How many sections are listed before the outline asks to be expanded.
 *
 * Short outlines are shown whole: hiding two of six rows behind a control costs the reader a click
 * and saves them nothing.
 */
export const SECTION_PREVIEW_LIMIT = 8;

export function outlineNeedsDisclosure(sections: PublicCourseDetail["sections"]): boolean {
  return sections.length > SECTION_PREVIEW_LIMIT;
}

export function visibleSections(
  sections: PublicCourseDetail["sections"],
  expanded: boolean,
): PublicCourseDetail["sections"] {
  return expanded || !outlineNeedsDisclosure(sections)
    ? sections
    : sections.slice(0, SECTION_PREVIEW_LIMIT);
}

/**
 * The instructor's initials, for the avatar fallback.
 *
 * A fallback, not an identity claim: the public contract carries a display name and no photograph,
 * so the avatar can only ever be the name restated. `toUpperCase` is a no-op on Arabic script,
 * which is why the same function serves both languages.
 */
export function instructorInitials(displayName: string): string {
  const words = displayName.trim().split(/\s+/).filter((word) => word !== "");
  if (words.length === 0) return "";
  return words
    .slice(0, 2)
    .map((word) => Array.from(word)[0])
    .join("")
    .toUpperCase();
}
