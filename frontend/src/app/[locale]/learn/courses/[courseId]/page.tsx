import Link from "next/link";
import { AccessUntil, CourseOutline, LearningProgressSummary, LearningStatusBadge, LearningUnavailable } from "@/components/learning/learning-views";
import { requestCourseHomeServer } from "@/lib/api/learning-server";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningLocaleToggle } from "@/components/learning/learning-locale-toggle";
import { ReportTargetActions } from "@/components/learning/report-content-dialog";
import { courseReportTargets } from "@/components/learning/report-targets";
import { reportLabels } from "@/components/learning/report-labels";
import {
  accessLabels,
  learningStatusLabel,
  outlineLabels,
  progressLabels,
  unavailableLabels,
} from "@/components/learning/learning-label-sets";

export const dynamic = "force-dynamic";
export const revalidate = 0;

export default async function CourseHomePage({ params }: { params: Promise<{ locale: string; courseId: string }> }) {
  const { locale: requestedLocale, courseId } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  try {
    const course = await requestCourseHomeServer(courseId, locale);
    return (
      <main dir={locale === "ar" ? "rtl" : "ltr"} className="mx-auto min-h-screen max-w-6xl px-5 py-10 sm:px-6">
        <header className="mb-8">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-semibold text-muted-foreground">{dictionary.learning.courseHome}</p>
              <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
                <h1 className="font-display text-4xl font-bold text-foreground">{course.title}</h1>
                <LearningStatusBadge status={course.learning_status} label={learningStatusLabel(course.learning_status, dictionary.learning)} />
              </div>
            </div>
            <LearningLocaleToggle locale={locale} label={dictionary.meta.switchToAria} />
          </div>
          <div className="mt-5 space-y-2">
            <LearningProgressSummary progress={course.progress} labels={progressLabels(dictionary.learning)} locale={locale} />
            <AccessUntil expiresAt={course.expires_at} labels={accessLabels(dictionary.learning)} locale={locale} />
          </div>
        </header>
        <CourseOutline
          courseId={course.course_id}
          learningStatus={course.learning_status}
          sections={course.sections}
          locale={locale}
          labels={outlineLabels(dictionary.learning)}
        />
        {/* Offered only when this active read issued a COURSE context (D-065). With no target the
            client component is not mounted at all, so no report copy enters the payload either. */}
        {courseReportTargets(course).length > 0 ? (
          <div className="mt-6">
            <ReportTargetActions
              targets={courseReportTargets(course)}
              scopePrefix={course.course_id}
              locale={locale}
              labels={reportLabels(dictionary.learning)}
            />
          </div>
        ) : null}
        <Link
          href={`/${locale}/learn/dashboard`}
          className="mt-8 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {dictionary.learning.dashboardTitle}
        </Link>
      </main>
    );
  } catch {
    return (
      <main dir={locale === "ar" ? "rtl" : "ltr"} className="mx-auto min-h-screen max-w-3xl px-5 py-10 sm:px-6">
        <div className="space-y-5">
          <div className="flex justify-end"><LearningLocaleToggle locale={locale} label={dictionary.meta.switchToAria} /></div>
          <LearningUnavailable labels={unavailableLabels(dictionary.learning)} />
        </div>
      </main>
    );
  }
}
