"use client";

import Link from "next/link";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { describeApiError } from "@/lib/api/api-error";
import { getCourseAnalytics, type CourseAnalytics as CourseAnalyticsData } from "@/lib/api/instructor";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { Table, TableBody, TableCell, TableContainer, TableHead, TableHeaderCell, TableRow } from "@/components/ui/table";
import { WorkspacePage, WorkspacePageHeader, WorkspaceSection } from "@/components/layout/workspace-page";

function titleFor(data: CourseAnalyticsData, locale: "ar" | "en"): string {
  return (locale === "ar" ? data.title_ar : data.title_en).trim() || "—";
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <Card>
      <CardContent className="p-5">
        <dt className="text-sm text-muted-foreground">{label}</dt>
        <dd className="mt-2 font-display text-2xl font-bold text-foreground">{value}</dd>
      </CardContent>
    </Card>
  );
}

export function CourseAnalytics({ courseID }: { courseID: string }) {
  const { locale, t } = useLocale();
  const labels = t.instructor.analytics;
  const Back = locale === "ar" ? ArrowRight : ArrowLeft;
  const [data, setData] = useState<CourseAnalyticsData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setData(await getCourseAnalytics(courseID, locale));
    } catch (cause) {
      setError(describeApiError(cause, locale));
    } finally {
      setLoading(false);
    }
  }, [courseID, locale]);

  useEffect(() => { void load(); }, [load]);

  return (
    <WorkspacePage className="space-y-8">
      <WorkspacePageHeader
        breadcrumb={<Button asChild variant="ghost" size="sm" className="-ms-3"><Link href={`/${locale}/instructor`}><Back aria-hidden />{labels.back}</Link></Button>}
        title={data ? titleFor(data, locale) : labels.title}
      />
      {loading ? <LoadingState label={labels.loading} /> : null}
      {!loading && error ? <ErrorState title={labels.loadFailed} detail={error} retryLabel={labels.retry} onRetry={() => void load()} /> : null}
      {!loading && !error && data ? (
        <>
          <dl className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
            <Metric label={labels.enrolled} value={data.enrolled} />
            <Metric label={labels.started} value={data.started} />
            <Metric label={labels.active7d} value={data.learning_active_students_7d} />
            <Metric label={labels.averageProgress} value={`${Math.round(data.average_progress)}%`} />
            <Metric label={labels.completed} value={data.completed} />
          </dl>
          <Alert tone="info" title={labels.definitionsTitle}>
            <p>{labels.watchTimeNotice}</p>
          </Alert>
          <WorkspaceSection title={labels.reachHeading} description={labels.reachDescription}>
            {data.lesson_reach.length === 0 ? (
              <EmptyState density="compact" title={labels.noLessons} />
            ) : (
              <TableContainer>
                <Table data-testid="course-analytics-reach">
                  <TableHead>
                    <TableRow>
                      <TableHeaderCell scope="col">{labels.section}</TableHeaderCell>
                      <TableHeaderCell scope="col">{labels.lesson}</TableHeaderCell>
                      <TableHeaderCell scope="col">{labels.reached}</TableHeaderCell>
                      <TableHeaderCell scope="col">{labels.completedLessons}</TableHeaderCell>
                      <TableHeaderCell scope="col">{labels.reach}</TableHeaderCell>
                      <TableHeaderCell scope="col">{labels.dropOff}</TableHeaderCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {data.lesson_reach.map((lesson) => (
                      <TableRow key={lesson.lesson_id}>
                        <TableCell className="min-w-40 text-muted-foreground"><span className="block">{locale === "ar" ? lesson.section_title_ar : lesson.section_title_en}</span></TableCell>
                        <TableHeaderCell scope="row" className="min-w-48">{locale === "ar" ? lesson.lesson_title_ar : lesson.lesson_title_en}</TableHeaderCell>
                        <TableCell>{lesson.students_reached}</TableCell>
                        <TableCell>{lesson.students_completed}</TableCell>
                        <TableCell>{Math.round(lesson.reach_percent)}%</TableCell>
                        <TableCell>{Math.round(lesson.drop_off_percent)}%</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </TableContainer>
            )}
          </WorkspaceSection>
        </>
      ) : null}
    </WorkspacePage>
  );
}
