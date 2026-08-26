import Link from "next/link";
import { ArrowLeft, ArrowRight, CheckCircle2, Circle, CirclePlay } from "lucide-react";
import { LessonPlayer } from "@/components/learning/lesson-player";
import { lessonPlaybackPlan } from "@/components/learning/lesson-state";
import {
  AccessUntil,
  LearningStatusBadge,
  LearningUnavailable,
  LessonMaterials,
  LessonNavigation,
} from "@/components/learning/learning-views";
import { CurriculumSheet, CurriculumSidebar } from "@/components/learning/lesson-curriculum-panel";
import { courseCurriculum, lessonState, type CurriculumSection } from "@/components/learning/curriculum-model";
import { requestCourseHomeServer, requestLessonReadModelServer } from "@/lib/api/learning-server";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningShell } from "@/components/learning/learning-shell";
import { ReportTargetActions } from "@/components/learning/report-content-dialog";
import { lessonReportTargets } from "@/components/learning/report-targets";
import { reportLabels } from "@/components/learning/report-labels";
import {
  accessLabels,
  curriculumLabels,
  learningStatusDetail,
  learningStatusLabel,
  materialsLabels,
  navigationLabels,
  shellLabels,
  unavailableLabels,
} from "@/components/learning/learning-label-sets";
import { Button } from "@/components/ui/button";

export const dynamic = "force-dynamic";
export const revalidate = 0;

export default async function LessonPage({
  params,
}: {
  params: Promise<{ locale: "ar" | "en"; courseId: string; lessonId: string }>;
}) {
  const { locale: requestedLocale, courseId, lessonId } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  // No dictionary crosses this boundary. The status label can only be resolved once the read model
  // says which status it is, and passing both strings so the child could choose is exactly the
  // defect (GAP-04) — so the child selects its own dictionary from the locale and narrows after the
  // fetch instead.
  return <LessonContent courseID={courseId} lessonID={lessonId} locale={locale} />;
}

/**
 * The Course's contents, read alongside the Lesson.
 *
 * A Lesson read model names its own section and its two neighbours by identifier; it does not carry
 * the Course. Without this the Lesson screen could say neither which Course it belonged to nor what
 * came next by name, and the only way back to the contents was the browser's Back button.
 *
 * It is deliberately **secondary**: the two reads are issued together, and a failure here resolves
 * to `null` rather than throwing. A Student whose Lesson loads must still get their Lesson if the
 * contents cannot be built — they lose the sidebar, not the Course.
 */
async function courseContentsFor(
  courseID: string,
  locale: "ar" | "en",
): Promise<{ title: string; sections: CurriculumSection[] } | null> {
  try {
    const course = await requestCourseHomeServer(courseID, locale);
    return { title: course.title, sections: courseCurriculum(course.sections) };
  } catch {
    return null;
  }
}

/** The neighbour's own title, found by the server's pointer — never by recomputing the order. */
function titleForLesson(sections: CurriculumSection[] | undefined, lessonID: string | null): string | null {
  if (!sections || !lessonID) return null;
  for (const section of sections) {
    const found = section.lessons.find((lesson) => lesson.lessonID === lessonID);
    if (found) return found.title;
  }
  return null;
}

async function LessonContent({
  courseID,
  lessonID,
  locale,
}: {
  courseID: string;
  lessonID: string;
  locale: "ar" | "en";
}) {
  const dictionary = locale === "ar" ? ar : en;
  const labels = dictionary.learning;
  const playerLabels = locale === "ar" ? ar.player : en.player;
  const shell = shellLabels(dictionary);
  const Backward = locale === "ar" ? ArrowRight : ArrowLeft;
  try {
    const [lesson, contents] = await Promise.all([
      requestLessonReadModelServer(courseID, lessonID, locale),
      courseContentsFor(courseID, locale),
    ]);
    const playbackPlan = lessonPlaybackPlan(lesson.learning_status);
    const state = lessonState(lesson.progress);
    const StateIcon =
      state === "completed" ? CheckCircle2 : state === "in-progress" ? CirclePlay : Circle;
    const stateText =
      state === "completed"
        ? labels.completed
        : state === "in-progress"
          ? labels.lessonInProgress
          : labels.lessonNotStarted;

    return (
      <LearningShell
        locale={locale}
        dir={locale === "ar" ? "rtl" : "ltr"}
        labels={shell}
        courseContext={
          contents ? (
            <Link
              href={`/${locale}/learn/courses/${lesson.course_id}`}
              className="min-w-0 truncate rounded-md px-2 py-1 font-display text-sm font-bold text-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {contents.title}
            </Link>
          ) : null
        }
      >
        <div className="mx-auto max-w-container px-5 py-6 sm:px-6 sm:py-8">
          <Button asChild variant="ghost" size="sm" className="-ms-3">
            <Link href={`/${locale}/learn/courses/${lesson.course_id}`}>
              <Backward aria-hidden />
              {labels.backToCourse}
            </Link>
          </Button>

          {/* Content first, contents second — in the markup as well as on the screen. From `lg` the
              grid puts the contents in a second column beside the Lesson; below it they collapse to
              a single control under the header, and nothing is visually reordered behind a screen
              reader's back. */}
          <div className="mt-4 lg:grid lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start lg:gap-8">
            <div className="min-w-0">
              <header>
                <p className="font-display text-sm font-bold uppercase tracking-wide text-muted-foreground">
                  {lesson.section.title}
                </p>
                <h1 className="mt-2 font-display text-2xl font-bold text-foreground sm:text-3xl">
                  {lesson.title}
                </h1>
                <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
                  {/* A Lesson's own state, in an icon and a word. Completion is the server's and is
                      reached by watching, not by a control here — so there is no button that could
                      claim a completion the server has not recorded. */}
                  <p
                    data-testid="lesson-state"
                    data-lesson-state={state}
                    className="inline-flex items-center gap-1.5 text-sm font-semibold text-foreground"
                  >
                    <StateIcon
                      aria-hidden
                      className={`size-[18px] ${state === "completed" ? "text-primary" : "text-muted-foreground"}`}
                    />
                    {stateText}
                  </p>
                  <LearningStatusBadge
                    status={lesson.learning_status}
                    label={learningStatusLabel(lesson.learning_status, labels)}
                    detail={learningStatusDetail(lesson.learning_status, labels)}
                  />
                </div>
                <AccessUntil
                  className="mt-2"
                  expiresAt={lesson.expires_at}
                  labels={accessLabels(labels)}
                  locale={locale}
                />
              </header>

              {contents ? (
                <div className="mt-5 lg:hidden">
                  <CurriculumSheet
                    courseID={lesson.course_id}
                    locale={locale}
                    sections={contents.sections}
                    currentLessonID={lesson.lesson_id}
                    labels={{
                      ...curriculumLabels(labels),
                      courseOutline: labels.courseOutline,
                      courseContents: labels.courseContents,
                      closeCourseContents: labels.closeCourseContents,
                    }}
                  />
                </div>
              ) : null}

              <div className="mt-5">
                {playbackPlan.mountPlayer ? (
                  <LessonPlayer
                    lessonID={lesson.lesson_id}
                    locale={locale}
                    labels={playerLabels}
                    initialPositionSeconds={lesson.progress.position_seconds}
                  />
                ) : (
                  <section className="rounded-lg border border-border bg-card p-6">
                    <p className="text-muted-foreground">{labels.expired}</p>
                  </section>
                )}
              </div>

              {lesson.learning_status === "active" ? (
                <LessonMaterials
                  className="mt-8"
                  headingLevel="h2"
                  resources={lesson.resources}
                  labMaterials={lesson.lab_materials}
                  locale={locale}
                  labels={materialsLabels(labels)}
                />
              ) : null}

              <p className="mt-8 text-xs text-muted-foreground">{labels.completionAutomatic}</p>

              <LessonNavigation
                className="mt-4"
                courseId={lesson.course_id}
                navigation={lesson.navigation}
                locale={locale}
                labels={navigationLabels(labels)}
                previousTitle={titleForLesson(contents?.sections, lesson.navigation.previous_lesson_id)}
                nextTitle={titleForLesson(contents?.sections, lesson.navigation.next_lesson_id)}
              />

              {/* One action per target this visible Lesson issued a context for, and no other. A read
                  with no contexts mounts nothing, so its payload carries no reporting copy. */}
              {lessonReportTargets(lesson).length > 0 ? (
                <div className="mt-8">
                  <ReportTargetActions
                    targets={lessonReportTargets(lesson)}
                    scopePrefix={`${lesson.course_id} ${lesson.lesson_id}`}
                    locale={locale}
                    labels={reportLabels(labels)}
                  />
                </div>
              ) : null}
            </div>

            {contents ? (
              <div className="hidden lg:block">
                <CurriculumSidebar
                  courseID={lesson.course_id}
                  locale={locale}
                  sections={contents.sections}
                  currentLessonID={lesson.lesson_id}
                  labels={{
                    ...curriculumLabels(labels),
                    courseOutline: labels.courseOutline,
                    courseContents: labels.courseContents,
                    closeCourseContents: labels.closeCourseContents,
                  }}
                />
              </div>
            ) : null}
          </div>
        </div>
      </LearningShell>
    );
  } catch {
    return (
      <LearningShell locale={locale} dir={locale === "ar" ? "rtl" : "ltr"} labels={shell}>
        <div className="mx-auto max-w-3xl px-5 py-10 sm:px-6">
          <LearningUnavailable labels={unavailableLabels(labels)} />
        </div>
      </LearningShell>
    );
  }
}
