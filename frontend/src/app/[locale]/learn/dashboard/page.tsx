import Link from "next/link";
import { ArrowRight, GraduationCap } from "lucide-react";
import {
  AccessUntil,
  LearningProgressSummary,
  LearningStatusBadge,
  LearningUnavailable,
} from "@/components/learning/learning-views";
import {
  accessLabels,
  learningStatusDetail,
  learningStatusLabel,
  progressLabels,
  shellLabels,
  unavailableLabels,
} from "@/components/learning/learning-label-sets";
import { LearningShell } from "@/components/learning/learning-shell";
import { requestLearningDashboardServer, requestStudentCourseAccessServer } from "@/lib/api/learning-server";
import { hasPendingAccess, pendingAccessSummary } from "@/components/learning/pending-access-summary";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { AcademicProfilePrompt } from "@/components/learning/academic-profile-prompt";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/common/empty-state";

export const dynamic = "force-dynamic";
export const revalidate = 0;

/**
 * The Student's home.
 *
 * # WHAT THIS SCREEN IS FOR
 *
 * One question: *what should I learn next?* The answer the product can actually give is the
 * server's continue-learning pointer, so that is the first thing on the page and the only thing
 * styled as the primary action. Everything below it — anything waiting on someone else, then the
 * Courses themselves — is context for when the answer is "something else today".
 *
 * There are no study statistics here, and no room made for any. The server knows how many Lessons a
 * Student has completed and nothing more: no watch time, no streak, no time remaining. A dashboard
 * of tiles would have had to invent the rest.
 */
export default async function LearningDashboardPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale: requestedLocale } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  const shell = shellLabels(dictionary);
  try {
    // Issued together rather than in sequence: the pending summary is secondary information and
    // must not add a second serial round trip to the Course list every Student already waits for.
    // The access read resolves to null on failure, so only the Course list can fail this page.
    const [dashboard, access] = await Promise.all([
      requestLearningDashboardServer(locale),
      requestStudentCourseAccessServer(locale),
    ]);
    const pending = pendingAccessSummary(access?.items);
    return (
      <LearningShell locale={locale} dir={locale === "ar" ? "rtl" : "ltr"} labels={shell}>
        <div className="mx-auto max-w-container px-5 py-8 sm:px-6 sm:py-10">
          <header className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
            <div className="min-w-0">
              <h1 className="font-display text-2xl font-bold text-foreground sm:text-3xl">
                {dictionary.learning.dashboardTitle}
              </h1>
              <p className="mt-2 max-w-2xl text-sm text-muted-foreground sm:text-base">
                {dictionary.learning.dashboardIntro}
              </p>
            </div>
            {/* The Student's route back to their access status, so it no longer depends on keeping
                the one-time invitation email. */}
            <Button asChild variant="outline" size="sm">
              <Link href={`/${locale}/access`} data-testid="dashboard-access-link">
                {dictionary.access.navLabel}
              </Link>
            </Button>
          </header>

          {/* Academic profile invitation. A card on a page the Student already
              reached, never a redirect: onboarding is personalisation, not a gate,
              and it renders only for a Student who has made no decision yet. */}
          <AcademicProfilePrompt />

          {/* Continue learning. The server chooses the target from Progress and only returns one the
              Student may currently open, so this can never point somewhere they would be refused.
              Playback position stays the Lesson Player's business. */}
          {dashboard.resume ? (
            <section
              data-testid="continue-learning"
              aria-labelledby="continue-learning-title"
              className="mt-8 rounded-lg border border-gx-blue-200 bg-gx-blue-50 p-5 sm:p-6 dark:border-border dark:bg-card"
            >
              <h2
                id="continue-learning-title"
                className="font-display text-sm font-bold uppercase tracking-wide text-gx-blue-600 dark:text-primary"
              >
                {dashboard.resume.started
                  ? dictionary.learning.resumeHeading
                  : dictionary.learning.resumeStartHeading}
              </h2>
              <p
                data-testid="continue-learning-course"
                className="mt-3 font-display text-xl font-bold text-foreground sm:text-2xl"
              >
                {dashboard.resume.course_title}
              </p>
              <p data-testid="continue-learning-lesson" className="mt-1 text-sm text-muted-foreground">
                {dictionary.learning.resumeLesson}: {dashboard.resume.lesson_title}
              </p>
              <Button asChild className="mt-5">
                <Link
                  href={`/${locale}/learn/courses/${dashboard.resume.course_id}/lessons/${dashboard.resume.lesson_id}`}
                  data-testid="continue-learning-action"
                >
                  {dashboard.resume.started
                    ? dictionary.learning.resumeAction
                    : dictionary.learning.resumeStartAction}
                </Link>
              </Button>
            </section>
          ) : null}

          {/* Pending Course access. Counts only, and each line names the actor who is holding
              things up. The Access page owns the detail, so nothing here exposes a Course or
              invitation identifier, or any lifecycle vocabulary. */}
          {hasPendingAccess(pending) ? (
            <section data-testid="pending-access-summary" aria-labelledby="pending-access-title" className="mt-6">
              <Card className="p-5">
                <h2 id="pending-access-title" className="font-display text-lg font-bold text-foreground">
                  {dictionary.learning.pendingAccessTitle}
                </h2>
                {pending.actionRequired > 0 ? (
                  <p data-testid="pending-access-action-required" className="mt-2 font-semibold text-foreground">
                    {pending.actionRequired === 1
                      ? dictionary.learning.accessActionRequiredOne
                      : `${pending.actionRequired} ${dictionary.learning.accessActionRequiredMany}`}
                  </p>
                ) : null}
                {pending.awaitingApproval > 0 ? (
                  <p data-testid="pending-access-awaiting-approval" className="mt-2 text-muted-foreground">
                    {pending.awaitingApproval === 1
                      ? dictionary.learning.pendingAccessOne
                      : `${pending.awaitingApproval} ${dictionary.learning.pendingAccessMany}`}
                  </p>
                ) : null}
                <Button asChild variant="outline" size="sm" className="mt-4">
                  <Link href={`/${locale}/access`} data-testid="pending-access-action">
                    {dictionary.learning.pendingAccessAction}
                  </Link>
                </Button>
              </Card>
            </section>
          ) : null}

          {/* The Courses are the page's own second level, so each card's title is its `h2` and the
              region is named rather than headed. A separate "My courses" heading above them would
              claim the same rank as the Courses it introduces. */}
          <section aria-label={dictionary.learning.myCourses} className="mt-10">
            {dashboard.courses.length === 0 ? (
              <EmptyState
                className="mt-4"
                icon={<GraduationCap aria-hidden />}
                title={dictionary.learning.emptyTitle}
                description={dictionary.learning.emptyBody}
                action={
                  <Button asChild variant="outline">
                    <Link href={`/${locale}/access`}>{dictionary.learning.emptyAction}</Link>
                  </Button>
                }
              />
            ) : (
              <ul className="grid gap-4 md:grid-cols-2">
                {dashboard.courses.map((course) => (
                  <li key={course.course_id}>
                    <Card asChild interactive>
                      <article className="flex h-full flex-col p-5">
                      <h2 className="font-display text-lg font-bold text-foreground">
                        {course.title}
                      </h2>
                      <div className="mt-2">
                      <LearningStatusBadge
                        status={course.learning_status}
                        label={learningStatusLabel(course.learning_status, dictionary.learning)}
                        detail={learningStatusDetail(course.learning_status, dictionary.learning)}
                      />
                      </div>
                      <LearningProgressSummary
                        className="mt-4"
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
                      <Button asChild variant="outline" size="sm" className="mt-5 self-start">
                        <Link href={`/${locale}/learn/courses/${course.course_id}`}>
                          {dictionary.learning.openCourse}
                          <ArrowRight aria-hidden className={locale === "ar" ? "rotate-180" : undefined} />
                        </Link>
                      </Button>
                      </article>
                    </Card>
                  </li>
                ))}
              </ul>
            )}
          </section>
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
