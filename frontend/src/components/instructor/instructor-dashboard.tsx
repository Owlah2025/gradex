"use client";

import Link from "next/link";
import { BarChart3, Megaphone, Pencil, Users } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { describeApiError } from "@/lib/api/api-error";
import {
  getInstructorDashboard,
  type InstructorAlert,
  type InstructorDashboard as InstructorDashboardData,
  type InstructorDashboardCourse,
} from "@/lib/api/instructor";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";
import { canManageCourseAnnouncements } from "./dashboard-course-actions";

function titleFor(course: InstructorDashboardCourse, locale: "ar" | "en", fallback: string): string {
  const title = locale === "ar" ? course.title_ar : course.title_en;
  return title.trim() || fallback;
}

function percent(value: number): string {
  return `${Math.round(value)}%`;
}

function alertCopy(alert: InstructorAlert, labels: typeof import("@/lib/i18n/dictionaries/en").en.instructor.dashboard): string {
  switch (alert.kind) {
    case "CHANGES_REQUESTED":
      return labels.changesRequested;
    case "AWAITING_REVIEW":
      return labels.awaitingReview;
    case "MEDIA_PROCESSING_FAILED":
      return labels.mediaProcessingFailed;
    case "PROFILE_CHANGES_REQUESTED":
      return labels.profileChangesRequested;
    default:
      return labels.alertsHeading;
  }
}

function CourseActions({ course, labels, locale }: { course: InstructorDashboardCourse; labels: typeof import("@/lib/i18n/dictionaries/en").en.instructor.dashboard; locale: "ar" | "en" }) {
  const base = `/${locale}/instructor/courses/${encodeURIComponent(course.course_id)}`;
  const actions = [
    { href: base, label: labels.edit, icon: Pencil },
    { href: `${base}/analytics`, label: labels.analytics, icon: BarChart3 },
    { href: `${base}/students`, label: labels.students, icon: Users },
    ...(canManageCourseAnnouncements(course)
      ? [{ href: `${base}/announcements`, label: labels.announcements, icon: Megaphone }]
      : []),
  ];
  return (
    <div className="flex flex-wrap gap-2">
      {actions.map(({ href, label, icon: Icon }, index) => (
        <Button key={href} asChild size="sm" variant={index === 0 ? "default" : "outline"}>
          <Link href={href}>
            <Icon aria-hidden />
            {label}
          </Link>
        </Button>
      ))}
    </div>
  );
}

function CourseCard({ course, locale, labels }: { course: InstructorDashboardCourse; locale: "ar" | "en"; labels: typeof import("@/lib/i18n/dictionaries/en").en.instructor.dashboard }) {
  const title = titleFor(course, locale, labels.untitled);
  return (
    <Card asChild>
      <article className="h-full">
        <CardHeader className="gap-3 border-b border-border/70">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <CardTitle className="min-w-0 text-xl leading-tight">{title}</CardTitle>
            <span className="rounded-pill bg-muted px-3 py-1 text-xs font-semibold text-foreground">
              {labels.statuses[course.lifecycle as keyof typeof labels.statuses] ?? course.lifecycle}
            </span>
          </div>
          {course.candidate_revision ? (
            <p className="text-sm text-muted-foreground">
              {labels.courseStatus}: {labels.statuses[course.candidate_revision.state as keyof typeof labels.statuses] ?? course.candidate_revision.state}
            </p>
          ) : null}
        </CardHeader>
        <CardContent className="flex h-full flex-col gap-5 pt-5">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-4 text-sm sm:grid-cols-5">
            <div>
              <dt className="text-muted-foreground">{labels.enrollments}</dt>
              <dd className="mt-1 font-display text-lg font-bold">{course.enrollments}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{labels.active7d}</dt>
              <dd className="mt-1 font-display text-lg font-bold">{course.learning_active_students_7d}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{labels.averageProgress}</dt>
              <dd className="mt-1 font-display text-lg font-bold">{percent(course.average_progress)}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{labels.completions}</dt>
              <dd className="mt-1 font-display text-lg font-bold">{course.completions}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{labels.mediaFailures}</dt>
              <dd className="mt-1 font-display text-lg font-bold">{course.media_processing_failures}</dd>
            </div>
          </dl>
          {course.latest_change_request_reason ? (
            <Alert tone="error" title={labels.changesRequested}>
              <p className="whitespace-pre-wrap">{course.latest_change_request_reason}</p>
            </Alert>
          ) : null}
          <div className="mt-auto pt-1">
            <CourseActions course={course} labels={labels} locale={locale} />
          </div>
        </CardContent>
      </article>
    </Card>
  );
}

function DashboardContent({ data, locale, labels }: { data: InstructorDashboardData; locale: "ar" | "en"; labels: typeof import("@/lib/i18n/dictionaries/en").en.instructor.dashboard }) {
  const courses = data.courses;
  return (
    <>
      <WorkspaceSection title={labels.alertsHeading} description={labels.intro}>
        {data.alerts.length === 0 ? (
          <EmptyState density="compact" title={labels.noAlerts} />
        ) : (
          <div className="grid gap-3 md:grid-cols-2">
            {data.alerts.map((alert, index) => (
              <Alert key={`${alert.kind}-${alert.course_id ?? "profile"}-${index}`} tone={alert.kind === "MEDIA_PROCESSING_FAILED" ? "error" : "info"} title={alertCopy(alert, labels)}>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  {alert.reason ? <p className="whitespace-pre-wrap">{alert.reason}</p> : <span />}
                  <Button asChild size="sm" variant="outline">
                    <Link href={alert.course_id ? `/${locale}/instructor/courses/${encodeURIComponent(alert.course_id)}` : `/${locale}/instructor/profile`}>
                      {alert.course_id ? labels.edit : labels.profile}
                    </Link>
                  </Button>
                </div>
              </Alert>
            ))}
          </div>
        )}
      </WorkspaceSection>
      <WorkspaceSection title={labels.coursesHeading}>
        {courses.length === 0 ? (
          <EmptyState title={labels.emptyTitle} description={labels.emptyBody} />
        ) : (
          <div className="grid gap-5 lg:grid-cols-2">
            {courses.map((course) => <CourseCard key={course.course_id} course={course} locale={locale} labels={labels} />)}
          </div>
        )}
      </WorkspaceSection>
    </>
  );
}

export function InstructorDashboard() {
  const { locale, t } = useLocale();
  const labels = t.instructor.dashboard;
  const [data, setData] = useState<InstructorDashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useMemo(() => async () => {
    setLoading(true);
    setError(null);
    try {
      setData(await getInstructorDashboard(locale));
    } catch (cause) {
      setError(describeApiError(cause, locale));
    } finally {
      setLoading(false);
    }
  }, [locale]);

  useEffect(() => { void load(); }, [load]);

  return (
    <WorkspacePage className="space-y-8">
      <WorkspacePageHeader title={labels.title} description={labels.intro} />
      {loading ? <LoadingState label={labels.loading} /> : null}
      {!loading && error ? <ErrorState title={labels.loadFailed} detail={error} retryLabel={labels.retry} onRetry={() => void load()} /> : null}
      {!loading && !error && data ? <DashboardContent data={data} locale={locale} labels={labels} /> : null}
    </WorkspacePage>
  );
}
