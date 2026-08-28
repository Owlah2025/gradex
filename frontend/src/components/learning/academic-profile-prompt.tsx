"use client";

import Link from "next/link";
import { shouldPromptOnboarding } from "@/lib/api/academic-profile";
import { useAcademicContext } from "@/components/academic/academic-context-provider";
import { useLocale } from "@/lib/i18n/locale-provider";

/**
 * A dismissible invitation to complete the academic profile (D-092, T3).
 *
 * It renders only for a Student who has never made a decision. A Student who
 * chose to defer is NOT_STARTED no longer, so they are not asked again — which
 * is the whole reason SKIPPED is its own state.
 *
 * This is a card on a page the Student already reached. It blocks nothing, and
 * a failure to read the profile simply renders nothing.
 *
 * # WHERE THE PROFILE COMES FROM
 *
 * From the one place the application already holds it. `AcademicContextProvider` reads
 * `/me/academic-profile` once per authenticated session to decide whether an account's profile
 * outranks a browsing preference, and that read answers this question too: the setup state is the
 * same fact, from the same principal-scoped resource, in the same browser session. Asking for it a
 * second time on the Dashboard bought nothing but a second request.
 *
 * Reading it here rather than fetching is not a cache. The value is the provider's own state, held
 * for exactly as long as the session it belongs to — it is dropped the moment the session resolves
 * to anything but `AUTHENTICATED`, so it can never describe a different Student — and nothing here
 * writes to it or treats it as authority for anything but whether to show a card.
 *
 * A profile still in flight, a profile that could not be read, and a Student with no session all
 * arrive as `null`, and all three mean the same thing to this component: say nothing. That is the
 * same safe direction the local read took when it failed.
 */
export function AcademicProfilePrompt() {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const { profile } = useAcademicContext();
  const prompt = shouldPromptOnboarding(profile);

  if (!prompt) return null;

  return (
    <section
      data-testid="academic-profile-prompt"
      aria-labelledby="academic-profile-prompt-title"
      className="mb-8 rounded-lg border border-border bg-card p-5"
    >
      <h2 id="academic-profile-prompt-title" className="font-display text-lg font-bold text-foreground">
        {isAr ? "خلّينا نعرف دراستك" : "Tell us about your studies"}
      </h2>
      <p className="mt-2 text-muted-foreground">
        {isAr
          ? "أضف جامعتك وتخصصك ومستواك حتى نرتب لك الكتالوج. تقدر تتخطاها الآن."
          : "Add your university, major, and level so we can order the catalogue for you. You can skip for now."}
      </p>
      <Link
        href={`/${locale}/learn/academic-profile`}
        data-testid="academic-profile-prompt-action"
        className="mt-4 inline-flex rounded-md border border-border px-4 py-2 font-semibold text-foreground hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {isAr ? "أكمل ملفك الدراسي" : "Complete your academic profile"}
      </Link>
    </section>
  );
}
