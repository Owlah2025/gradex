"use client";

import { useEffect, useState } from "react";
import { describeApiError } from "@/lib/api/api-error";
import { getCourseRoster, type CourseRosterPage } from "@/lib/api/catalog";
import { formatLearningExpiry } from "@/lib/formatters/learning";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  courseRosterStatusLabel,
  courseRosterViewState,
  type CourseRosterAccessStatus,
  type CourseRosterViewState,
} from "./course-roster-state";

type CourseRosterLabels = {
  title: string;
  loading: string;
  error: string;
  empty: string;
  student: string;
  accessStatus: string;
  joined: string;
  accessStarted: string;
  accessUntil: string;
  unavailableDate: string;
  previous: string;
  next: string;
  page: string;
  statuses: Record<CourseRosterAccessStatus, string>;
};

function RosterDate({ value, locale, fallback }: { value?: string; locale: "ar" | "en"; fallback: string }) {
  if (!value) return <span>{fallback}</span>;
  const formatted = formatLearningExpiry(value, locale);
  return formatted ? <time dateTime={formatted.dateTime}>{formatted.text}</time> : <span>{fallback}</span>;
}

function CourseRosterTable({ roster, locale, labels }: { roster: CourseRosterPage; locale: "ar" | "en"; labels: CourseRosterLabels }) {
  return (
    <div className="mt-3 overflow-x-auto">
      <table className="min-w-full text-start text-sm" data-testid="course-roster-table">
        <caption className="sr-only">{labels.title}</caption>
        <thead className="border-b border-slate-200 text-xs uppercase text-slate-600 dark:border-slate-700 dark:text-slate-300">
          <tr>
            <th scope="col" className="px-3 py-2 text-start">{labels.student}</th>
            <th scope="col" className="px-3 py-2 text-start">{labels.accessStatus}</th>
            <th scope="col" className="px-3 py-2 text-start">{labels.joined}</th>
            <th scope="col" className="px-3 py-2 text-start">{labels.accessStarted}</th>
            <th scope="col" className="px-3 py-2 text-start">{labels.accessUntil}</th>
          </tr>
        </thead>
        <tbody>
          {roster.items.map((student, index) => (
            <tr key={`${student.enrolled_at}-${index}`} className="border-b border-slate-100 dark:border-slate-800" data-testid="course-roster-row">
              <th scope="row" className="px-3 py-3 text-start font-medium">{student.display_name}</th>
              <td className="px-3 py-3" data-roster-status={student.access_status}>
                {courseRosterStatusLabel(student.access_status, labels.statuses)}
              </td>
              <td className="px-3 py-3"><RosterDate value={student.enrolled_at} locale={locale} fallback={labels.unavailableDate} /></td>
              <td className="px-3 py-3"><RosterDate value={student.access_started_at} locale={locale} fallback={labels.unavailableDate} /></td>
              <td className="px-3 py-3"><RosterDate value={student.access_ends_at} locale={locale} fallback={labels.unavailableDate} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CourseRosterPagination({ roster, page, loading, labels, onPrevious, onNext }: {
  roster: CourseRosterPage;
  page: number;
  loading: boolean;
  labels: CourseRosterLabels;
  onPrevious: () => void;
  onNext: () => void;
}) {
  return (
    <nav aria-label={labels.title} className="mt-4 flex items-center justify-between gap-3 text-sm">
      <button
        type="button"
        onClick={onPrevious}
        disabled={page === 1 || loading}
        className="rounded-md border border-slate-300 px-3 py-2 font-medium disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700"
      >
        {labels.previous}
      </button>
      <span aria-live="polite">{labels.page} {roster.page}</span>
      <button
        type="button"
        onClick={onNext}
        disabled={!roster.has_next || loading}
        className="rounded-md border border-slate-300 px-3 py-2 font-medium disabled:cursor-not-allowed disabled:opacity-50 dark:border-slate-700"
      >
        {labels.next}
      </button>
    </nav>
  );
}

function CourseRosterContent({ state, error, roster, page, loading, locale, labels, onPrevious, onNext }: {
  state: CourseRosterViewState;
  error: string | null;
  roster: CourseRosterPage | null;
  page: number;
  loading: boolean;
  locale: "ar" | "en";
  labels: CourseRosterLabels;
  onPrevious: () => void;
  onNext: () => void;
}) {
  if (state === "loading") return <p role="status" className="mt-3 text-sm text-slate-600 dark:text-slate-300">{labels.loading}</p>;
  if (state === "error") return <p role="alert" className="mt-3 text-sm text-red-700 dark:text-red-300">{error ?? labels.error}</p>;
  if (state === "empty") return <p className="mt-3 text-sm text-slate-600 dark:text-slate-300" data-testid="course-roster-empty">{labels.empty}</p>;
  if (!roster) return null;

  return (
    <>
      <CourseRosterTable roster={roster} locale={locale} labels={labels} />
      <CourseRosterPagination
        roster={roster}
        page={page}
        loading={loading}
        labels={labels}
        onPrevious={onPrevious}
        onNext={onNext}
      />
    </>
  );
}

export function CourseRoster({ courseID }: { courseID: string }) {
  const { locale, t } = useLocale();
  const labels = t.instructor.roster as CourseRosterLabels;
  const [page, setPage] = useState(1);
  const [roster, setRoster] = useState<CourseRosterPage | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setRoster(null);
    getCourseRoster(courseID, locale, page)
      .then((nextRoster) => {
        if (!cancelled) setRoster(nextRoster);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(describeApiError(cause, locale));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [courseID, locale, page]);

  const state = courseRosterViewState(loading, error, roster?.items.length ?? 0);
  return (
    <section aria-labelledby="course-roster-title" data-testid="course-roster">
      <h3 id="course-roster-title" className="text-lg font-semibold">{labels.title}</h3>
      <CourseRosterContent
        state={state}
        error={error}
        roster={roster}
        page={page}
        loading={loading}
        locale={locale}
        labels={labels}
        onPrevious={() => setPage((current) => Math.max(1, current - 1))}
        onNext={() => setPage((current) => current + 1)}
      />
    </section>
  );
}
