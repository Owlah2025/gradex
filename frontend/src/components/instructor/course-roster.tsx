"use client";

import { useEffect, useState } from "react";
import { describeApiError } from "@/lib/api/api-error";
import { getCourseRoster, type CourseRosterPage } from "@/lib/api/catalog";
import { formatLearningExpiry } from "@/lib/formatters/learning";
import { useLocale } from "@/lib/i18n/locale-provider";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { WorkspaceSection } from "@/components/layout/workspace-page";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
  TableSkeletonRows,
} from "@/components/ui/table";
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

const COLUMN_COUNT = 5;

function RosterDate({ value, locale, fallback }: { value?: string; locale: "ar" | "en"; fallback: string }) {
  if (!value) return <span>{fallback}</span>;
  const formatted = formatLearningExpiry(value, locale);
  return formatted ? <time dateTime={formatted.dateTime}>{formatted.text}</time> : <span>{fallback}</span>;
}

function RosterTableHead({ labels }: { labels: CourseRosterLabels }) {
  return (
    <TableHead>
      <TableRow>
        <TableHeaderCell scope="col">{labels.student}</TableHeaderCell>
        <TableHeaderCell scope="col">{labels.accessStatus}</TableHeaderCell>
        <TableHeaderCell scope="col">{labels.joined}</TableHeaderCell>
        <TableHeaderCell scope="col">{labels.accessStarted}</TableHeaderCell>
        <TableHeaderCell scope="col">{labels.accessUntil}</TableHeaderCell>
      </TableRow>
    </TableHead>
  );
}

function CourseRosterTable({ roster, locale, labels }: { roster: CourseRosterPage; locale: "ar" | "en"; labels: CourseRosterLabels }) {
  return (
    <TableContainer>
      <Table data-testid="course-roster-table">
        <TableCaption>{labels.title}</TableCaption>
        <RosterTableHead labels={labels} />
        <TableBody>
          {roster.items.map((student, index) => (
            <TableRow key={`${student.enrolled_at}-${index}`} data-testid="course-roster-row">
              <TableHeaderCell scope="row" className="min-w-40">
                {student.display_name}
              </TableHeaderCell>
              {/* The access status is a word, not a colour. The wire enum stays on the cell as a
                  data attribute for support and tests; it is never what the Instructor reads. */}
              <TableCell data-roster-status={student.access_status}>
                {courseRosterStatusLabel(student.access_status, labels.statuses)}
              </TableCell>
              <TableCell className="whitespace-nowrap text-muted-foreground">
                <RosterDate value={student.enrolled_at} locale={locale} fallback={labels.unavailableDate} />
              </TableCell>
              <TableCell className="whitespace-nowrap text-muted-foreground">
                <RosterDate value={student.access_started_at} locale={locale} fallback={labels.unavailableDate} />
              </TableCell>
              <TableCell className="whitespace-nowrap text-muted-foreground">
                <RosterDate value={student.access_ends_at} locale={locale} fallback={labels.unavailableDate} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
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
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onPrevious}
        disabled={page === 1 || loading}
      >
        {labels.previous}
      </Button>
      <span aria-live="polite" className="text-muted-foreground">
        {labels.page} {roster.page}
      </span>
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={onNext}
        disabled={!roster.has_next || loading}
      >
        {labels.next}
      </Button>
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
  if (state === "loading") {
    // The header row is rendered above placeholder rows so the columns are already measured when
    // the page lands, instead of the roster appearing by pushing the rest of the screen down.
    return (
      <>
        <LoadingState visuallyHidden label={labels.loading} />
        <TableContainer>
          <Table>
            <TableCaption>{labels.title}</TableCaption>
            <RosterTableHead labels={labels} />
            <TableSkeletonRows columns={COLUMN_COUNT} rows={3} />
          </Table>
        </TableContainer>
      </>
    );
  }
  if (state === "error") return <ErrorState title={labels.error} detail={error} />;
  if (state === "empty") {
    return (
      <div data-testid="course-roster-empty">
        <EmptyState density="compact" title={labels.empty} />
      </div>
    );
  }
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
    <WorkspaceSection title={labels.title} headingLevel="h3" testID="course-roster">
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
    </WorkspaceSection>
  );
}
