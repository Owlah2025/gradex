import "server-only";
import { headers } from "next/headers";
import { readJSONResponse } from "./http";
import { buildProtectedServerRequest } from "./learning-server-request";
import type {
  CourseHome,
  LearningDashboard,
  LessonReadModel,
} from "./learning";

async function requestProtectedRead<T>(path: string, locale: "ar" | "en"): Promise<T> {
  const requestHeaders = headers() as unknown as { get: (name: string) => string | null };
  const request = buildProtectedServerRequest(path, locale, requestHeaders.get("cookie"));
  const response = await fetch(request.url, request.init);
  return readJSONResponse<T>(response);
}

export function requestLearningDashboardServer(locale: "ar" | "en"): Promise<LearningDashboard> {
  return requestProtectedRead<LearningDashboard>("/learn/dashboard", locale);
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
