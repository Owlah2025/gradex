import { authenticatedRequest } from "./http";
import type { CourseRevisionWire, LessonWire, OwnedCourseSummary, SectionWire } from "./catalog";

/**
 * Instructor Course authoring commands.
 *
 * Every function here is a thin, typed call onto a route the Go API already
 * serves. Nothing in this module holds Course state: the server's response is
 * the only Course that exists, and callers re-read the owned-Course graph
 * rather than reconciling a local copy.
 */

export type AuthoringInput = {
  locale: "ar" | "en";
  csrf: string;
};

export type CourseWire = OwnedCourseSummary & {
  editable_revision?: CourseRevisionWire;
};

function requireCSRF(input: AuthoringInput): void {
  if (!input.csrf) {
    throw new Error(
      input.locale === "ar" ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing",
    );
  }
}

function requireResult<T>(result: T | null, locale: "ar" | "en"): T {
  if (result === null) {
    throw new Error(locale === "ar" ? "لم يرجع الخادم نتيجة" : "The server returned an empty result");
  }
  return result;
}

const path = {
  course: (courseID: string) => `/courses/${encodeURIComponent(courseID)}`,
  revision: (courseID: string, revisionID: string) =>
    `/courses/${encodeURIComponent(courseID)}/revisions/${encodeURIComponent(revisionID)}`,
};

export async function createCourse(
  input: AuthoringInput & {
    titleAr: string;
    titleEn: string;
    descriptionAr: string;
    descriptionEn: string;
  },
): Promise<CourseWire> {
  requireCSRF(input);
  const created = await authenticatedRequest<CourseWire>("/courses", "POST", input.locale, input.csrf, {
    title_ar: input.titleAr,
    title_en: input.titleEn,
    description_ar: input.descriptionAr,
    description_en: input.descriptionEn,
  });
  return requireResult(created, input.locale);
}

/**
 * Updates the named editable revision.
 *
 * The Go handler binds a complete revision body, so every localized field is
 * sent on every call. Sending a partial body would blank the fields it omits.
 */
export async function updateCourseRevision(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    titleAr: string;
    titleEn: string;
    descriptionAr: string;
    descriptionEn: string;
    majorTermID?: string;
    subjectTermID?: string;
    studyYear?: string;
  },
): Promise<CourseRevisionWire> {
  requireCSRF(input);
  const updated = await authenticatedRequest<CourseRevisionWire>(
    path.revision(input.courseID, input.revisionID),
    "PATCH",
    input.locale,
    input.csrf,
    {
      title_ar: input.titleAr,
      title_en: input.titleEn,
      description_ar: input.descriptionAr,
      description_en: input.descriptionEn,
      major_term_id: input.majorTermID || undefined,
      subject_term_id: input.subjectTermID || undefined,
      study_year: input.studyYear || undefined,
    },
  );
  return requireResult(updated, input.locale);
}

export async function addSection(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    titleAr: string;
    titleEn: string;
  },
): Promise<SectionWire> {
  requireCSRF(input);
  const created = await authenticatedRequest<SectionWire>(
    `${path.revision(input.courseID, input.revisionID)}/sections`,
    "POST",
    input.locale,
    input.csrf,
    { title_ar: input.titleAr, title_en: input.titleEn },
  );
  return requireResult(created, input.locale);
}

export async function updateSection(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    sectionID: string;
    titleAr: string;
    titleEn: string;
  },
): Promise<SectionWire> {
  requireCSRF(input);
  const updated = await authenticatedRequest<SectionWire>(
    `${path.revision(input.courseID, input.revisionID)}/sections/${encodeURIComponent(input.sectionID)}`,
    "PATCH",
    input.locale,
    input.csrf,
    { title_ar: input.titleAr, title_en: input.titleEn },
  );
  return requireResult(updated, input.locale);
}

export async function deleteSection(
  input: AuthoringInput & { courseID: string; revisionID: string; sectionID: string },
): Promise<void> {
  requireCSRF(input);
  await authenticatedRequest<unknown>(
    `${path.revision(input.courseID, input.revisionID)}/sections/${encodeURIComponent(input.sectionID)}`,
    "DELETE",
    input.locale,
    input.csrf,
  );
}

export async function addLesson(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    sectionID: string;
    titleAr: string;
    titleEn: string;
  },
): Promise<LessonWire> {
  requireCSRF(input);
  const created = await authenticatedRequest<LessonWire>(
    `${path.revision(input.courseID, input.revisionID)}/sections/${encodeURIComponent(input.sectionID)}/lessons`,
    "POST",
    input.locale,
    input.csrf,
    { title_ar: input.titleAr, title_en: input.titleEn },
  );
  return requireResult(created, input.locale);
}

export async function deleteLesson(
  input: AuthoringInput & { courseID: string; revisionID: string; lessonID: string },
): Promise<void> {
  requireCSRF(input);
  await authenticatedRequest<unknown>(
    `${path.revision(input.courseID, input.revisionID)}/lessons/${encodeURIComponent(input.lessonID)}`,
    "DELETE",
    input.locale,
    input.csrf,
  );
}

/**
 * Attaches an Asset Version to a Lesson.
 *
 * The server re-validates that the Asset Version exists, is READY, and belongs
 * to a Course this Instructor owns; a successful response is the only evidence
 * that the Lesson now has a video.
 */
export async function setLessonVideo(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    lessonID: string;
    assetVersionID: string;
  },
): Promise<LessonWire> {
  requireCSRF(input);
  const updated = await authenticatedRequest<LessonWire>(
    `${path.revision(input.courseID, input.revisionID)}/lessons/${encodeURIComponent(input.lessonID)}/video`,
    "PUT",
    input.locale,
    input.csrf,
    { video_asset_version_id: input.assetVersionID },
  );
  return requireResult(updated, input.locale);
}

export async function submitCourseRevision(
  input: AuthoringInput & { courseID: string; revisionID: string },
): Promise<CourseWire> {
  requireCSRF(input);
  const submitted = await authenticatedRequest<CourseWire>(
    `${path.revision(input.courseID, input.revisionID)}/submit`,
    "POST",
    input.locale,
    input.csrf,
  );
  return requireResult(submitted, input.locale);
}

export { getOwnedCourses, getOwnedCourseDetail } from "./catalog";
export type { CourseRevisionWire, LessonWire, OwnedCourseSummary, SectionWire } from "./catalog";
