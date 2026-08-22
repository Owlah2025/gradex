"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { getAcademicProfile, shouldPromptOnboarding } from "@/lib/api/academic-profile";
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
 */
export function AcademicProfilePrompt() {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [prompt, setPrompt] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const profile = await getAcademicProfile(locale);
        if (!cancelled) setPrompt(shouldPromptOnboarding(profile));
      } catch {
        // Personalisation is optional. A Student's dashboard must never degrade
        // because a discovery-only read failed.
        if (!cancelled) setPrompt(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [locale]);

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
