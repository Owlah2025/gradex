export const progressReportIntervalMilliseconds = 15_000;

export type ProgressReport = {
  position_seconds: number;
  asset_version_id: string;
};

export function progressPath(lessonID: string): string {
  return `/api/v1/learn/lessons/${encodeURIComponent(lessonID)}/progress`;
}

export function progressReport(positionSeconds: number, assetVersionID: string): ProgressReport | null {
  if (!Number.isFinite(positionSeconds) || positionSeconds < 0 || assetVersionID === "") return null;
  return { position_seconds: positionSeconds, asset_version_id: assetVersionID };
}

/**
 * The canonical state the server returns from an accepted Progress write.
 *
 * The write used to answer 204, so a successful report told the browser only
 * that it had been accepted. Every surface showing completion or a course
 * percentage therefore kept rendering whatever it had at page load until the
 * Student reloaded the page. This is the payload that removes the reload.
 *
 * `course_progress` is optional because the confirming read can fail after the
 * write has already committed. Absent means "no new aggregate", never "zero".
 */
export type ConfirmedLessonProgress = {
  position_seconds: number;
  completed: boolean;
};

export type ConfirmedCourseProgress = {
  completed_lessons: number;
  total_lessons: number;
  percent: number;
};

export type ProgressConfirmation = {
  lessonID: string;
  lesson: ConfirmedLessonProgress;
  course: ConfirmedCourseProgress | null;
};

/**
 * Reads a confirmation out of a response body.
 *
 * The body is validated rather than cast. It crosses the network, and a
 * malformed or truncated response must degrade to "no update" — the visible
 * progress then stays where it was, which is stale but never wrong.
 */
export function progressConfirmation(lessonID: string, body: unknown): ProgressConfirmation | null {
  if (typeof body !== "object" || body === null) return null;
  const payload = body as Record<string, unknown>;
  const lesson = payload.lesson_progress;
  if (typeof lesson !== "object" || lesson === null) return null;
  const lessonFields = lesson as Record<string, unknown>;
  if (
    typeof lessonFields.position_seconds !== "number" ||
    !Number.isFinite(lessonFields.position_seconds) ||
    typeof lessonFields.completed !== "boolean"
  ) {
    return null;
  }
  return {
    lessonID,
    lesson: {
      position_seconds: lessonFields.position_seconds,
      completed: lessonFields.completed,
    },
    course: courseProgressOf(payload.course_progress),
  };
}

function courseProgressOf(value: unknown): ConfirmedCourseProgress | null {
  if (typeof value !== "object" || value === null) return null;
  const fields = value as Record<string, unknown>;
  const numbers = [fields.completed_lessons, fields.total_lessons, fields.percent];
  if (numbers.some((entry) => typeof entry !== "number" || !Number.isFinite(entry))) return null;
  return {
    completed_lessons: fields.completed_lessons as number,
    total_lessons: fields.total_lessons as number,
    percent: fields.percent as number,
  };
}
