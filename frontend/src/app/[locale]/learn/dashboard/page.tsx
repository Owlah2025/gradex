import Link from "next/link";
import { LearningProgressSummary, LearningStatusBadge, AccessUntil, LearningUnavailable } from "@/components/learning/learning-views";
import { requestLearningDashboardServer } from "@/lib/api/learning-server";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";
import { LearningLocaleToggle } from "@/components/learning/learning-locale-toggle";

export const dynamic = "force-dynamic";
export const revalidate = 0;

export default async function LearningDashboardPage({ params }: { params: { locale: string } }) {
  const locale = params.locale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  try {
    const dashboard = await requestLearningDashboardServer(locale);
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
        </header>
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
                  <LearningStatusBadge status={course.learning_status} labels={dictionary.learning} />
                </div>
                <div className="mt-5 space-y-2">
                  <LearningProgressSummary progress={course.progress} labels={dictionary.learning} locale={locale} />
                  <AccessUntil expiresAt={course.expires_at} labels={dictionary.learning} locale={locale} />
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
          <LearningUnavailable labels={dictionary.learning} />
        </div>
      </main>
    );
  }
}
