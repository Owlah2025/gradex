import { LessonPlayer } from "@/components/learning/lesson-player";
import { lessonPlaybackPlan } from "@/components/learning/lesson-state";
import {
  AccessUntil,
  LearningStatusBadge,
  LearningUnavailable,
  LessonMaterials,
  LessonNavigation,
} from "@/components/learning/learning-views";
import { requestLessonReadModelServer } from "@/lib/api/learning-server";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningLocaleToggle } from "@/components/learning/learning-locale-toggle";
import { ReportTargetActions } from "@/components/learning/report-content-dialog";
import { lessonReportTargets } from "@/components/learning/report-targets";
import { reportLabels } from "@/components/learning/report-labels";
import { formatLearningPositionSeconds } from "@/lib/formatters/learning";

export const dynamic = "force-dynamic";
export const revalidate = 0;

export default async function LessonPage({
  params,
}: {
  params: Promise<{ locale: "ar" | "en"; courseId: string; lessonId: string }>;
}) {
  const { locale: requestedLocale, courseId, lessonId } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  return <LessonContent courseID={courseId} lessonID={lessonId} locale={locale} labels={dictionary.learning} playerLabels={dictionary.player} localeSwitchLabel={dictionary.meta.switchToAria} />;
}

async function LessonContent({
  courseID,
  lessonID,
  locale,
  labels,
  playerLabels,
  localeSwitchLabel,
}: {
  courseID: string;
  lessonID: string;
  locale: "ar" | "en";
  labels: typeof ar.learning;
  playerLabels: typeof ar.player;
  localeSwitchLabel: string;
}) {
  try {
    const lesson = await requestLessonReadModelServer(courseID, lessonID, locale);
    const playbackPlan = lessonPlaybackPlan(lesson.learning_status);
    return (
      <main dir={locale === "ar" ? "rtl" : "ltr"} className="mx-auto min-h-screen max-w-6xl px-5 py-10 sm:px-6">
        <header className="mb-8">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-sm font-semibold text-foreground/80">{lesson.section.title}</p>
              <div className="mt-2 flex flex-wrap items-start justify-between gap-3">
                <h1 className="font-display text-4xl font-bold text-foreground">{lesson.title}</h1>
                <LearningStatusBadge status={lesson.learning_status} labels={labels} />
              </div>
            </div>
            <LearningLocaleToggle locale={locale} label={localeSwitchLabel} />
          </div>
          <div className="mt-5 space-y-2">
            <p className="text-sm text-foreground/80">{labels.progress}: {formatLearningPositionSeconds(lesson.progress.position_seconds, locale)} {labels.positionSeconds} · {lesson.progress.completed ? labels.completed : labels.notCompleted}</p>
            <AccessUntil expiresAt={lesson.expires_at} labels={labels} locale={locale} />
          </div>
        </header>
        {lesson.learning_status === "active" ? <LessonMaterials lessonID={lesson.lesson_id} lessonTitle={lesson.title} materials={lesson.materials} labels={labels} /> : null}
        {playbackPlan.mountPlayer ? (
          <LessonPlayer lessonID={lesson.lesson_id} locale={locale} labels={playerLabels} initialPositionSeconds={lesson.progress.position_seconds} />
        ) : (
          <section className="rounded-2xl border border-border bg-card p-6">
            <p className="text-muted-foreground">{labels.expired}</p>
          </section>
        )}
        {/* One action per target this visible Lesson issued a context for, and no other. A read
            with no contexts mounts nothing, so its payload carries no reporting copy. */}
        {lessonReportTargets(lesson).length > 0 ? (
          <div className="mt-6">
            <ReportTargetActions
              targets={lessonReportTargets(lesson)}
              scopePrefix={`${lesson.course_id} ${lesson.lesson_id}`}
              locale={locale}
              labels={reportLabels(labels)}
            />
          </div>
        ) : null}
        <LessonNavigation lesson={lesson} locale={locale} labels={labels} />
      </main>
    );
  } catch {
    return (
      <main dir={locale === "ar" ? "rtl" : "ltr"} className="mx-auto min-h-screen max-w-3xl px-5 py-10 sm:px-6">
        <div className="space-y-5">
          <div className="flex justify-end"><LearningLocaleToggle locale={locale} label={localeSwitchLabel} /></div>
          <LearningUnavailable labels={labels} />
        </div>
      </main>
    );
  }
}
