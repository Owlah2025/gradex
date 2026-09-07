import { LessonPlayer } from "@/components/learning/lesson-player";
import { lessonPlaybackPlan } from "@/components/learning/lesson-state";
import {
  AccessUntil,
  LearningProgressSummary,
  LearningStatusBadge,
  LearningUnavailable,
  LessonMaterials,
  LessonNavigation,
  MaterialsInline,
} from "@/components/learning/learning-views";
import { CurriculumSheet, CurriculumSidebar } from "@/components/learning/lesson-curriculum-panel";
import { LessonResourcesPopover } from "@/components/learning/lesson-resources-popover";
import { LearningTabs } from "@/components/learning/learning-tabs";
import { courseCurriculum, type CurriculumSection } from "@/components/learning/curriculum-model";
import { LessonProgressState } from "@/components/learning/lesson-progress-state";
import { requestCourseHomeServer, requestLessonReadModelServer } from "@/lib/api/learning-server";
import type { CourseHome } from "@/lib/api/learning";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningShell } from "@/components/learning/learning-shell";
import { ReportTargetActions } from "@/components/learning/report-content-dialog";
import { lessonReportTargets } from "@/components/learning/report-targets";
import { reportLabels } from "@/components/learning/report-labels";
import { formatLearningInteger } from "@/lib/formatters/learning";
import {
  accessLabels,
  curriculumLabels,
  learningStatusDetail,
  learningStatusLabel,
  materialsLabels,
  navigationLabels,
  progressLabels,
  shellLabels,
  unavailableLabels,
} from "@/components/learning/learning-label-sets";
import { Breadcrumbs } from "@/components/layout/breadcrumbs";

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
 *
 * The Course read model is carried out as well as the narrowed sections, because the same read is
 * what the contents' per-Lesson resource controls and the Course-wide progress figure are built
 * from. Reading it twice to avoid passing it around would be two identical protected reads.
 */
async function courseContentsFor(
  courseID: string,
  locale: "ar" | "en",
): Promise<{ title: string; sections: CurriculumSection[]; home: CourseHome } | null> {
  try {
    const course = await requestCourseHomeServer(courseID, locale);
    return { title: course.title, sections: courseCurriculum(course.sections), home: course };
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
  try {
    const [lesson, contents] = await Promise.all([
      requestLessonReadModelServer(courseID, lessonID, locale),
      courseContentsFor(courseID, locale),
    ]);
    const playbackPlan = lessonPlaybackPlan(lesson.learning_status);
    const materials = materialsLabels(labels);

    /**
     * Each Lesson's downloads, composed here and handed to the contents already built.
     *
     * The download paths, the file names and the decision that access even permits a download all
     * stay on this side of the boundary; the contents receive a subtree, never a material. Built
     * only on an active read, for the same reason the Lesson's own materials are: an expired read
     * carries no material to offer.
     */
    const resourcesByLesson =
      contents && lesson.learning_status === "active"
        ? Object.fromEntries(
            contents.home.sections.flatMap((section) =>
              section.lessons
                .filter((entry) => entry.resources.length > 0 || entry.lab_materials.length > 0)
                .map((entry) => [
                  entry.lesson_id,
                  <LessonResourcesPopover
                    key={entry.lesson_id}
                    label={labels.resources}
                    accessibleLabel={`${labels.resources}: ${entry.title}`}
                    heading={labels.materials}
                    count={formatLearningInteger(
                      entry.resources.length + entry.lab_materials.length,
                      locale,
                    )}
                  >
                    <MaterialsInline
                      layout="panel"
                      resources={entry.resources}
                      labMaterials={entry.lab_materials}
                      labels={materials}
                      locale={locale}
                    />
                  </LessonResourcesPopover>,
                ]),
            ),
          )
        : undefined;

    const curriculumPanel = contents
      ? {
          courseID: lesson.course_id,
          locale,
          sections: contents.sections,
          currentLessonID: lesson.lesson_id,
          resourcesByLesson,
          labels: {
            ...curriculumLabels(labels),
            courseOutline: labels.courseOutline,
            courseContents: labels.courseContents,
            closeCourseContents: labels.closeCourseContents,
          },
        }
      : null;

    const reportTargets = lessonReportTargets(lesson);
    const hasLessonFiles = lesson.resources.length > 0 || lesson.lab_materials.length > 0;

    return (
      <LearningShell
        locale={locale}
        dir={locale === "ar" ? "rtl" : "ltr"}
        labels={shell}
      >
        <div className="mx-auto max-w-container px-5 py-6 sm:px-6 sm:py-8">
          {/* Where this Lesson sits. The Course crumb is rendered only when the contents read
              succeeded, because that is where the Course's own title comes from; without it the
              breadcrumb would name the Course by identifier, which is not something to show a
              Student. */}
          {contents ? (
            <Breadcrumbs
              locale={locale}
              label={dictionary.nav.breadcrumb}
              items={[
                { label: labels.myCourses, href: `/${locale}/learn/dashboard` },
                { label: contents.title, href: `/${locale}/learn/courses/${lesson.course_id}` },
                { label: lesson.title },
              ]}
            />
          ) : null}

          {/* Content first, contents second — in the markup as well as on the screen. From `lg` the
              grid puts the contents in a second column beside the Lesson; below it they collapse to
              a single control under the header, and nothing is visually reordered behind a screen
              reader's back. */}
          <div className="mt-4 lg:grid lg:grid-cols-[minmax(0,1fr)_20rem] xl:grid-cols-[minmax(0,1fr)_23rem] lg:items-start lg:gap-8">
            <div className="min-w-0">
              {/* The heading band is deliberately short. Everything that used to stand between the
                  title and the picture — the access state, the expiry, the note about how a Lesson
                  completes — is real and is still on the page, one tab below the player. What a
                  Student came for is the video, and it should be the first thing under the title
                  rather than the fourth. */}
              <header>
                <p className="font-display text-sm font-bold uppercase tracking-wide text-muted-foreground">
                  {lesson.section.title}
                </p>
                <h1 className="mt-1.5 font-display text-2xl font-bold text-foreground sm:text-[28px]">
                  {lesson.title}
                </h1>
                <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2">
                  {/* A Lesson's own state, in an icon and a word. Completion is the server's and is
                      reached by watching, not by a control here — so there is no button that could
                      claim a completion the server has not recorded. */}
                  <LessonProgressState
                    lessonID={lesson.lesson_id}
                    initial={{
                      position_seconds: lesson.progress.position_seconds,
                      completed: lesson.progress.completed,
                    }}
                    labels={{
                      completed: labels.completed,
                      inProgress: labels.lessonInProgress,
                      notStarted: labels.lessonNotStarted,
                    }}
                  />
                </div>
              </header>

              {/* Below `lg` the contents have no column, so they sit behind one control — placed
                  above the player, where a Student looking for the next Lesson looks first. */}
              {curriculumPanel ? (
                <div className="mt-4 lg:hidden">
                  <CurriculumSheet {...curriculumPanel} />
                </div>
              ) : null}

              <div className="mt-4">
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

              {/* Directly under the picture and never over it. An overlay control on a video is a
                  control that disappears exactly when the video ends and the Student wants it. */}
              <LessonNavigation
                className="mt-4"
                courseId={lesson.course_id}
                navigation={lesson.navigation}
                locale={locale}
                labels={navigationLabels(labels)}
                previousTitle={titleForLesson(contents?.sections, lesson.navigation.previous_lesson_id)}
                nextTitle={titleForLesson(contents?.sections, lesson.navigation.next_lesson_id)}
              />

              {/* Three tabs, and only three, because three are what this product has. Each panel is
                  built here and handed over: the tab set chooses which is visible and never sees a
                  read model or a report context. */}
              <LearningTabs
                className="mt-8"
                label={labels.learningTabs}
                // Radix cannot read this from the document, so the page that already knows the
                // locale is the one that says it.
                dir={locale === "ar" ? "rtl" : "ltr"}
                tabs={[
                  {
                    value: "overview",
                    label: labels.overview,
                    content: (
                      <div className="space-y-5">
                        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                          <LearningStatusBadge
                            status={lesson.learning_status}
                            label={learningStatusLabel(lesson.learning_status, labels)}
                            detail={learningStatusDetail(lesson.learning_status, labels)}
                          />
                        </div>
                        <AccessUntil
                          expiresAt={lesson.expires_at}
                          labels={accessLabels(labels)}
                          locale={locale}
                        />
                        {/* The Course-wide figure, which is the server's own count over the
                            qualifying graph. Nothing is recomputed here, so this cannot disagree
                            with the Dashboard or the Course page. */}
                        {contents ? (
                          <LearningProgressSummary
                            className="max-w-sm"
                            progress={contents.home.progress}
                            labels={progressLabels(labels)}
                            locale={locale}
                          />
                        ) : null}
                        <p className="text-xs text-muted-foreground">{labels.completionAutomatic}</p>
                      </div>
                    ),
                  },
                  // The Lesson's own files, in full: kind, type and size, which the compact panel
                  // on the contents row deliberately drops. Absent entirely when the Lesson has no
                  // files or the access to read them has ended.
                  ...(lesson.learning_status === "active" && hasLessonFiles
                    ? [
                        {
                          value: "resources",
                          label: labels.resources,
                          content: (
                            <LessonMaterials
                              headingLevel="h2"
                              resources={lesson.resources}
                              labMaterials={lesson.lab_materials}
                              locale={locale}
                              labels={materials}
                            />
                          ),
                        },
                      ]
                    : []),
                  // One action per target this visible Lesson issued a context for, and no other.
                  // A read with no contexts contributes no tab, so its payload carries no
                  // reporting copy at all.
                  ...(reportTargets.length > 0
                    ? [
                        {
                          value: "report",
                          label: labels.reportDialogTitle,
                          content: (
                            <ReportTargetActions
                              targets={reportTargets}
                              scopePrefix={`${lesson.course_id} ${lesson.lesson_id}`}
                              locale={locale}
                              labels={reportLabels(labels)}
                            />
                          ),
                        },
                      ]
                    : []),
                ]}
              />
            </div>

            {curriculumPanel ? (
              // Sticky beneath the 64px header, exactly as Course Details holds its access card.
              // Without it the outline scrolls *under* the sticky header, and every Lesson row that
              // passes behind it is a partially obscured target — a real WCAG target-size failure
              // that lands on a different row at every viewport.
              <div className="hidden lg:sticky lg:top-20 lg:block">
                <CurriculumSidebar {...curriculumPanel} />
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
