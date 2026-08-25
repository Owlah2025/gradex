import Link from "next/link";
import type {
  CourseHomeSection,
  LearningCourseProgress,
  LearningMaterial,
  LearningProgress,
  LearningStatus,
  LessonNavigation as LessonNavigationModel,
} from "@/lib/api/learning";
import type {
  AccessLabels,
  LessonProgressLabels,
  MaterialsLabels,
  NavigationLabels,
  OutlineLabels,
  ProgressLabels,
  UnavailableLabels,
} from "./learning-label-sets";
import {
  formatLearningExpiry,
  formatLearningInteger,
  formatLearningPercent,
  formatLearningPositionSeconds,
} from "@/lib/formatters/learning";
import { MaterialDownload } from "./material-download";

/**
 * Every component here takes the narrowest data it renders (T7).
 *
 * These are server components, and in development React publishes each server component's props in
 * its owner stack, so an oversized prop reaches the page exactly as a client prop would. Taking a
 * read-model slice rather than the whole model, and a label subset rather than the dictionary, is
 * what keeps `report_context` and unrendered status copy out of the payload in every build mode.
 */

export function LessonMaterials({
  resources,
  labMaterials,
  labels,
  locale,
}: {
  resources: LearningMaterial[];
  labMaterials: LearningMaterial[];
  labels: MaterialsLabels;
  locale: "ar" | "en";
}) {
  if (resources.length === 0 && labMaterials.length === 0) return null;
  return (
    <section aria-label={labels.materials} className="mt-4 space-y-4 rounded-lg border border-border bg-card p-4">
      {resources.length > 0 ? (
        <MaterialList title={labels.resources} items={resources} locale={locale} labels={labels} />
      ) : null}
      {labMaterials.length > 0 ? (
        <MaterialList title={labels.labMaterials} items={labMaterials} locale={locale} labels={labels} />
      ) : null}
    </section>
  );
}

function MaterialList({
  title,
  items,
  locale,
  labels,
}: {
  title: string;
  items: LearningMaterial[];
  locale: "ar" | "en";
  labels: MaterialsLabels;
}) {
  return (
    <section aria-label={title}>
      <h2 className="font-display text-lg font-bold text-foreground">{title}</h2>
      <ul className="mt-2 space-y-2">
        {items.map((item) => (
          <li key={item.download_authorization_path} className="rounded-md border border-border px-3 py-2">
            <MaterialDownload
              authorizationPath={item.download_authorization_path}
              title={item.title}
              locale={locale}
              downloadLabel={labels.download}
              preparingLabel={labels.preparingDownload}
              unavailableLabel={labels.downloadUnavailable}
            />
            <p className="mt-1 text-xs text-muted-foreground">
              {item.file_type} · {formatMaterialSize(item.size_bytes, locale)}
            </p>
          </li>
        ))}
      </ul>
    </section>
  );
}

function formatMaterialSize(bytes: number, locale: "ar" | "en"): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let size = bytes;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: unit === 0 ? 0 : 1 }).format(size)} ${units[unit]}`;
}

export function LearningUnavailable({ labels }: { labels: UnavailableLabels }) {
  return (
    <section role="alert" aria-labelledby="learning-unavailable-title" className="rounded-2xl border border-border bg-card p-8 text-center shadow-sm">
      <h1 id="learning-unavailable-title" className="font-display text-3xl font-bold text-foreground">
        {labels.unavailableTitle}
      </h1>
      <p className="mx-auto mt-3 max-w-xl text-foreground/80">{labels.unavailableBody}</p>
    </section>
  );
}

/**
 * The badge receives its resolved text, not both strings to choose between. Choosing here would
 * publish the copy the page deliberately does not display — which is precisely how an expired
 * Lesson came to carry active-state copy (GAP-04).
 */
export function LearningStatusBadge({ status, label }: { status: LearningStatus; label: string }) {
  const text = label;
  return (
    <span
      data-learning-status={status}
      className="inline-flex rounded-full border border-border px-3 py-1 text-sm font-semibold text-foreground/80"
    >
      {text}
    </span>
  );
}

export function LearningProgressSummary({ progress, labels, locale }: { progress: LearningCourseProgress; labels: ProgressLabels; locale: "ar" | "en" }) {
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

export function AccessUntil({ expiresAt, labels, locale }: { expiresAt: string | null; labels: AccessLabels; locale: "ar" | "en" }) {
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

function lessonProgressText(progress: LearningProgress, labels: LessonProgressLabels, locale: "ar" | "en"): string {
  return `${formatLearningPositionSeconds(progress.position_seconds, locale)} ${labels.positionSeconds} · ${progress.completed ? labels.completed : labels.notCompleted}`;
}

/**
 * The outline renders sections, lesson links, and per-lesson materials. It therefore takes exactly
 * those, never the whole CourseHome: the read model also carries the opaque report context, and a
 * component that accepts the whole model publishes the whole model (GAP-03).
 */
export function CourseOutline({
  courseId,
  learningStatus,
  sections,
  locale,
  labels,
}: {
  courseId: string;
  learningStatus: LearningStatus;
  sections: CourseHomeSection[];
  locale: "ar" | "en";
  labels: OutlineLabels;
}) {
  return (
    <nav aria-label={labels.courseOutline} className="space-y-6">
      {sections.map((section) => (
        <section key={section.section_id} aria-labelledby={`section-${section.section_id}`}>
          <h2 id={`section-${section.section_id}`} className="font-display text-xl font-bold text-foreground">
            {section.title}
          </h2>
          <ol className="mt-3 space-y-2">
            {section.lessons.map((lesson) => (
              <li key={lesson.lesson_id}>
                <div className="rounded-lg border border-border bg-card px-4 py-3">
                  <Link
                    href={`/${locale}/learn/courses/${courseId}/lessons/${lesson.lesson_id}`}
                    className="flex items-center justify-between gap-4 transition-colors hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                  >
                    <span className="min-w-0 truncate font-medium text-foreground">{lesson.title}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">{lessonProgressText(lesson.progress, labels, locale)}</span>
                  </Link>
                  {learningStatus === "active" ? <LessonMaterials resources={lesson.resources} labMaterials={lesson.lab_materials} locale={locale} labels={labels} /> : null}
                </div>
              </li>
            ))}
          </ol>
        </section>
      ))}
    </nav>
  );
}

/**
 * Navigation renders two links. It takes the two pointers, not the whole LessonReadModel, which
 * also carries the per-target report contexts.
 */
export function LessonNavigation({
  courseId,
  navigation,
  locale,
  labels,
}: {
  courseId: string;
  navigation: LessonNavigationModel;
  locale: "ar" | "en";
  labels: NavigationLabels;
}) {
  const basePath = `/${locale}/learn/courses/${courseId}/lessons`;
  return (
    <nav aria-label={labels.lessonNavigation} className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-5">
      {navigation.previous_lesson_id ? (
        <Link
          href={`${basePath}/${navigation.previous_lesson_id}`}
          className="rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {labels.previousLesson}
        </Link>
      ) : (
        <span className="text-sm font-medium text-foreground/80">{labels.firstLesson}</span>
      )}
      {navigation.next_lesson_id ? (
        <Link
          href={`${basePath}/${navigation.next_lesson_id}`}
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
