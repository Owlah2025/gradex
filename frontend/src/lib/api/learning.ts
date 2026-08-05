import { authenticatedRequest, ensureAnonymousBrowser } from "./http";

export type LearningStatus = "active" | "expired";
export type LearningMaterialKind = "resource" | "lab_material";

export type LearningMaterial = {
  kind: LearningMaterialKind;
};

export type LearningProgress = {
  position_seconds: number;
  completed: boolean;
};

export type LearningCourseProgress = {
  completed_lessons: number;
  total_lessons: number;
  percent: number;
};

export type DashboardCourse = {
  course_id: string;
  title: string;
  learning_status: LearningStatus;
  expires_at: string | null;
  progress: LearningCourseProgress;
};

export type LearningDashboard = {
  courses: DashboardCourse[];
};

export type CourseHomeLesson = {
  lesson_id: string;
  title: string;
  progress: LearningProgress;
  materials: LearningMaterial[];
};

export type CourseHomeSection = {
  section_id: string;
  title: string;
  lessons: CourseHomeLesson[];
};

/**
 * An opaque, server-encrypted report context (D-065).
 *
 * It binds a future report to the exact content instance this response rendered. It is not
 * readable, not decodable by the client, and grants no authority — it is carried, never
 * interpreted. Do not render it, place it in the DOM or an attribute, persist it to
 * localStorage/sessionStorage, log it, or decode it.
 */
export type ReportContext = string;

export type CourseHome = {
  course_id: string;
  title: string;
  learning_status: LearningStatus;
  expires_at: string | null;
  progress: LearningCourseProgress;
  sections: CourseHomeSection[];
  /** Present only on an active read; absent when access has expired or content is unavailable. */
  report_context?: ReportContext;
};

export type LessonNavigation = {
  previous_lesson_id: string | null;
  next_lesson_id: string | null;
};

export type LessonReadModel = {
  course_id: string;
  lesson_id: string;
  section: { section_id: string; title: string };
  title: string;
  learning_status: LearningStatus;
  expires_at: string | null;
  progress: LearningProgress;
  navigation: LessonNavigation;
  materials: LearningMaterial[];
  /**
   * Present only on an active read, and only for target kinds actually present in the visible
   * Lesson. A kind absent here is a kind this page cannot report — availability is read from the
   * context alone, never inferred from a title, a player, or a material route.
   */
  report_contexts?: {
    lesson: ReportContext;
    video?: ReportContext;
    resource?: ReportContext;
    lab_material?: ReportContext;
  };
};

export type PlaybackAuthorization = {
  playback_session: string;
  manifest_url: string;
  asset_version_id: string;
  expires_at: string;
};

/**
 * The closed report reason set, exactly as the server's `rep_reason` enumeration defines it.
 *
 * These are wire values, never display text: each is mapped to localized copy in the dictionaries.
 * Widening this list without widening the database constraint produces a `422`.
 */
export const learningReportReasons = [
  "broken_unavailable",
  "inaccurate",
  "inappropriate",
  "suspected_copyright_violation",
  "other",
] as const;

export type LearningReportReason = (typeof learningReportReasons)[number];

export function isLearningReportReason(value: string): value is LearningReportReason {
  return (learningReportReasons as readonly string[]).includes(value);
}

/**
 * The whole public acknowledgement (FR-034). There is no status, no queue position, and no route
 * that reads a report back, so nothing here is a handle to anything.
 */
export type LearningReportAcknowledgement = {
  report_id: string;
  created_at: string;
};

export type LearningReportSubmission = {
  /** Carried verbatim from the read model that rendered the target. Never inspected. */
  report_context: ReportContext;
  reason: LearningReportReason;
  explanation?: string;
};

/**
 * Submits one content report.
 *
 * The request body is the only place a report context is ever sent, and it is passed through
 * untouched — this function does not decode, parse, trim, store, or log it, and no error it throws
 * carries it. There is deliberately **no retry**: a resent report either duplicates the Student's
 * own open report or spends another of their five hourly attempts, so recovery is always an
 * explicit action by the Student.
 */
export async function submitLearningReport(
  submission: LearningReportSubmission,
  locale: "ar" | "en",
  csrf: string | null,
): Promise<LearningReportAcknowledgement> {
  const effectiveCSRF = csrf || (await ensureAnonymousBrowser());
  const body: LearningReportSubmission = {
    report_context: submission.report_context,
    reason: submission.reason,
  };
  // The server rejects unknown fields, and an empty explanation is not an explanation.
  if (submission.explanation && submission.explanation.trim() !== "") {
    body.explanation = submission.explanation;
  }
  return authenticatedRequest<LearningReportAcknowledgement>(
    "/learn/reports",
    "POST",
    locale,
    effectiveCSRF,
    body,
  ).then((acknowledgement) =>
    requireLearningResponse(acknowledgement, "Report acknowledgement"),
  );
}

export async function requestPlayback(
  lessonID: string,
  locale: "ar" | "en",
  csrf: string | null,
): Promise<PlaybackAuthorization> {
  const effectiveCSRF = csrf || (await ensureAnonymousBrowser());
  return authenticatedRequest<PlaybackAuthorization>(
    `/learn/lessons/${encodeURIComponent(lessonID)}/playback`,
    "POST",
    locale,
    effectiveCSRF,
  ).then((playback) => {
    if (playback === null) throw new Error("Playback authorization was empty.");
    return playback;
  });
}

function requireLearningResponse<T>(response: T | null, description: string): T {
  if (response === null) throw new Error(`${description} was empty.`);
  return response;
}

export function requestLearningDashboard(locale: "ar" | "en"): Promise<LearningDashboard> {
  return authenticatedRequest<LearningDashboard>("/learn/dashboard", "GET", locale).then((response) =>
    requireLearningResponse(response, "Learning dashboard response"),
  );
}

export function requestCourseHome(courseID: string, locale: "ar" | "en"): Promise<CourseHome> {
  return authenticatedRequest<CourseHome>(
    `/learn/courses/${encodeURIComponent(courseID)}`,
    "GET",
    locale,
  ).then((response) => requireLearningResponse(response, "Course Home response"));
}

export function requestLessonReadModel(
  courseID: string,
  lessonID: string,
  locale: "ar" | "en",
): Promise<LessonReadModel> {
  return authenticatedRequest<LessonReadModel>(
    `/learn/courses/${encodeURIComponent(courseID)}/lessons/${encodeURIComponent(lessonID)}`,
    "GET",
    locale,
  ).then((response) => requireLearningResponse(response, "Lesson response"));
}
