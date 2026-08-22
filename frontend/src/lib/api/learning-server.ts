import "server-only";
import { headers } from "next/headers";
import { readJSONResponse } from "./http";
import { buildProtectedServerRequest } from "./learning-server-request";
import type {
  CourseHome,
  LearningDashboard,
  LessonReadModel,
} from "./learning";
import type { StudentCourseAccessHistoryResponse } from "./access";

async function requestProtectedRead<T>(path: string, locale: "ar" | "en"): Promise<T> {
  const requestHeaders = await headers();
  const request = buildProtectedServerRequest(path, locale, requestHeaders.get("cookie"));
  const response = await fetch(request.url, request.init);
  return readJSONResponse<T>(response);
}

export function requestLearningDashboardServer(locale: "ar" | "en"): Promise<LearningDashboard> {
  return requestProtectedRead<LearningDashboard>("/learn/dashboard", locale);
}

/**
 * The Student's Course-access records, read server-side for the Dashboard's pending summary.
 *
 * It resolves to `null` rather than throwing: the pending summary is secondary information, and a
 * Student whose Courses load correctly must still see them if this one read fails.
 */
export async function requestStudentCourseAccessServer(
  locale: "ar" | "en",
): Promise<StudentCourseAccessHistoryResponse | null> {
  try {
    return await requestProtectedRead<StudentCourseAccessHistoryResponse>("/me/course-access", locale);
  } catch {
    return null;
  }
}

export function requestCourseHomeServer(courseID: string, locale: "ar" | "en"): Promise<CourseHome> {
  return requestProtectedRead<CourseHome>(
    `/learn/courses/${encodeURIComponent(courseID)}`,
    locale,
  );
}

export function requestLessonReadModelServer(
  courseID: string,
  lessonID: string,
  locale: "ar" | "en",
): Promise<LessonReadModel> {
  return requestProtectedRead<LessonReadModel>(
    `/learn/courses/${encodeURIComponent(courseID)}/lessons/${encodeURIComponent(lessonID)}`,
    locale,
  );
}
