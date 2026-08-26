import Link from "next/link";
import { ArrowLeft, ArrowRight, FileText } from "lucide-react";
import type {
  LearningCourseProgress,
  LearningMaterial,
  LearningStatus,
  LessonNavigation as LessonNavigationModel,
} from "@/lib/api/learning";
import type {
  AccessLabels,
  MaterialsLabels,
  NavigationLabels,
  ProgressLabels,
  UnavailableLabels,
} from "./learning-label-sets";
import {
  formatLearningExpiry,
  formatLearningInteger,
  formatLearningPercent,
} from "@/lib/formatters/learning";
import { Badge } from "@/components/ui/badge";
import { MaterialDownload } from "./material-download";
import { cn } from "@/lib/utils";

/**
 * Every component here takes the narrowest data it renders (T7).
 *
 * These are server components, and in development React publishes each server component's props in
 * its owner stack, so an oversized prop reaches the page exactly as a client prop would. Taking a
 * read-model slice rather than the whole model, and a label subset rather than the dictionary, is
 * what keeps `report_context` and unrendered status copy out of the payload in every build mode.
 */

/**
 * A Lesson's downloadable items, split the way the product splits them.
 *
 * `headingLevel` is a prop because the same list appears in two places at two depths: beside the
 * Lesson it belongs to, where Resources and Lab materials are the page's second level, and inside
 * the Course contents, where they sit under a section heading that is already an `h2`.
 */
export function LessonMaterials({
  resources,
  labMaterials,
  labels,
  locale,
  headingLevel = "h3",
  className,
}: {
  resources: LearningMaterial[];
  labMaterials: LearningMaterial[];
  labels: MaterialsLabels;
  locale: "ar" | "en";
  headingLevel?: "h2" | "h3" | "h4";
  className?: string;
}) {
  if (resources.length === 0 && labMaterials.length === 0) return null;
  return (
    <section aria-label={labels.materials} className={cn("space-y-5", className)}>
      {resources.length > 0 ? (
        <MaterialList
          title={labels.resources}
          items={resources}
          locale={locale}
          labels={labels}
          headingLevel={headingLevel}
        />
      ) : null}
      {labMaterials.length > 0 ? (
        <MaterialList
          title={labels.labMaterials}
          items={labMaterials}
          locale={locale}
          labels={labels}
          headingLevel={headingLevel}
        />
      ) : null}
    </section>
  );
}

function MaterialList({
  title,
  items,
  locale,
  labels,
  headingLevel: Heading,
}: {
  title: string;
  items: LearningMaterial[];
  locale: "ar" | "en";
  labels: MaterialsLabels;
  headingLevel: "h2" | "h3" | "h4";
}) {
  return (
    <section aria-label={title}>
      <Heading className="font-display text-sm font-bold uppercase tracking-wide text-muted-foreground">
        {title}
      </Heading>
      <ul className="mt-2 space-y-2">
        {items.map((item) => (
          <li
            key={item.download_authorization_path}
            className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border border-border bg-card px-3 py-2.5"
          >
            <FileText aria-hidden className="size-[18px] shrink-0 text-muted-foreground" />
            <div className="min-w-0 flex-1">
              <MaterialDownload
                authorizationPath={item.download_authorization_path}
                title={item.title}
                locale={locale}
                downloadLabel={labels.download}
                preparingLabel={labels.preparingDownload}
                unavailableLabel={labels.downloadUnavailable}
              />
              {/* The type and size are the file's own description, not a storage key: a Student
                  decides whether to spend the download on a phone connection from these two facts.
                  Isolated so a Latin-script extension beside an Arabic file name cannot reorder the
                  line it sits on. */}
              <p className="mt-1 flex flex-wrap gap-x-2 text-xs text-muted-foreground">
                <bdi>{item.file_type}</bdi>
                <bdi>{formatMaterialSize(item.size_bytes, locale)}</bdi>
              </p>
            </div>
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
    <section
      role="alert"
      aria-labelledby="learning-unavailable-title"
      className="rounded-lg border border-border bg-card p-8 text-center shadow-sm"
    >
      <h1 id="learning-unavailable-title" className="font-display text-3xl font-bold text-foreground">
        {labels.unavailableTitle}
      </h1>
      <p className="mx-auto mt-3 max-w-xl text-muted-foreground">{labels.unavailableBody}</p>
    </section>
  );
}

/**
 * The badge receives its resolved text, not both strings to choose between. Choosing here would
 * publish the copy the page deliberately does not display — which is precisely how an expired
 * Lesson came to carry active-state copy (GAP-04).
 *
 * `detail` is what the state *means* for the Student, said in words beside the pill. A tone alone
 * distinguishes the two states only for a reader who can separate the two tones; the sentence
 * distinguishes them for everyone, and is the half a screen reader conveys.
 *
 * Neither tone is the success token. `gx-success` on `gx-success-soft` measures 3.94:1, which is
 * below AA for text this size, and an access state is not a place to spend a known contrast defect.
 */
export function LearningStatusBadge({
  status,
  label,
  detail,
}: {
  status: LearningStatus;
  label: string;
  detail?: string;
}) {
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      {/* `data-learning-status` sits on the pill, not on a wrapper around it. The attribute names
          the state, and the element it names must be the one whose text *is* the state — the S5 and
          T8A suites read both from it, and a wrapper that also contains the explanatory sentence
          makes "the badge says Active access" false while nothing is actually wrong. */}
      <Badge
        data-learning-status={status}
        variant={status === "expired" ? "neutral" : "default"}
        className="whitespace-nowrap"
      >
        {label}
      </Badge>
      {detail ? <span className="text-xs font-semibold text-muted-foreground">{detail}</span> : null}
    </span>
  );
}

/**
 * The one progress representation in the product.
 *
 * Every figure here is the server's: it counts completed Lessons over the qualifying graph and
 * sends the percentage with them. Nothing is recomputed on the client, so the Dashboard, the Course
 * page and the Lesson cannot disagree about how far a Student has got.
 *
 * The bar is `aria-hidden` and the numbers are the accessible content — a progress bar that only
 * draws is a progress bar that says nothing.
 */
export function LearningProgressSummary({
  progress,
  labels,
  locale,
  className,
}: {
  progress: LearningCourseProgress;
  labels: ProgressLabels;
  locale: "ar" | "en";
  className?: string;
}) {
  const percent = formatLearningPercent(progress.percent, locale);
  const completed = formatLearningInteger(progress.completed_lessons, locale);
  const total = formatLearningInteger(progress.total_lessons, locale);
  const filled = Math.min(100, Math.max(0, Math.round(progress.percent)));
  return (
    <div className={cn("space-y-1.5", className)}>
      {/* The two facts are separated by space rather than by a middle dot. In Arabic the dot sits
          beside Arabic-Indic digits and is all but indistinguishable from ٠, so "· ٢ ملفات" read as
          twenty files. Nothing is lost by spacing them instead. */}
      <p className="flex flex-wrap items-baseline gap-x-2 text-sm text-foreground">
        <span className="font-display font-bold">{percent}</span>
        <span className="text-muted-foreground">
          {completed}/{total} {labels.completedLessons}
        </span>
      </p>
      <div
        role="progressbar"
        aria-label={labels.progress}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={filled}
        aria-valuetext={percent}
        className="h-1.5 w-full overflow-hidden rounded-pill bg-muted"
      >
        {/* Inline width because the value is data, not a design decision; the track carries the
            shape and the tokens carry the colour. */}
        <div className="h-full rounded-pill bg-primary" style={{ width: `${filled}%` }} />
      </div>
    </div>
  );
}

export function AccessUntil({
  expiresAt,
  labels,
  locale,
  className,
}: {
  expiresAt: string | null;
  labels: AccessLabels;
  locale: "ar" | "en";
  className?: string;
}) {
  if (expiresAt === null) {
    return (
      <p className={cn("text-sm text-muted-foreground", className)}>
        {labels.accessUntil}: {labels.noExpiry}
      </p>
    );
  }
  const formatted = formatLearningExpiry(expiresAt, locale);
  if (!formatted) return null;
  return (
    <p className={cn("text-sm text-muted-foreground", className)}>
      {labels.accessUntil}: <time dateTime={formatted.dateTime}>{formatted.text}</time>
    </p>
  );
}

/**
 * Navigation renders two links. It takes the two pointers, not the whole LessonReadModel, which
 * also carries the per-target report contexts.
 *
 * The neighbouring titles are optional and are looked up *by the server's pointer* rather than by
 * recomputing an order here. The pointers stay authoritative; the titles only say where they lead,
 * so a Student moving through a Course can see the next Lesson without opening the contents.
 *
 * The arrows are `ArrowLeft`/`ArrowRight` chosen by reading direction, because "previous" is behind
 * the reader in Arabic and in English alike. Playback controls are not treated this way — a media
 * timeline runs the same way in both languages — which is why no icon here is shared with the
 * player.
 */
export function LessonNavigation({
  courseId,
  navigation,
  locale,
  labels,
  previousTitle,
  nextTitle,
  className,
}: {
  courseId: string;
  navigation: LessonNavigationModel;
  locale: "ar" | "en";
  labels: NavigationLabels;
  previousTitle?: string | null;
  nextTitle?: string | null;
  className?: string;
}) {
  const basePath = `/${locale}/learn/courses/${courseId}/lessons`;
  const Backward = locale === "ar" ? ArrowRight : ArrowLeft;
  const Forward = locale === "ar" ? ArrowLeft : ArrowRight;
  return (
    <nav
      aria-label={labels.lessonNavigation}
      className={cn("grid gap-3 border-t border-border pt-5 sm:grid-cols-2", className)}
    >
      {navigation.previous_lesson_id ? (
        <LessonNavigationLink
          href={`${basePath}/${navigation.previous_lesson_id}`}
          direction={labels.previousLesson}
          title={previousTitle}
          icon={<Backward aria-hidden className="size-4 shrink-0 text-muted-foreground" />}
          align="start"
        />
      ) : (
        <p className="rounded-md border border-dashed border-border px-4 py-3 text-sm text-muted-foreground">
          {labels.firstLesson}
        </p>
      )}
      {navigation.next_lesson_id ? (
        <LessonNavigationLink
          href={`${basePath}/${navigation.next_lesson_id}`}
          direction={labels.nextLesson}
          title={nextTitle}
          icon={<Forward aria-hidden className="size-4 shrink-0 text-muted-foreground" />}
          align="end"
        />
      ) : (
        <p className="rounded-md border border-dashed border-border px-4 py-3 text-end text-sm text-muted-foreground">
          {labels.lastLesson}
        </p>
      )}
    </nav>
  );
}

function LessonNavigationLink({
  href,
  direction,
  title,
  icon,
  align,
}: {
  href: string;
  direction: string;
  title?: string | null;
  icon: React.ReactNode;
  align: "start" | "end";
}) {
  return (
    <Link
      href={href}
      // The accessible name is the direction *and* the destination, so two links in the same
      // navigation are never announced as the same control.
      aria-label={title ? `${direction}: ${title}` : direction}
      className={cn(
        "flex items-center gap-3 rounded-md border border-border bg-card px-4 py-3 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        // The forward link puts its arrow on the side it travels towards, at every width — an
        // arrow leading a "next" control points back at the reader.
        align === "end" && "flex-row-reverse text-end",
      )}
    >
      {icon}
      <span className="min-w-0">
        <span className="block text-xs font-semibold text-muted-foreground">{direction}</span>
        {title ? (
          <span className="mt-0.5 block truncate font-display text-[15px] font-bold text-foreground">
            {title}
          </span>
        ) : null}
      </span>
    </Link>
  );
}

/**
 * The same materials, compacted for a row inside the Course contents.
 *
 * The full list carries a heading per kind. Nested inside a Lesson row that is itself inside a
 * section disclosure, that produced three headings and two bordered cards for two files, and at
 * 390px the downloads took more vertical space than the Lesson they belonged to. Here the kind is a
 * word on the row instead, so the same information costs one line each.
 */
export function MaterialsInline({
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
  const items = [
    ...resources.map((item) => ({ item, kind: labels.resource })),
    ...labMaterials.map((item) => ({ item, kind: labels.labMaterial })),
  ];
  if (items.length === 0) return null;
  return (
    <ul aria-label={labels.materials} className="mt-1 space-y-1 ps-9">
      {items.map(({ item, kind }) => (
        <li key={item.download_authorization_path} className="flex items-start gap-2 px-2 py-1.5">
          <FileText aria-hidden className="mt-1 size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0 flex-1">
            <MaterialDownload
              authorizationPath={item.download_authorization_path}
              title={item.title}
              locale={locale}
              downloadLabel={labels.download}
              preparingLabel={labels.preparingDownload}
              unavailableLabel={labels.downloadUnavailable}
            />
            <p className="mt-0.5 flex flex-wrap gap-x-2 text-xs text-muted-foreground">
              <span>{kind}</span>
              <bdi>{item.file_type}</bdi>
              <bdi>{formatMaterialSize(item.size_bytes, locale)}</bdi>
            </p>
          </div>
        </li>
      ))}
    </ul>
  );
}
