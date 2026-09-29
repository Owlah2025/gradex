"use client";

import { Info, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  getAdminMetricsOverview,
  listAdminMetricCourses,
  listAdminMetricInstructors,
  metricByKey,
  metricSubjectDemand,
  defaultAdminCourseMetricsFilters,
  defaultAdminInstructorMetricsFilters,
  type AdminMetric,
  type AdminMetricsOverview,
  type AdminMetricsPageFilters,
  type AdminMetricsPage,
  type AdminCourseMetric,
  type AdminInstructorMetric,
} from "@/lib/api/admin-metrics";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@/components/ui/table";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";

const groupDefinitions = [
  {
    key: "students",
    metricKeys: [
      "students.total",
      "students.active",
      "students.pending_verification",
      "students.suspended",
      "registrations.new_7d",
      "registrations.new_30d",
    ],
  },
  {
    key: "learning",
    metricKeys: [
      "signed_in_activity.today",
      "signed_in_activity.7d",
      "signed_in_activity.30d",
      "learning_activity.7d",
      "learning_activity.30d",
      "students.started_lessons",
      "learning.average_course_progress",
      "learning.completions",
    ],
  },
  {
    key: "courses",
    metricKeys: [
      "courses.draft",
      "courses.pending_review",
      "courses.revisions_pending_review",
      "courses.changes_requested",
      "courses.published",
      "courses.delisted",
      "courses.archived",
      "enrollments.total",
      "enrollments.new_30d",
    ],
  },
  {
    key: "instructors",
    metricKeys: ["instructors.active", "instructors.with_published_course"],
  },
  {
    key: "accessCommerce",
    metricKeys: [
      "purchase_requests.waiting_payment",
      "purchase_requests.invitation_created",
      "purchase_requests.access_granted",
      "purchase_requests.cancelled",
      "access_invitations.pending_student_acceptance",
      "access_invitations.pending_admin_approval",
      "access_invitations.approved",
      "access_invitations.rejected",
      "access_invitations.cancelled",
      "entitlements.manual_invitation.active",
      "entitlements.manual_invitation.revoked",
      "entitlements.purchase_request.active",
      "entitlements.purchase_request.revoked",
      "entitlements.bundle_purchase.active",
      "entitlements.bundle_purchase.revoked",
    ],
  },
] as const;

export function AdminAnalytics() {
  const { locale, t } = useLocale();
  const copy = t.adminAnalytics;
  const [overview, setOverview] = useState<AdminMetricsOverview | null>(null);
  const [courses, setCourses] = useState<AdminMetricsPage<AdminCourseMetric> | null>(null);
  const [instructors, setInstructors] = useState<AdminMetricsPage<AdminInstructorMetric> | null>(null);
  const [courseFilters, setCourseFilters] = useState<AdminMetricsPageFilters>(defaultAdminCourseMetricsFilters);
  const [instructorFilters, setInstructorFilters] = useState<AdminMetricsPageFilters>(defaultAdminInstructorMetricsFilters);
  const [overviewError, setOverviewError] = useState<string | null>(null);
  const [coursesError, setCoursesError] = useState<string | null>(null);
  const [instructorsError, setInstructorsError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  const loadOverview = useCallback(async () => {
    setOverview(null);
    setOverviewError(null);
    try {
      setOverview(await getAdminMetricsOverview(locale));
    } catch (reason) {
      setOverviewError(describeApiError(reason, locale));
    }
  }, [locale]);

  const loadCourses = useCallback(async () => {
    setCourses(null);
    setCoursesError(null);
    try {
      setCourses(await listAdminMetricCourses(locale, courseFilters));
    } catch (reason) {
      setCoursesError(describeApiError(reason, locale));
    }
  }, [courseFilters, locale]);

  const loadInstructors = useCallback(async () => {
    setInstructors(null);
    setInstructorsError(null);
    try {
      setInstructors(await listAdminMetricInstructors(locale, instructorFilters));
    } catch (reason) {
      setInstructorsError(describeApiError(reason, locale));
    }
  }, [instructorFilters, locale]);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview, attempt]);

  useEffect(() => {
    void loadCourses();
  }, [loadCourses, attempt]);

  useEffect(() => {
    void loadInstructors();
  }, [loadInstructors, attempt]);

  const demand = useMemo(() => (overview ? metricSubjectDemand(overview.metrics) : []), [overview]);

  return (
    <WorkspacePage>
      <WorkspacePageHeader
        title={copy.title}
        description={copy.description}
        actions={
          <Button type="button" variant="outline" onClick={() => setAttempt((value) => value + 1)}>
            <RefreshCw aria-hidden />
            {copy.refresh}
          </Button>
        }
      />

      {overviewError ? (
        <div className="mt-8">
          <ErrorState title={copy.loadFailed} detail={overviewError} retryLabel={copy.retry} onRetry={() => setAttempt((value) => value + 1)} />
        </div>
      ) : overview ? (
        <>
          <div className="mt-8 grid gap-6">
            {groupDefinitions.map((group) => (
              <MetricGroup key={group.key} group={group} metrics={overview.metrics} copy={copy} locale={locale} />
            ))}
          </div>
          <DemandPanel demand={demand} copy={copy} locale={locale} />
        </>
      ) : (
        <LoadingState label={copy.loading} />
      )}

      <WorkspaceSection title={copy.courses.title} description={copy.courses.description} testID="admin-analytics-courses">
        {coursesError ? (
          <ErrorState title={copy.loadFailed} detail={coursesError} retryLabel={copy.retry} onRetry={() => setAttempt((value) => value + 1)} />
        ) : courses ? (
          <CourseMetricsTable
            page={courses}
            filters={courseFilters}
            onFiltersChange={setCourseFilters}
            copy={copy}
            locale={locale}
          />
        ) : (
          <LoadingState label={copy.loading} />
        )}
      </WorkspaceSection>

      <WorkspaceSection title={copy.instructors.title} description={copy.instructors.description} testID="admin-analytics-instructors">
        {instructorsError ? (
          <ErrorState title={copy.loadFailed} detail={instructorsError} retryLabel={copy.retry} onRetry={() => setAttempt((value) => value + 1)} />
        ) : instructors ? (
          <InstructorMetricsTable
            page={instructors}
            filters={instructorFilters}
            onFiltersChange={setInstructorFilters}
            copy={copy}
            locale={locale}
          />
        ) : (
          <LoadingState label={copy.loading} />
        )}
      </WorkspaceSection>
    </WorkspacePage>
  );
}

function MetricGroup({
  group,
  metrics,
  copy,
  locale,
}: {
  group: (typeof groupDefinitions)[number];
  metrics: AdminMetric[];
  copy: ReturnType<typeof useLocale>["t"]["adminAnalytics"];
  locale: "ar" | "en";
}) {
  const sectionLabel = copy.sections[group.key as keyof typeof copy.sections];
  const visibleMetrics = group.metricKeys
    .map((key) => metricByKey(metrics, key))
    .filter((metric): metric is AdminMetric => metric !== undefined);
  if (visibleMetrics.length === 0) return null;
  return (
    <section aria-labelledby={`analytics-${group.key}`}>
      <div className="mb-3 flex items-end justify-between gap-3">
        <h2 id={`analytics-${group.key}`} className="font-display text-lg font-bold text-foreground">
          {sectionLabel}
        </h2>
        <span className="text-xs uppercase tracking-[0.18em] text-muted-foreground">{visibleMetrics.length}</span>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {visibleMetrics.map((metric) => (
          <MetricTile key={metric.key} metric={metric} copy={copy} locale={locale} />
        ))}
      </div>
    </section>
  );
}

function MetricTile({
  metric,
  copy,
  locale,
}: {
  metric: AdminMetric;
  copy: ReturnType<typeof useLocale>["t"]["adminAnalytics"];
  locale: "ar" | "en";
}) {
  const [open, setOpen] = useState(false);
  const label = copy.metricLabels[metric.key as keyof typeof copy.metricLabels] ?? metric.key;
  const definition = copy.definitions[metric.definition_key as keyof typeof copy.definitions] ?? metric.definition_key;
  const value = Array.isArray(metric.value)
    ? metric.value.length.toLocaleString(locale)
    : metric.key === "learning.average_course_progress"
      ? `${metric.value.toFixed(1)}%`
      : metric.value.toLocaleString(locale);
  const definitionID = `metric-definition-${metric.key.replaceAll(".", "-")}`;
  return (
    <Card className="min-h-36">
      <CardHeader className="flex-row items-start justify-between gap-3 pb-2">
        <CardTitle className="text-sm font-semibold leading-5 text-muted-foreground">{label}</CardTitle>
        <button
          type="button"
          aria-label={open ? copy.hideDefinition : copy.showDefinition}
          aria-expanded={open}
          aria-controls={definitionID}
          onClick={() => setOpen((value) => !value)}
          className="rounded-full p-1 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          <Info className="size-4" aria-hidden />
        </button>
      </CardHeader>
      <CardContent>
        <p className="font-display text-2xl font-bold tabular-nums text-foreground">{value}</p>
        {open ? (
          <p id={definitionID} className="mt-3 border-t border-border pt-3 text-xs leading-5 text-muted-foreground">
            {definition}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function DemandPanel({
  demand,
  copy,
  locale,
}: {
  demand: ReturnType<typeof metricSubjectDemand>;
  copy: ReturnType<typeof useLocale>["t"]["adminAnalytics"];
  locale: "ar" | "en";
}) {
  return (
    <WorkspaceSection title={copy.metricLabels["subject_demand.top"]} description={copy.definitions["subject_demand.top"]}>
      {demand.length === 0 ? (
        <EmptyState density="compact" title={copy.demand.empty} />
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
          {demand.map((subject) => (
            <Card key={subject.subject_id}>
              <CardContent className="p-5">
                <p className="font-semibold text-foreground">{subject.title}</p>
                <p className="mt-2 text-2xl font-bold tabular-nums text-foreground">{subject.students.toLocaleString(locale)}</p>
                <p className="text-xs text-muted-foreground">{copy.demand.students}</p>
                <Badge className="mt-3" variant={subject.served ? "success" : "neutral"}>
                  {subject.served ? copy.demand.served : copy.demand.unserved}
                </Badge>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </WorkspaceSection>
  );
}

function CourseMetricsTable({
  page,
  filters,
  onFiltersChange,
  copy,
  locale,
}: {
  page: AdminMetricsPage<AdminCourseMetric>;
  filters: AdminMetricsPageFilters;
  onFiltersChange: (filters: AdminMetricsPageFilters) => void;
  copy: ReturnType<typeof useLocale>["t"]["adminAnalytics"];
  locale: "ar" | "en";
}) {
  if (page.items.length === 0) return <EmptyState density="compact" title={copy.courses.empty} />;
  return (
    <>
      <TableContainer>
        <Table>
          <TableCaption>{copy.courses.tableCaption}</TableCaption>
          <TableHead>
            <TableRow>
              <SortableHeader label={copy.courses.course} column="title" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.instructor} column="instructor" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.lifecycle} column="lifecycle" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.enrolled} column="enrolled" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.started} column="started" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.active7d} column="learning_active_7d" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.progress} column="average_progress" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.courses.completed} column="completed" filters={filters} onChange={onFiltersChange} scope="col" />
            </TableRow>
          </TableHead>
          <TableBody>
            {page.items.map((course) => (
              <TableRow key={course.id}>
                <TableCell className="font-semibold">{course.title}</TableCell>
                <TableCell>{course.instructor}</TableCell>
                <TableCell><Badge variant="neutral">{copy.lifecycle[course.lifecycle as keyof typeof copy.lifecycle] ?? course.lifecycle}</Badge></TableCell>
                <TableCell className="tabular-nums">{course.enrolled.toLocaleString(locale)}</TableCell>
                <TableCell className="tabular-nums">{course.started.toLocaleString(locale)}</TableCell>
                <TableCell className="tabular-nums">{course.learning_active_7d.toLocaleString(locale)}</TableCell>
                <TableCell className="tabular-nums">{course.average_progress.toFixed(1)}%</TableCell>
                <TableCell className="tabular-nums">{course.completed.toLocaleString(locale)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <Pagination page={page} filters={filters} onFiltersChange={onFiltersChange} previous={copy.courses.previous} next={copy.courses.next} pageLabel={copy.courses.page} paginationLabel={copy.courses.pagination} />
    </>
  );
}

function InstructorMetricsTable({
  page,
  filters,
  onFiltersChange,
  copy,
  locale,
}: {
  page: AdminMetricsPage<AdminInstructorMetric>;
  filters: AdminMetricsPageFilters;
  onFiltersChange: (filters: AdminMetricsPageFilters) => void;
  copy: ReturnType<typeof useLocale>["t"]["adminAnalytics"];
  locale: "ar" | "en";
}) {
  if (page.items.length === 0) return <EmptyState density="compact" title={copy.instructors.empty} />;
  return (
    <>
      <TableContainer>
        <Table>
          <TableCaption>{copy.instructors.tableCaption}</TableCaption>
          <TableHead>
            <TableRow>
              <SortableHeader label={copy.instructors.instructor} column="name" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.instructors.publishedCourses} column="published_courses" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.instructors.enrollments} column="total_enrollments" filters={filters} onChange={onFiltersChange} scope="col" />
              <SortableHeader label={copy.instructors.active7d} column="learning_active_students_7d" filters={filters} onChange={onFiltersChange} scope="col" />
            </TableRow>
          </TableHead>
          <TableBody>
            {page.items.map((instructor) => (
              <TableRow key={instructor.id}>
                <TableCell className="font-semibold">{instructor.name}</TableCell>
                <TableCell className="tabular-nums">{instructor.published_courses.toLocaleString(locale)}</TableCell>
                <TableCell className="tabular-nums">{instructor.total_enrollments.toLocaleString(locale)}</TableCell>
                <TableCell className="tabular-nums">{instructor.learning_active_students_7d.toLocaleString(locale)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>
      <Pagination page={page} filters={filters} onFiltersChange={onFiltersChange} previous={copy.instructors.previous} next={copy.instructors.next} pageLabel={copy.instructors.page} paginationLabel={copy.instructors.pagination} />
    </>
  );
}

function SortableHeader({
  label,
  column,
  filters,
  onChange,
  scope,
}: {
  label: string;
  column: string;
  filters: AdminMetricsPageFilters;
  onChange: (filters: AdminMetricsPageFilters) => void;
  scope: "col" | "row";
}) {
  const active = filters.sort === column;
  const direction = filters.direction ?? "asc";
  return (
    <TableHeaderCell scope={scope} aria-sort={active ? (direction === "asc" ? "ascending" : "descending") : "none"}>
      <button
        type="button"
        className="rounded-sm text-start hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        onClick={() => onChange({ ...filters, sort: column, direction: active && direction === "asc" ? "desc" : "asc", page: 1 })}
      >
        {label}
        <span className="ms-1 text-[10px]" aria-hidden>{active ? (direction === "asc" ? "↑" : "↓") : "↕"}</span>
      </button>
    </TableHeaderCell>
  );
}

function Pagination({
  page,
  filters,
  onFiltersChange,
  previous,
  next,
  pageLabel,
  paginationLabel,
}: {
  page: { page: number; limit: number; has_more: boolean };
  filters: AdminMetricsPageFilters;
  onFiltersChange: (filters: AdminMetricsPageFilters) => void;
  previous: string;
  next: string;
  pageLabel: string;
  paginationLabel: string;
}) {
  return (
    <nav aria-label={paginationLabel} className="mt-4 flex items-center justify-between gap-3">
      <Button type="button" variant="outline" size="sm" disabled={page.page <= 1} onClick={() => onFiltersChange({ ...filters, page: page.page - 1 })}>
        {previous}
      </Button>
      <span className="text-sm text-muted-foreground">{pageLabel} {page.page}</span>
      <Button type="button" variant="outline" size="sm" disabled={!page.has_more} onClick={() => onFiltersChange({ ...filters, page: page.page + 1 })}>
        {next}
      </Button>
    </nav>
  );
}
