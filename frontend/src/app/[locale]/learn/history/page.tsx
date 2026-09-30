import Link from "next/link";
import { ArrowRight } from "lucide-react";
import {
  LearningCompletionSummary,
  LearningProgressSummary,
  LearningUnavailable,
} from "@/components/learning/learning-views";
import { completionLabels, progressLabels, shellLabels, unavailableLabels } from "@/components/learning/learning-label-sets";
import { LearningShell } from "@/components/learning/learning-shell";
import { requestLearningHistoryServer } from "@/lib/api/learning-server";
import type { LearningHistoryCourse } from "@/lib/api/learning";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { formatLearningExpiry } from "@/lib/formatters/learning";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";

export const dynamic = "force-dynamic";
export const revalidate = 0;

function HistoryCourseCard({
  course,
  locale,
  dictionary,
  accessState,
}: {
  course: LearningHistoryCourse;
  locale: "ar" | "en";
  dictionary: Dictionary;
  accessState: "active" | "ended";
}) {
  const endedAt = course.access_ended_at ? formatLearningExpiry(course.access_ended_at, locale) : null;
  const endedReason =
    course.access_ended_reason === "revoked"
      ? dictionary.learning.endedAccessRevoked
      : dictionary.learning.endedAccessExpired;
  return (
    <Card asChild interactive={accessState === "active"}>
      <article className="flex h-full flex-col p-5">
        <h3 className="font-display text-lg font-bold text-foreground">{course.title}</h3>
        {accessState === "ended" ? (
          <p className="mt-2 text-sm font-semibold text-muted-foreground">
            {endedReason}
            {endedAt ? (
              <>
                <span className="px-1">·</span>
                <time dateTime={endedAt.dateTime}>{endedAt.text}</time>
              </>
            ) : null}
          </p>
        ) : null}
        {course.completion ? (
          <LearningCompletionSummary
            className="mt-3"
            completion={course.completion}
            labels={completionLabels(dictionary.learning)}
            locale={locale}
          />
        ) : null}
        <LearningProgressSummary
          className="mt-4"
          progress={course.progress}
          labels={progressLabels(dictionary.learning)}
          locale={locale}
        />
        {accessState === "active" ? (
          <Button asChild variant="outline" size="sm" className="mt-5 self-start">
            <Link href={`/${locale}/learn/courses/${course.course_id}`}>
              {dictionary.learning.openCourse}
              <ArrowRight aria-hidden className={locale === "ar" ? "rotate-180" : undefined} />
            </Link>
          </Button>
        ) : null}
      </article>
    </Card>
  );
}

function HistorySection({
  sectionKey,
  title,
  empty,
  courses,
  locale,
  dictionary,
  accessState,
}: {
  sectionKey: "in-progress" | "completed" | "ended";
  title: string;
  empty: string;
  courses: LearningHistoryCourse[];
  locale: "ar" | "en";
  dictionary: Dictionary;
  accessState: "active" | "ended";
}) {
  const headingID = `${sectionKey}-history-heading`;
  return (
    <section aria-labelledby={headingID} className="mt-10">
      <h2 id={headingID} className="font-display text-xl font-bold text-foreground">
        {title}
      </h2>
      {courses.length === 0 ? (
        <p className="mt-3 text-sm text-muted-foreground">{empty}</p>
      ) : (
        <ul className="mt-4 grid gap-4 md:grid-cols-2">
          {courses.map((course) => (
            <li key={course.course_id}>
              <HistoryCourseCard course={course} locale={locale} dictionary={dictionary} accessState={accessState} />
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export default async function LearningHistoryPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale: requestedLocale } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  const shell = shellLabels(dictionary);
  try {
    const history = await requestLearningHistoryServer(locale);
    return (
      <LearningShell locale={locale} dir={locale === "ar" ? "rtl" : "ltr"} labels={shell}>
        <div className="mx-auto max-w-container px-5 py-8 sm:px-6 sm:py-10">
          <header className="max-w-2xl">
            <h1 className="font-display text-2xl font-bold text-foreground sm:text-3xl">
              {dictionary.learning.historyTitle}
            </h1>
            <p className="mt-2 text-sm text-muted-foreground sm:text-base">{dictionary.learning.historyIntro}</p>
          </header>
          <HistorySection
            sectionKey="in-progress"
            title={dictionary.learning.inProgressTab}
            empty={dictionary.learning.noInProgressHistory}
            courses={history.in_progress}
            locale={locale}
            dictionary={dictionary}
            accessState="active"
          />
          <HistorySection
            sectionKey="completed"
            title={dictionary.learning.completedTab}
            empty={dictionary.learning.noCompletedHistory}
            courses={history.completed}
            locale={locale}
            dictionary={dictionary}
            accessState="active"
          />
          <HistorySection
            sectionKey="ended"
            title={dictionary.learning.endedAccess}
            empty={dictionary.learning.noEndedHistory}
            courses={history.ended_access}
            locale={locale}
            dictionary={dictionary}
            accessState="ended"
          />
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
