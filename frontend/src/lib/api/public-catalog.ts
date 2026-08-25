import { isProblem, ProblemError } from "./problem";

export type PublicTaxonomy = { label: string; code?: string };
export type PublicPrice = { minor_units: number; currency: "KWD" };
export type PublicCourse = {
  id: string;
  slug: string;
  title: string;
  instructor_display_name: string;
  university?: PublicTaxonomy;
  major?: PublicTaxonomy;
  subject?: PublicTaxonomy;
  study_year?: PublicTaxonomy;
  price?: PublicPrice;
  has_preview: boolean;
};
export type PublicCourseDetail = PublicCourse & {
  description: string;
  sections: { title: string; position: number; lesson_count: number }[];
  /** Localized Program names this Course is relevant to. Never identifiers. */
  program_audience?: string[];
};

/**
 * Academic discovery filters (T6).
 *
 * Every value is a public, shareable slug or Subject code produced by the
 * option endpoints below. The Student never types or sees one — selecting a
 * named option is what produces it — and no UUID is required to express a
 * filter.
 */
export type AcademicFilters = {
  institution?: string;
  program?: string;
  /** The academic level a study plan records for the Course's Subject. */
  level?: string;
  subject?: string;
  /**
   * Ranking input only. It carries the Program the signed-in Student's own
   * academic profile names so relevant Courses sort first. It never removes a
   * Course from the catalogue and it never grants access to one.
   */
  relevantToProgram?: string;
};

export type InstitutionOption = {
  slug: string;
  name_ar: string;
  name_en: string;
};
export type ProgramOption = {
  slug: string;
  name_ar: string;
  name_en: string;
  college_name_ar?: string;
  college_name_en?: string;
};
export type SubjectOption = {
  value: string;
  code?: string;
  title_ar: string;
  title_en: string;
};
export type PublicCourseList = {
  items: PublicCourse[];
  page: number;
  page_size: number;
  total: number;
};
export type PublicPreviewAuthorization = { url: string; expires_at: string };

async function publicRequest<T>(path: string, locale: "ar" | "en"): Promise<T> {
  const response = await fetch(`/api/v1/catalog${path}`, {
    headers: {
      Accept: "application/json, application/problem+json",
      "Accept-Language": locale,
    },
    cache: "no-store",
  });
  const body: unknown = await response.json();
  if (!response.ok)
    throw isProblem(body)
      ? new ProblemError(body)
      : new Error("Public catalogue request failed");
  return body as T;
}

export function getPublicCourses(
  locale: "ar" | "en",
  query = "",
  filters: AcademicFilters = {},
) {
  const parameters = new URLSearchParams();
  if (query !== "") parameters.set("q", query);
  // An empty filter is omitted rather than sent as an empty value, so "no
  // filter" and "a filter matching nothing" stay different requests.
  if (filters.institution) parameters.set("institution", filters.institution);
  if (filters.program) parameters.set("program", filters.program);
  if (filters.level) parameters.set("level", filters.level);
  if (filters.subject) parameters.set("subject", filters.subject);
  if (filters.relevantToProgram)
    parameters.set("relevant_to_program", filters.relevantToProgram);
  const suffix = parameters.size === 0 ? "" : `?${parameters}`;
  return publicRequest<PublicCourseList>(`/courses${suffix}`, locale);
}

/**
 * The public academic option lists that drive the catalogue's filters.
 *
 * These are the anonymous catalogue endpoints, deliberately not the Admin or
 * Student academic surfaces: a public page must never call an authenticated
 * one, and the Admin lists carry retired rows and audit metadata that must not
 * reach a visitor.
 */
export function getPublicInstitutions(locale: "ar" | "en") {
  return publicRequest<{ items: InstitutionOption[] }>(
    `/academic-options/institutions`,
    locale,
  ).then((body) => body.items);
}

export function getPublicPrograms(
  institutionSlug: string,
  locale: "ar" | "en",
) {
  return publicRequest<{ items: ProgramOption[] }>(
    `/academic-options/institutions/${encodeURIComponent(institutionSlug)}/programs`,
    locale,
  ).then((body) => body.items);
}

export function getPublicLevels(
  institutionSlug: string,
  programSlug: string,
  locale: "ar" | "en",
) {
  const suffix =
    programSlug === "" ? "" : `?program=${encodeURIComponent(programSlug)}`;
  return publicRequest<{ items: number[] }>(
    `/academic-options/institutions/${encodeURIComponent(institutionSlug)}/levels${suffix}`,
    locale,
  ).then((body) => body.items);
}

export function getPublicSubjects(
  institutionSlug: string,
  programSlug: string,
  locale: "ar" | "en",
) {
  const suffix =
    programSlug === "" ? "" : `?program=${encodeURIComponent(programSlug)}`;
  return publicRequest<{ items: SubjectOption[] }>(
    `/academic-options/institutions/${encodeURIComponent(institutionSlug)}/subjects${suffix}`,
    locale,
  ).then((body) => body.items);
}
export function getPublicCourse(idOrSlug: string, locale: "ar" | "en") {
  return publicRequest<PublicCourseDetail>(
    `/courses/${encodeURIComponent(idOrSlug)}`,
    locale,
  );
}

/**
 * Requests the preview for the public Course, not for a browser-supplied Asset
 * Version. The server resolves the currently approved live revision before
 * returning an expiry-bounded media URL.
 */
export async function getPublicCoursePreview(
  courseID: string,
  locale: "ar" | "en",
): Promise<PublicPreviewAuthorization> {
  const response = await fetch(
    `/api/v1/media/courses/${encodeURIComponent(courseID)}/preview`,
    {
      headers: {
        Accept: "application/json, application/problem+json",
        "Accept-Language": locale,
      },
      cache: "no-store",
    },
  );
  const body: unknown = await response.json();
  if (!response.ok) {
    throw isProblem(body)
      ? new ProblemError(body)
      : new Error("Public preview request failed");
  }
  return body as PublicPreviewAuthorization;
}
