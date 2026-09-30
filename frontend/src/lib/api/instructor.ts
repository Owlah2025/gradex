import { authenticatedRequest } from "./http";

export type InstructorRevisionSummary = {
  id: string;
  revision_number: number;
  state: string;
  review_reason?: string;
};

export type InstructorDashboardCourse = {
  course_id: string;
  title_ar: string;
  title_en: string;
  lifecycle: string;
  published_revision?: InstructorRevisionSummary;
  candidate_revision?: InstructorRevisionSummary;
  latest_change_request_reason?: string;
  enrollments: number;
  learning_active_students_7d: number;
  average_progress: number;
  completions: number;
  media_processing_failures: number;
};

export type InstructorAlert = {
  kind: "CHANGES_REQUESTED" | "MEDIA_PROCESSING_FAILED" | "AWAITING_REVIEW" | "PROFILE_CHANGES_REQUESTED" | string;
  course_id?: string;
  title_ar: string;
  title_en: string;
  reason?: string;
};

export type InstructorDashboard = {
  courses: InstructorDashboardCourse[];
  alerts: InstructorAlert[];
};

export type CourseLessonReach = {
  lesson_id: string;
  section_title_ar: string;
  section_title_en: string;
  lesson_title_ar: string;
  lesson_title_en: string;
  section_position: number;
  lesson_position: number;
  students_reached: number;
  students_completed: number;
  reach_percent: number;
  drop_off_percent: number;
};

export type CourseAnalytics = {
  course_id: string;
  title_ar: string;
  title_en: string;
  lifecycle: string;
  enrolled: number;
  started: number;
  learning_active_students_7d: number;
  average_progress: number;
  completed: number;
  watch_time_collected: boolean;
  definitions: Record<string, { ar: string; en: string }>;
  lesson_reach: CourseLessonReach[];
};

export type CourseAnnouncement = {
  id: string;
  title: string;
  body: string;
  created_at: string;
  published_at: string;
};

type Locale = "ar" | "en";

function requireResult<T>(result: T | null, locale: Locale, fallback: string): T {
  if (result === null) {
    throw new Error(locale === "ar" ? "لم يُرجع الخادم نتيجة" : fallback);
  }
  return result;
}

export async function getInstructorDashboard(locale: Locale): Promise<InstructorDashboard> {
  const result = await authenticatedRequest<InstructorDashboard>("/instructor/dashboard", "GET", locale);
  return requireResult(result, locale, "No instructor dashboard returned from server");
}

export async function getCourseAnalytics(courseID: string, locale: Locale): Promise<CourseAnalytics> {
  const result = await authenticatedRequest<CourseAnalytics>(
    `/courses/${encodeURIComponent(courseID)}/analytics`,
    "GET",
    locale,
  );
  return requireResult(result, locale, "No course analytics returned from server");
}

export async function getCourseAnnouncements(courseID: string, locale: Locale): Promise<CourseAnnouncement[]> {
  const result = await authenticatedRequest<CourseAnnouncement[]>(
    `/courses/${encodeURIComponent(courseID)}/announcements`,
    "GET",
    locale,
  );
  return requireResult(result, locale, "No course announcements returned from server");
}

export async function createCourseAnnouncement(input: {
  courseID: string;
  locale: Locale;
  csrf: string;
  title: string;
  body: string;
}): Promise<CourseAnnouncement> {
  const result = await authenticatedRequest<CourseAnnouncement>(
    `/courses/${encodeURIComponent(input.courseID)}/announcements`,
    "POST",
    input.locale,
    input.csrf,
    { title: input.title, body: input.body },
  );
  return requireResult(result, input.locale, "The announcement was not created");
}
