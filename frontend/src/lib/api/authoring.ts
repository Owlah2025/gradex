import { authenticatedRequest } from "./http";
import type {
  CourseRevisionWire,
  LessonFileWire,
  LessonWire,
  OwnedCourseSummary,
	RevisionAudienceWire,
  SectionWire,
} from "./catalog";

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

/**
 * Begins a new editable revision of an already-published Course.
 *
 * The server clones the live revision — metadata, taxonomy, sections, Lessons, and their Asset
 * Version references — into a fresh `DRAFT` and leaves `live_revision_id` pointing at the published
 * revision, so Students keep seeing the published Course throughout.
 *
 * Safe to call more than once: the server keeps at most one active candidate per Course and returns
 * the existing `DRAFT`/`CHANGES_REQUESTED`/`PENDING_REVIEW` revision instead of cloning a second.
 */
export async function createCandidateRevision(
  input: AuthoringInput & { courseID: string },
): Promise<CourseRevisionWire> {
  requireCSRF(input);
  const candidate = await authenticatedRequest<CourseRevisionWire>(
    `${path.course(input.courseID)}/candidate`,
    "PUT",
    input.locale,
    input.csrf,
  );
  return requireResult(candidate, input.locale);
}

/**
 * Creates a Course on the Academic Catalog model (D-093 §1, T4-B).
 *
 * The university is required. The canonical Subject is normally supplied; T4-D
 * may omit it only for a subject-less draft immediately attached to a request.
 * There is deliberately
 * no classification argument: the server derives ACADEMIC_CATALOG from the
 * academic context, so this module offers no way to create a legacy Course.
 * Existing legacy Courses stay editable through their own compatibility surface
 * until T5 migrates them.
 */
export async function createCourse(
  input: AuthoringInput & {
    titleAr: string;
    titleEn: string;
    descriptionAr: string;
    descriptionEn: string;
    institutionID: string;
    subjectID?: string;
  },
): Promise<CourseWire> {
  requireCSRF(input);
  const created = await authenticatedRequest<CourseWire>("/courses", "POST", input.locale, input.csrf, {
    title_ar: input.titleAr,
    title_en: input.titleEn,
    description_ar: input.descriptionAr,
    description_en: input.descriptionEn,
    institution_id: input.institutionID,
    ...(input.subjectID ? { subject_id: input.subjectID } : {}),
  });
  return requireResult(created, input.locale);
}

/**
 * Corrects the canonical Subject of an Academic Course that has never been
 * published.
 *
 * Every lifecycle rule lives on the server: never published, no candidate under
 * review, active Subject, same Institution. This call does not pre-judge them —
 * a refusal comes back as a semantic problem the surface reports.
 */
export async function setCourseSubject(
  input: AuthoringInput & { courseID: string; subjectID: string },
): Promise<CourseWire> {
  requireCSRF(input);
  const updated = await authenticatedRequest<CourseWire>(
    `/courses/${encodeURIComponent(input.courseID)}/subject`,
    "PUT",
    input.locale,
    input.csrf,
    { subject_id: input.subjectID },
  );
  return requireResult(updated, input.locale);
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

export async function setRevisionAudience(
	input: AuthoringInput & { courseID: string; revisionID: string; programIDs: string[] },
): Promise<RevisionAudienceWire> {
	requireCSRF(input);
	const audience = await authenticatedRequest<RevisionAudienceWire>(
		`/courses/${encodeURIComponent(input.courseID)}/revisions/${encodeURIComponent(input.revisionID)}/audience`,
		"PUT",
		input.locale,
		input.csrf,
		{ program_ids: input.programIDs },
	);
	return requireResult(audience, input.locale);
}

export async function resetRevisionAudience(
	input: AuthoringInput & { courseID: string; revisionID: string },
): Promise<RevisionAudienceWire> {
	requireCSRF(input);
	const audience = await authenticatedRequest<RevisionAudienceWire>(
		`/courses/${encodeURIComponent(input.courseID)}/revisions/${encodeURIComponent(input.revisionID)}/audience`,
		"DELETE",
		input.locale,
		input.csrf,
	);
	return requireResult(audience, input.locale);
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

/**
 * Attaches a READY, separately uploaded public-preview Asset Version to this
 * editable revision. The server proves kind, ownership, Course/revision origin
 * and scanner evidence; callers never send a Lesson or storage identifier.
 */
export async function setPublicPreview(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    assetVersionID: string;
  },
): Promise<CourseRevisionWire> {
  requireCSRF(input);
  const revision = await authenticatedRequest<CourseRevisionWire>(
    `${path.revision(input.courseID, input.revisionID)}/preview`,
    "PUT",
    input.locale,
    input.csrf,
    { preview_asset_version_id: input.assetVersionID },
  );
  return requireResult(revision, input.locale);
}

/** Removes the preview from this editable revision without deleting bytes. */
export async function clearPublicPreview(
  input: AuthoringInput & { courseID: string; revisionID: string },
): Promise<CourseRevisionWire> {
  requireCSRF(input);
  const revision = await authenticatedRequest<CourseRevisionWire>(
    `${path.revision(input.courseID, input.revisionID)}/preview`,
    "DELETE",
    input.locale,
    input.csrf,
  );
  return requireResult(revision, input.locale);
}

/**
 * Attaches a READY Asset Version to a Lesson as a downloadable file.
 *
 * The server re-validates the Asset Version's readiness and this Instructor's
 * ownership of the Course, so a successful response is the only evidence that
 * the Lesson now carries the file.
 */
export async function addLessonFile(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    lessonID: string;
    kind: LessonFileWire["kind"];
    assetVersionID: string;
    displayNameAr: string;
    displayNameEn: string;
  },
): Promise<LessonFileWire> {
  requireCSRF(input);
  const created = await authenticatedRequest<LessonFileWire>(
    `${path.revision(input.courseID, input.revisionID)}/lessons/${encodeURIComponent(input.lessonID)}/files`,
    "PUT",
    input.locale,
    input.csrf,
    {
      kind: input.kind,
      asset_version_id: input.assetVersionID,
      display_name_ar: input.displayNameAr,
      display_name_en: input.displayNameEn,
    },
  );
  return requireResult(created, input.locale);
}

/**
 * Detaches one file from a Lesson.
 *
 * The stored Asset Version is untouched: media versions are immutable, and
 * removing a file from a draft Lesson is a Course-authoring change, not a
 * deletion of bytes.
 */
export async function deleteLessonFile(
  input: AuthoringInput & {
    courseID: string;
    revisionID: string;
    lessonID: string;
    fileID: string;
  },
): Promise<void> {
  requireCSRF(input);
  await authenticatedRequest<unknown>(
    `${path.revision(input.courseID, input.revisionID)}/lessons/${encodeURIComponent(input.lessonID)}/files?file_id=${encodeURIComponent(input.fileID)}`,
    "DELETE",
    input.locale,
    input.csrf,
  );
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
export type {
  CourseRevisionWire,
  LessonFileWire,
  LessonWire,
  OwnedCourseSummary,
  SectionWire,
} from "./catalog";
