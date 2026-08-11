import { authenticatedRequest } from "./http";
import type { CourseRevisionWire, OwnedCourseSummary } from "./catalog";

/**
 * Admin Course review commands.
 *
 * Every function here is a thin, typed call onto a route the Go API already
 * serves under `/admin/review`. This module holds no queue of its own: the
 * server's `PENDING_REVIEW` revisions are the only queue that exists, and an
 * empty response is an empty queue, never a cue to invent a fixture.
 */

export type ReviewInput = {
  locale: "ar" | "en";
  csrf: string;
};

/** One `PENDING_REVIEW` revision, exactly as `catalog.ReviewQueueItem` serializes it. */
export type ReviewQueueItem = {
  course_id: string;
  owner_account_id: string;
  revision_id: string;
  revision_number: number;
  title_ar: string;
  title_en: string;
  submitted_at?: string | null;
  course_lifecycle: string;
  is_first_publish: boolean;
};

/**
 * A same-origin protected manifest route issued for an Admin's review of one
 * submitted Lesson. It is deliberately not an object-storage URL.
 */
export type AdminLessonPreview = {
  course_id: string;
  revision_id: string;
  lesson_id: string;
  video_asset_version_id: string;
  playback_url: string;
};

export type ReviewedCourse = OwnedCourseSummary & {
  live_revision_id?: string | null;
  editable_revision?: CourseRevisionWire;
  live_revision?: CourseRevisionWire;
};

function requireCSRF(input: ReviewInput): void {
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

const revisionPath = (courseID: string, revisionID: string) =>
  `/admin/review/courses/${encodeURIComponent(courseID)}/revisions/${encodeURIComponent(revisionID)}`;

/**
 * Reads the Admin review queue.
 *
 * A `200` carrying `[]` is an honest empty queue and is returned as such. A
 * `204`/null body is not: that would leave the caller unable to distinguish
 * "nothing is pending" from "the server said nothing", so it fails closed.
 */
export async function listReviewQueue(locale: "ar" | "en"): Promise<ReviewQueueItem[]> {
  const queue = await authenticatedRequest<ReviewQueueItem[]>("/admin/review/queue", "GET", locale);
  if (queue === null) {
    throw new Error(
      locale === "ar" ? "لم يرجع الخادم قائمة المراجعة" : "No review queue returned from server",
    );
  }
  return queue;
}

/** Reads the full authored graph of one submitted revision, by its authoritative IDs. */
export async function getReviewCourseRevision(
  courseID: string,
  revisionID: string,
  locale: "ar" | "en",
): Promise<ReviewedCourse> {
  const course = await authenticatedRequest<ReviewedCourse>(
    revisionPath(courseID, revisionID),
    "GET",
    locale,
  );
  return requireResult(course, locale);
}

export async function approveCourseRevision(
  input: ReviewInput & { courseID: string; revisionID: string },
): Promise<ReviewedCourse> {
  requireCSRF(input);
  const course = await authenticatedRequest<ReviewedCourse>(
    `${revisionPath(input.courseID, input.revisionID)}/approve`,
    "POST",
    input.locale,
    input.csrf,
  );
  return requireResult(course, input.locale);
}

/**
 * Returns the submitted revision to the Instructor with a mandatory reason.
 *
 * The reason is required by the Go handler, so it is required here too rather
 * than being sent empty and refused on the wire.
 */
export async function requestCourseRevisionChanges(
  input: ReviewInput & { courseID: string; revisionID: string; reason: string },
): Promise<ReviewedCourse> {
  requireCSRF(input);
  if (!input.reason.trim()) {
    throw new Error(
      input.locale === "ar" ? "سبب طلب التعديلات إجباري" : "Reason for change request is mandatory",
    );
  }
  const course = await authenticatedRequest<ReviewedCourse>(
    `${revisionPath(input.courseID, input.revisionID)}/request-changes`,
    "POST",
    input.locale,
    input.csrf,
    { reason: input.reason.trim() },
  );
  return requireResult(course, input.locale);
}

/**
 * Issues the existing audited Admin-review preview for one Lesson in the
 * inspected revision. The returned URL is an application-owned protected
 * playback route and is held by the caller only for the active view.
 */
export async function previewAdminLesson(
  input: ReviewInput & { courseID: string; revisionID: string; lessonID: string },
): Promise<AdminLessonPreview> {
  requireCSRF(input);
  const preview = await authenticatedRequest<AdminLessonPreview>(
    `${revisionPath(input.courseID, input.revisionID)}/preview/${encodeURIComponent(input.lessonID)}`,
    "POST",
    input.locale,
    input.csrf,
  );
  return requireResult(preview, input.locale);
}
