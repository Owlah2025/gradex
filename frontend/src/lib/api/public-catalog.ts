import { isProblem, ProblemError } from "./problem";
import { publicCatalogRequest as publicRequest } from "./public-request";

export type PublicTaxonomy = { label: string; code?: string };
export type PublicPrice = {
  /** Effective amount retained for existing Course clients. */
  minor_units: number;
  regular_minor_units?: number;
  offer_minor_units?: number | null;
  currency: "KWD";
};
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
  thumbnail?: { asset_version_id: string; card_url: string; large_url: string } | null;
};
/**
 * One Lesson of the live revision whose video an anonymous visitor may watch.
 *
 * The public projection returns these and nothing else about the curriculum's lessons: a visitor
 * who has not paid sees which Lessons they can watch, and nothing more about the ones they cannot.
 * There is no asset identifier, no duration and no state here, because the preview authorization
 * endpoint re-proves the whole chain server-side and needs only the Lesson.
 */
export type PublicPreviewableLesson = { id: string; title: string; position: number };
export type PublicCourseDetail = PublicCourse & {
  description: string;
  sections: {
    title: string;
    position: number;
    lesson_count: number;
    /**
     * The previewable Lessons of this section, in curriculum order. Absent or empty is the ordinary
     * case, and `lesson_count` remains the count of the WHOLE section rather than of this list.
     */
    lessons?: PublicPreviewableLesson[];
  }[];
  /** Localized Program names this Course is relevant to. Never identifiers. */
  program_audience?: string[];
};
export type PublicBundleMember = {
  course_id: string;
  slug: string;
  title: string;
  instructor_display_name: string;
  university?: PublicTaxonomy;
  major?: PublicTaxonomy;
  subject?: PublicTaxonomy;
  study_year?: PublicTaxonomy;
  thumbnail?: { asset_version_id: string; card_url: string; large_url: string } | null;
  position: number;
};
export type PublicBundle = {
  id: string;
  slug: string;
  title: string;
  description: string;
  course_count: number;
  price: PublicPrice;
  members: PublicBundleMember[];
};
export type PublicBundleList = {
  items: PublicBundle[];
  page: number;
  page_size: number;
  total: number;
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
/**
 * An anonymous Lesson preview capability.
 *
 * `manifest_url` is an application route, not a storage URL: the protected HLS master is generated
 * per request from the same canonical renditions paid playback uses. There is no object URL here to
 * leak or to cache.
 */
export type LessonPreviewAuthorization = {
  preview_session: string;
  manifest_url: string;
  expires_at: string;
};

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
/**
 * The University this product launched for.
 *
 * Matched on the slug rather than on either display name, so the rule holds in
 * both languages without comparing Arabic strings, and does not break the first
 * time someone edits the name.
 */
const HOME_INSTITUTION_SLUG = "kuwait-university";

/**
 * Kuwait University first, everything else exactly as the server ordered it.
 *
 * The API returns institutions `ORDER BY name_en ASC`, which is deterministic
 * and stays that way — so several institutions sort ahead of Kuwait University
 * on the alphabet alone, and the University most visitors are actually looking
 * for was not the one they saw first.
 *
 * Presentation only. Nothing here changes the request, the server's ordering,
 * or an institution's identity; the sort is stable, so the remaining rows keep
 * the relative order they arrived in rather than acquiring a second, competing
 * one. Applied at the shared client rather than in one component because every
 * Student-facing list of Universities reads this function — the landing rail,
 * the academic picker, the catalogue filter, and subject discovery. Two of them
 * appear on the same page, and disagreeing about the order reads as a bug.
 */
function homeInstitutionFirst(items: InstitutionOption[]): InstitutionOption[] {
  return items
    .map((item, index) => ({ item, index }))
    .sort((a, b) => {
      const aHome = a.item.slug === HOME_INSTITUTION_SLUG ? 0 : 1;
      const bHome = b.item.slug === HOME_INSTITUTION_SLUG ? 0 : 1;
      return aHome - bHome || a.index - b.index;
    })
    .map(({ item }) => item);
}

export function getPublicInstitutions(locale: "ar" | "en") {
  return publicRequest<{ items: InstitutionOption[] }>(
    `/academic-options/institutions`,
    locale,
  ).then((body) => homeInstitutionFirst(body.items));
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

export function getPublicBundles(locale: "ar" | "en") {
  return publicRequest<PublicBundleList>("/bundles", locale);
}

export function getPublicBundle(idOrSlug: string, locale: "ar" | "en") {
  return publicRequest<PublicBundle>(`/bundles/${encodeURIComponent(idOrSlug)}`, locale);
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

/**
 * Authorizes anonymous preview of one Lesson.
 *
 * Nothing about what may be watched is decided here. The server re-proves the Course's published
 * state, the live revision, the lesson's membership of it, the preview flag, the exact video asset
 * version, retirement, READY state and the canonical renditions — on this request and again on
 * every manifest request. A refusal is deliberately indistinguishable between "not previewable",
 * "still processing" and "does not exist", so it reveals nothing about a Course's unpublished
 * contents.
 */
export async function getLessonPreview(
  courseID: string,
  lessonID: string,
  locale: "ar" | "en",
): Promise<LessonPreviewAuthorization> {
  const response = await fetch(
    `/api/v1/media/courses/${encodeURIComponent(courseID)}/lessons/${encodeURIComponent(lessonID)}/preview-authorizations`,
    {
      method: "POST",
      headers: {
        Accept: "application/json, application/problem+json",
        "Accept-Language": locale,
      },
      cache: "no-store",
    },
  );
  const body: unknown = await response.json();
  if (!response.ok) {
    throw isProblem(body) ? new ProblemError(body) : new Error("Lesson preview request failed");
  }
  return body as LessonPreviewAuthorization;
}
