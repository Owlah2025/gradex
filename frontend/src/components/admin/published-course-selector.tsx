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
      <h2 className="text-xl font-semibold mb-1 text-gray-800">Selected Course</h2>
      <p className="text-sm text-gray-600 mb-4">
        Choose the published Course this expiry configuration and invitation apply to.
      </p>

      {loading && (
        <p className="text-sm text-gray-500" data-testid="course-access-courses-loading" aria-live="polite">
          Loading published Courses...
        </p>
      )}

      {!loading && error && (
        <div className="space-y-3" data-testid="course-access-courses-error">
          <p className="text-sm text-red-800 bg-red-50 border border-red-200 rounded-md p-3" role="alert">
            <strong>Could not load published Courses:</strong> {error}
          </p>
          <button
            type="button"
            onClick={onRetry}
            className="text-sm bg-gray-100 hover:bg-gray-200 text-gray-700 py-1 px-3 rounded-md border"
          >
            Retry loading Courses
          </button>
        </div>
      )}

      {!loading && !error && options.length === 0 && (
        <p
          className="text-sm text-gray-600 border rounded-md bg-gray-50 p-4"
          data-testid="course-access-courses-empty"
        >
          No published Courses are available yet. Publish a Course through Course review before granting access.
        </p>
      )}

      {!loading && !error && options.length > 0 && (
        <div className="space-y-3">
          <label className="block text-sm font-medium text-gray-700" htmlFor="course-access-course-select">
            Published Course
          </label>
          <select
            id="course-access-course-select"
            data-testid="course-access-course-select"
            required
            value={selectedCourseID}
            onChange={(event) => onSelect(event.target.value)}
            className="mt-1 block w-full rounded-md border-gray-300 shadow-sm border p-2 text-sm"
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
              className="rounded-md border bg-gray-50 p-3 text-sm text-gray-700 space-y-1"
              data-testid="course-access-selected-course"
            >
              <div className="font-semibold text-gray-900">{selected.title}</div>
              {selected.alternateTitle && <div dir="auto">{selected.alternateTitle}</div>}
              <div className="text-xs text-gray-600">
                {[selected.instructor, selected.subject, selected.studyYear].filter(Boolean).join(" · ")}
              </div>
              <span className="inline-block px-2 py-1 text-xs font-semibold rounded bg-green-100 text-green-800">
                PUBLISHED
              </span>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
