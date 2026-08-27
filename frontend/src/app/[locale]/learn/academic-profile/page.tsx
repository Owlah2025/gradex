import { AcademicProfileForm } from "@/components/learning/academic-profile-form";
import { LearningShell } from "@/components/learning/learning-shell";
import { shellLabels } from "@/components/learning/learning-label-sets";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";

/**
 * Student academic onboarding (D-092, T3).
 *
 * A normal Student page reached by an invitation from the dashboard. It is
 * deliberately NOT a route guard and NOT middleware: nothing redirects a
 * Student here, so invitation acceptance, Course access, and protected media
 * are untouched by onboarding state.
 *
 * It sits in the Student frame like every other Student screen. It used to be a
 * bare `<main>` — no header, no way back to their Courses, no language or theme
 * control, and no way to sign out. A Student who followed the dashboard's
 * invitation here and then changed their mind had the browser's Back button and
 * nothing else.
 */
export default async function AcademicOnboardingPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: requestedLocale } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  const dictionary = locale === "ar" ? ar : en;
  return (
    <LearningShell
      locale={locale}
      dir={locale === "ar" ? "rtl" : "ltr"}
      labels={shellLabels(dictionary)}
    >
      <div className="mx-auto max-w-3xl px-5 py-10 sm:px-6">
        <AcademicProfileForm mode="onboarding" />
      </div>
    </LearningShell>
  );
}
