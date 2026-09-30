import { ProblemError } from "@/lib/api/problem";

export function isCourseNotPublishedError(error: unknown): boolean {
  if (!(error instanceof ProblemError)) return false;
  return (
    error.problem.code === "STATE_CONFLICT" &&
    (error.problem.errors ?? []).some((violation) => violation.code === "COURSE_NOT_PUBLISHED")
  );
}
