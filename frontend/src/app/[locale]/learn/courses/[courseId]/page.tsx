import Link from "next/link";
import { ArrowLeft, ArrowRight, CheckCircle2 } from "lucide-react";
import {
  AccessUntil,
  LearningProgressSummary,
  LearningStatusBadge,
  LearningUnavailable,
  MaterialsInline,
} from "@/components/learning/learning-views";
import { CourseCurriculum } from "@/components/learning/course-curriculum";
import { courseCurriculum, courseIsComplete } from "@/components/learning/curriculum-model";
import { requestCourseHomeServer } from "@/lib/api/learning-server";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningShell } from "@/components/learning/learning-shell";
import { ReportTargetActions } from "@/components/learning/report-content-dialog";
import { courseReportTargets } from "@/components/learning/report-targets";
import { reportLabels } from "@/components/learning/report-labels";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  accessLabels,
  curriculumLabels,
  learningStatusDetail,
  learningStatusLabel,
  materialsLabels,
  progressLabels,
  shellLabels,
  unavailableLabels,
} from "@/components/learning/learning-label-sets";

export const dynamic = "force-dynamic";
export const revalidate = 0;

/**
 * The Course a Student is inside: how far they have got, and everything it contains.
 *
 * The contents are the page. The header states the Course, what its access means in words, and the
 * one progress figure the server computes — then gets out of the way, because a Student arriving
 * here is choosing a Lesson, not reading a summary.
 */
export default async function CourseHomePage({ params }: { params: Promise<{ locale: string; courseId: string }> }) {
  const { locale: requestedLocale, courseId } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  const shell = shellLabels(dictionary);
  const Backward = locale === "ar" ? ArrowRight : ArrowLeft;
  try {
    const course = await requestCourseHomeServer(courseId, locale);
    const sections = courseCurriculum(course.sections);
    const complete = courseIsComplete(course.progress);

    // Materials are composed here, on the server, and handed to the contents already built. The
    // download paths, the file names and the decision that access even permits a download all stay
    // on this side of the boundary; the contents component receives a subtree, never a material.
    const materialsByLesson =
      course.learning_status === "active"
        ? Object.fromEntries(
            course.sections.flatMap((section) =>
              section.lessons
                .filter((lesson) => lesson.resources.length > 0 || lesson.lab_materials.length > 0)
                .map((lesson) => [
                  lesson.lesson_id,
                  <MaterialsInline
                    key={lesson.lesson_id}
                    resources={lesson.resources}
                    labMaterials={lesson.lab_materials}
                    labels={materialsLabels(dictionary.learning)}
                    locale={locale}
                  />,
                ]),
            ),
          )
        : undefined;

    return (
      <LearningShell locale={locale} dir={locale === "ar" ? "rtl" : "ltr"} labels={shell}>
        <div className="mx-auto max-w-container px-5 py-8 sm:px-6 sm:py-10">
          <Button asChild variant="ghost" size="sm" className="-ms-3">
            <Link href={`/${locale}/learn/dashboard`}>
              <Backward aria-hidden />
              {dictionary.learning.myCourses}
            </Link>
          </Button>

          <header className="mt-3 border-b border-border pb-6">
            <p className="font-display text-sm font-bold uppercase tracking-wide text-muted-foreground">
              {dictionary.learning.courseHome}
            </p>
            <h1 className="mt-2 font-display text-2xl font-bold text-foreground sm:text-3xl">
              {course.title}
            </h1>
            <div className="mt-4">
              <LearningStatusBadge
                status={course.learning_status}
                label={learningStatusLabel(course.learning_status, dictionary.learning)}
                detail={learningStatusDetail(course.learning_status, dictionary.learning)}
              />
            </div>
            <div className="mt-4 max-w-sm">
              <LearningProgressSummary
                progress={course.progress}
                labels={progressLabels(dictionary.learning)}
                locale={locale}
              />
              <AccessUntil
                className="mt-2"
                expiresAt={course.expires_at}
                labels={accessLabels(dictionary.learning)}
                locale={locale}
              />
            </div>
          </header>

          {/* The only completion state this product can honestly show: every Lesson the server
              counts is done. No certificate, no score, no badge — none of those exist. */}
          {complete ? (
            <Card data-testid="course-complete" className="mt-6 flex items-start gap-3 p-5">
              <CheckCircle2 aria-hidden className="mt-0.5 size-5 shrink-0 text-primary" />
              <div>
                <h2 className="font-display text-base font-bold text-foreground">
                  {dictionary.learning.courseCompleteTitle}
                </h2>
                <p className="mt-1 text-sm text-muted-foreground">
                  {dictionary.learning.courseCompleteBody}
                </p>
              </div>
            </Card>
          ) : null}

          {/* The contents are a landmark named by the region, and the sections are the page's own
              second level — so the disclosure triggers are `h2` and there is no separate heading
              above them competing for the same rank. */}
          <section aria-label={dictionary.learning.courseOutline} className="mt-8">
            <p className="text-xs text-muted-foreground">{dictionary.learning.completionAutomatic}</p>
            <CourseCurriculum
              className="mt-4"
              courseID={course.course_id}
              locale={locale}
              sections={sections}
              labels={curriculumLabels(dictionary.learning)}
              headingLevel="h2"
              materialsByLesson={materialsByLesson}
            />
          </section>

          {/* Offered only when this active read issued a COURSE context (D-065). With no target the
              client component is not mounted at all, so no report copy enters the payload either. */}
          {courseReportTargets(course).length > 0 ? (
            <div className="mt-8">
              <ReportTargetActions
                targets={courseReportTargets(course)}
                scopePrefix={course.course_id}
                locale={locale}
                labels={reportLabels(dictionary.learning)}
              />
            </div>
          ) : null}
        </div>
      </LearningShell>
    );
  } catch {
    return (
      <LearningShell locale={locale} dir={locale === "ar" ? "rtl" : "ltr"} labels={shell}>
        <div className="mx-auto max-w-3xl px-5 py-10 sm:px-6">
          <LearningUnavailable labels={unavailableLabels(dictionary.learning)} />
        </div>
      </LearningShell>
    );
  }
}
