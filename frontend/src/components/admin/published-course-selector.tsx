"use client";

import {
  findPublishedCourse,
  publishedCourseOptionLabel,
  type PublishedCourseOption,
} from "./published-courses";

type PublishedCourseSelectorProps = {
  options: PublishedCourseOption[];
  loading: boolean;
  error: string | null;
  selectedCourseID: string;
  onSelect: (courseID: string) => void;
  onRetry: () => void;
};

/**
 * One Course context for the whole Course Access journey. Expiry
 * configuration and invitation creation both act on this selection, so the
 * two operations can never silently address different Courses.
 */
export function PublishedCourseSelector({
  options,
  loading,
  error,
  selectedCourseID,
  onSelect,
  onRetry,
}: PublishedCourseSelectorProps) {
  const selected = findPublishedCourse(options, selectedCourseID);

  return (
    <section className="bg-white p-6 rounded-lg border shadow-sm" data-testid="course-access-course-picker">
      <h2 className="text-xl font-semibold mb-1 text-foreground">Selected Course</h2>
      <p className="text-sm text-muted-foreground mb-4">
        Choose the published Course this expiry configuration and invitation apply to.
      </p>

      {loading && (
        <p className="text-sm text-muted-foreground" data-testid="course-access-courses-loading" aria-live="polite">
          Loading published Courses...
        </p>
      )}

      {!loading && error && (
        <div className="space-y-3" data-testid="course-access-courses-error">
          <p className="text-sm text-destructive bg-destructive border border-destructive/25 rounded-md p-3" role="alert">
            <strong>Could not load published Courses:</strong> {error}
          </p>
          <button
            type="button"
            onClick={onRetry}
            className="text-sm bg-muted hover:bg-accent text-muted-foreground py-1 px-3 rounded-md border"
          >
            Retry loading Courses
          </button>
        </div>
      )}

      {!loading && !error && options.length === 0 && (
        <p
          className="text-sm text-muted-foreground border rounded-md bg-muted p-4"
          data-testid="course-access-courses-empty"
        >
          No published Courses are available yet. Publish a Course through Course review before granting access.
        </p>
      )}

      {!loading && !error && options.length > 0 && (
        <div className="space-y-3">
          <label className="block text-sm font-medium text-muted-foreground" htmlFor="course-access-course-select">
            Published Course
          </label>
          <select
            id="course-access-course-select"
            data-testid="course-access-course-select"
            required
            value={selectedCourseID}
            onChange={(event) => onSelect(event.target.value)}
            className="mt-1 block w-full rounded-md border-border shadow-sm border p-2 text-sm"
          >
            <option value="">Select a published Course...</option>
            {options.map((option) => (
              <option key={option.id} value={option.id}>
                {publishedCourseOptionLabel(option)}
              </option>
            ))}
          </select>

          {selected && (
            <div
              className="rounded-md border bg-muted p-3 text-sm text-muted-foreground space-y-1"
              data-testid="course-access-selected-course"
            >
              <div className="font-semibold text-foreground">{selected.title}</div>
              {selected.alternateTitle && <div dir="auto">{selected.alternateTitle}</div>}
              <div className="text-xs text-muted-foreground">
                {[selected.instructor, selected.subject, selected.studyYear].filter(Boolean).join(" · ")}
              </div>
              <span className="inline-flex items-center rounded-pill bg-gx-success-soft px-2.5 py-1 font-display text-xs font-bold leading-none text-gx-success-strong">
                PUBLISHED
              </span>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
