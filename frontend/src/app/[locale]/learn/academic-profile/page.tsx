import { AcademicProfileForm } from "@/components/learning/academic-profile-form";

/**
 * Student academic onboarding (D-092, T3).
 *
 * A normal Student page reached by an invitation from the dashboard. It is
 * deliberately NOT a route guard and NOT middleware: nothing redirects a
 * Student here, so invitation acceptance, Course access, and protected media
 * are untouched by onboarding state.
 */
export default function AcademicOnboardingPage() {
  return (
    <main id="main" className="mx-auto min-h-screen max-w-3xl px-5 py-10 sm:px-6">
      <AcademicProfileForm mode="onboarding" />
    </main>
  );
}
