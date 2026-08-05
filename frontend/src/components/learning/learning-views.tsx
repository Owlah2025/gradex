import Link from "next/link";
import type {
  CourseHome,
  LearningMaterialKind,
  LearningCourseProgress,
  LearningProgress,
  LearningStatus,
  LessonReadModel,
} from "@/lib/api/learning";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import {
  formatLearningExpiry,
  formatLearningInteger,
  formatLearningPercent,
  formatLearningPositionSeconds,
} from "@/lib/formatters/learning";
import { materialEntryPath } from "./material-links";

type LearningLabels = Dictionary["learning"];

export function LessonMaterials({
  lessonID,
  materials,
  labels,
  lessonTitle,
}: {
  lessonID: string;
  materials: { kind: LearningMaterialKind }[];
  labels: LearningLabels;
  lessonTitle?: string;
}) {
  if (materials.length === 0) return null;
  return (
    <ul aria-label={labels.materials} className="flex flex-wrap gap-2">
      {materials.map(({ kind }) => {
        const label = kind === "resource" ? labels.resource : labels.labMaterial;
        const linkLabel = kind === "resource" ? labels.openResource : labels.openLabMaterial;
        const href = materialEntryPath(lessonID, kind);
        if (!href) return null;
        return (
          <li key={kind}>
            <a
              href={href}
              aria-label={lessonTitle ? `${linkLabel}: ${lessonTitle}` : linkLabel}
              className="inline-flex rounded-md border border-border px-3 py-2 text-sm font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              {label}
            </a>
          </li>
        );
      })}
    </ul>
  );
}

export function LearningUnavailable({ labels }: { labels: LearningLabels }) {
  return (
    <section role="alert" aria-labelledby="learning-unavailable-title" className="rounded-2xl border border-border bg-card p-8 text-center shadow-sm">
      <h1 id="learning-unavailable-title" className="font-display text-3xl font-bold text-foreground">
        {labels.unavailableTitle}
      </h1>
      <p className="mx-auto mt-3 max-w-xl text-foreground/80">{labels.unavailableBody}</p>
    </section>
  );
}

export function LearningStatusBadge({ status, labels }: { status: LearningStatus; labels: LearningLabels }) {
  const text = status === "expired" ? labels.expired : labels.active;
  return (
    <span
      data-learning-status={status}
      className="inline-flex rounded-full border border-border px-3 py-1 text-sm font-semibold text-foreground/80"
    >
      {text}
    </span>
  );
}

export function LearningProgressSummary({ progress, labels, locale }: { progress: LearningCourseProgress; labels: LearningLabels; locale: "ar" | "en" }) {
  const percent = formatLearningPercent(progress.percent, locale);
  const completed = formatLearningInteger(progress.completed_lessons, locale);
  const total = formatLearningInteger(progress.total_lessons, locale);
  return (
    <div aria-label={`${labels.progress}: ${percent}`} className="text-sm text-foreground/80">
      <p>
        <span className="font-semibold text-foreground">{percent}</span> · {completed}/{total} {labels.completedLessons}
      </p>
    </div>
  );
}

export function AccessUntil({ expiresAt, labels, locale }: { expiresAt: string | null; labels: LearningLabels; locale: "ar" | "en" }) {
  if (expiresAt === null) {
    return <p className="text-sm text-muted-foreground">{labels.accessUntil}: {labels.noExpiry}</p>;
  }
  const formatted = formatLearningExpiry(expiresAt, locale);
  if (!formatted) return null;
  return (
    <p className="text-sm text-foreground/80">
      {labels.accessUntil}: <time dateTime={formatted.dateTime}>{formatted.text}</time>
    </p>
  );
}

function lessonProgressText(progress: LearningProgress, labels: LearningLabels, locale: "ar" | "en"): string {
  return `${formatLearningPositionSeconds(progress.position_seconds, locale)} ${labels.positionSeconds} · ${progress.completed ? labels.completed : labels.notCompleted}`;
}

export function CourseOutline({ course, locale, labels }: { course: CourseHome; locale: "ar" | "en"; labels: LearningLabels }) {
  return (
    <nav aria-label={labels.courseOutline} className="space-y-6">
      {course.sections.map((section) => (
        <section key={section.section_id} aria-labelledby={`section-${section.section_id}`}>
          <h2 id={`section-${section.section_id}`} className="font-display text-xl font-bold text-foreground">
            {section.title}
          </h2>
          <ol className="mt-3 space-y-2">
            {section.lessons.map((lesson) => (
              <li key={lesson.lesson_id}>
                <div className="rounded-lg border border-border bg-card px-4 py-3">
                  <Link
                    href={`/${locale}/learn/courses/${course.course_id}/lessons/${lesson.lesson_id}`}
                    className="flex items-center justify-between gap-4 transition-colors hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  >
                    <span className="min-w-0 truncate font-medium text-foreground">{lesson.title}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">{lessonProgressText(lesson.progress, labels, locale)}</span>
                  </Link>
                  {course.learning_status === "active" ? <LessonMaterials lessonID={lesson.lesson_id} lessonTitle={lesson.title} materials={lesson.materials} labels={labels} /> : null}
                </div>
              </li>
            ))}
          </ol>
        </section>
      ))}
    </nav>
  );
}

export function LessonNavigation({ lesson, locale, labels }: { lesson: LessonReadModel; locale: "ar" | "en"; labels: LearningLabels }) {
  const basePath = `/${locale}/learn/courses/${lesson.course_id}/lessons`;
  return (
    <nav aria-label={labels.lessonNavigation} className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-5">
      {lesson.navigation.previous_lesson_id ? (
        <Link
          href={`${basePath}/${lesson.navigation.previous_lesson_id}`}
          className="rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {labels.previousLesson}
        </Link>
      ) : (
        <span className="text-sm font-medium text-foreground/80">{labels.firstLesson}</span>
      )}
      {lesson.navigation.next_lesson_id ? (
        <Link
          href={`${basePath}/${lesson.navigation.next_lesson_id}`}
          className="rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {labels.nextLesson}
        </Link>
      ) : (
        <span className="text-sm font-medium text-foreground/80">{labels.lastLesson}</span>
      )}
    </nav>
  );
}
