import { AcademicProfileForm } from "@/components/learning/academic-profile-form";
import { LearningShell } from "@/components/learning/learning-shell";
import { shellLabels } from "@/components/learning/learning-label-sets";
import { ar } from "@/lib/i18n/dictionaries/ar";
import { en } from "@/lib/i18n/dictionaries/en";

/** The Student's own profile surface, where the academic profile can be edited later. */
export default async function StudentProfilePage({
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
        <AcademicProfileForm mode="edit" />
      </div>
    </LearningShell>
  );
}
