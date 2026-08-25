import Link from "next/link";
import { LearningProgressSummary, LearningStatusBadge, AccessUntil, LearningUnavailable } from "@/components/learning/learning-views";
import {
  accessLabels,
  learningStatusLabel,
  progressLabels,
  unavailableLabels,
} from "@/components/learning/learning-label-sets";
import { requestLearningDashboardServer, requestStudentCourseAccessServer } from "@/lib/api/learning-server";
import { hasPendingAccess, pendingAccessSummary } from "@/components/learning/pending-access-summary";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningLocaleToggle } from "@/components/learning/learning-locale-toggle";
import { AcademicProfilePrompt } from "@/components/learning/academic-profile-prompt";

export const dynamic = "force-dynamic";
export const revalidate = 0;

export default async function LearningDashboardPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale: requestedLocale } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
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
      <main dir={locale === "ar" ? "rtl" : "ltr"} className="mx-auto min-h-screen max-w-6xl px-5 py-10 sm:px-6">
        <header className="mb-8">
          <div className="flex items-start justify-between gap-4">
            <div>
              <h1 className="font-display text-4xl font-bold text-foreground">{dictionary.learning.dashboardTitle}</h1>
              <p className="mt-3 max-w-2xl text-muted-foreground">{dictionary.learning.dashboardIntro}</p>
            </div>
            <LearningLocaleToggle locale={locale} label={dictionary.meta.switchToAria} />
          </div>
          {/* The Student's route back to their access status, so it no longer depends on keeping
              the one-time invitation email. */}
          <Link
            href={`/${locale}/access`}
            data-testid="dashboard-access-link"
            className="mt-4 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {dictionary.access.navLabel}
          </Link>
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
            className="mb-8 rounded-lg border border-border bg-card p-5"
          >
            <h2 id="continue-learning-title" className="font-display text-lg font-bold text-foreground">
              {dashboard.resume.started
                ? dictionary.learning.resumeHeading
                : dictionary.learning.resumeStartHeading}
            </h2>
            <p data-testid="continue-learning-course" className="mt-2 font-semibold text-foreground">
              {dashboard.resume.course_title}
            </p>
            <p data-testid="continue-learning-lesson" className="mt-1 text-muted-foreground">
              {dictionary.learning.resumeLesson}: {dashboard.resume.lesson_title}
            </p>
            <Link
              href={`/${locale}/learn/courses/${dashboard.resume.course_id}/lessons/${dashboard.resume.lesson_id}`}
              data-testid="continue-learning-action"
              className="mt-4 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              {dashboard.resume.started
                ? dictionary.learning.resumeAction
                : dictionary.learning.resumeStartAction}
            </Link>
          </section>
        ) : null}

        {/* Pending Course access. Counts only, and each line names the actor who is holding
            things up. The Access page owns the detail, so nothing here exposes a Course or
            invitation identifier, or any lifecycle vocabulary. */}
        {hasPendingAccess(pending) ? (
          <section
            data-testid="pending-access-summary"
            aria-labelledby="pending-access-title"
            className="mb-8 rounded-lg border border-border bg-card p-5"
          >
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
            <Link
              href={`/${locale}/access`}
              data-testid="pending-access-action"
              className="mt-4 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            >
              {dictionary.learning.pendingAccessAction}
            </Link>
          </section>
        ) : null}

        {dashboard.courses.length === 0 ? (
          <section aria-labelledby="learning-empty-title" className="rounded-2xl border border-dashed border-border bg-card px-6 py-14 text-center">
            <h2 id="learning-empty-title" className="font-display text-2xl font-bold text-foreground">{dictionary.learning.emptyTitle}</h2>
            <p className="mt-2 text-muted-foreground">{dictionary.learning.emptyBody}</p>
          </section>
        ) : (
          <section aria-label={dictionary.learning.dashboardTitle} className="grid gap-5 md:grid-cols-2">
            {dashboard.courses.map((course) => (
              <article key={course.course_id} className="rounded-2xl border border-border bg-card p-6 shadow-sm">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <h2 className="font-display text-2xl font-bold text-foreground">{course.title}</h2>
                  <LearningStatusBadge status={course.learning_status} label={learningStatusLabel(course.learning_status, dictionary.learning)} />
                </div>
                <div className="mt-5 space-y-2">
                  <LearningProgressSummary progress={course.progress} labels={progressLabels(dictionary.learning)} locale={locale} />
                  <AccessUntil expiresAt={course.expires_at} labels={accessLabels(dictionary.learning)} locale={locale} />
                </div>
                <Link
                  href={`/${locale}/learn/courses/${course.course_id}`}
                  className="mt-6 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                >
                  {dictionary.learning.openCourse}
                </Link>
              </article>
            ))}
          </section>
        )}
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
