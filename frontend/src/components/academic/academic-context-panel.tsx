"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Section, SectionHeader } from "@/components/layout/section";
import { AcademicContextPicker } from "./academic-context-picker";
import { AcademicContextSummary } from "./academic-context-summary";
import { useAcademicContext } from "./academic-context-provider";
import { catalogueHrefForContext } from "./catalogue-context";
import {
  academicContextNames,
  type AnonymousAcademicContext,
} from "@/lib/academic/anonymous-context";
import { useLocale } from "@/lib/i18n/locale-provider";
import { LoadingState } from "@/components/common/loading-state";

/**
 * The landing page's academic personalisation slot (Screen 1, Public).
 *
 * ## Why an inline section and not a first-visit dialog
 *
 * A modal is the obvious pattern and the wrong one here. It interrupts a visitor who arrived to
 * find out what Gradex *is*, it has to be dismissible — so it cannot be the only route to the
 * feature anyway — and on a 390px screen it covers the page that was about to answer their
 * question. An inline section under the hero is discoverable at the moment the reader has started
 * scrolling for courses, costs nothing to ignore, needs no focus trap, and is naturally usable on a
 * phone. It also carries its own returning state, so a visitor who has already chosen sees their
 * context confirmed rather than the question repeated.
 *
 * ## The three states
 *
 * A signed-in Student with a completed academic profile sees that profile, and is sent to the
 * profile editor to change it: the account's answer outranks anything a browser is holding, and
 * offering to overwrite it from here would be the wrong direction. Everyone else either has a
 * browsing preference — shown, with both exits — or does not, and is asked.
 */
export function AcademicContextPanel() {
  const router = useRouter();
  const { locale, t } = useLocale();
  const language = locale as "ar" | "en";
  const copy = t.academicContext;
  const { status, anonymous, profile, source, setAnonymous, clearAnonymous } =
    useAcademicContext();
  const [editing, setEditing] = React.useState(false);

  function apply(context: AnonymousAcademicContext) {
    setAnonymous(context);
    setEditing(false);
    router.push(catalogueHrefForContext(language, context));
  }

  const heading = (
    <SectionHeader
      eyebrow={copy.eyebrow}
      title={copy.title}
      lead={copy.lead}
      headingId="academic-context-title"
    />
  );

  if (status === "loading") {
    return (
      <Section
        id="academic-context"
        spacing="tight"
        aria-labelledby="academic-context-title"
      >
        {heading}
        <LoadingState label={copy.loading} />
      </Section>
    );
  }

  // The account's own answer. Its institution has no public slug on this contract, so it is
  // displayed and linked to the editor — never turned into a browsing preference behind the
  // Student's back.
  if (source === "profile" && profile) {
    return (
      <Section
        id="academic-context"
        spacing="tight"
        aria-labelledby="academic-context-title"
      >
        <SectionHeader
          eyebrow={copy.eyebrow}
          title={copy.summaryTitle}
          headingId="academic-context-title"
        />
        <AcademicContextSummary
          testID="academic-context-panel-summary"
          institution={profile.institution_name ?? ""}
          program={profile.program_name ?? ""}
          provenance={copy.profileBacked}
          changeHref={`/${language}/learn/academic-profile`}
        />
      </Section>
    );
  }

  if (source === "anonymous" && anonymous && !editing) {
    const names = academicContextNames(anonymous, language);
    return (
      <Section
        id="academic-context"
        spacing="tight"
        aria-labelledby="academic-context-title"
      >
        <SectionHeader
          eyebrow={copy.eyebrow}
          title={copy.summaryTitle}
          headingId="academic-context-title"
        />
        <AcademicContextSummary
          testID="academic-context-panel-summary"
          institution={names.institution || anonymous.institutionSlug}
          program={names.program}
          provenance={copy.savedOnDevice}
          onChange={() => setEditing(true)}
          onClear={clearAnonymous}
        />
      </Section>
    );
  }

  return (
    <Section
      id="academic-context"
      spacing="tight"
      aria-labelledby="academic-context-title"
    >
      {heading}
      {/**
       * On the base surface, not the muted section tone.
       *
       * `muted-foreground` measures 4.2:1 against `gx-blue-50` — below AA — so every secondary line
       * here (the lead, the "Optional" hint, the "no account needed" note) failed contrast on the
       * washed background. It meets AA on the page background, so the separation comes from a
       * bordered card instead of a background wash and all three lines pass.
       *
       * The same measurement applies to the existing muted-tone sections, which is a design-system
       * question rather than one this panel can answer; it is recorded for that work rather than
       * fixed by changing a shared token from here.
       */}
      <div className="max-w-2xl rounded-2xl border border-border bg-card p-6 sm:p-7">
        <AcademicContextPicker
          idPrefix="landing-academic"
          initial={anonymous}
          submitLabel={copy.submit}
          onSubmit={apply}
          onSkip={() => {
            // Skipping is not the same as choosing nothing: a visitor who was mid-edit keeps what
            // they already had, and one who never chose is taken to the unfiltered catalogue.
            setEditing(false);
            if (!anonymous) router.push(`/${language}/catalog`);
          }}
          skipLabel={copy.skip}
          autoFocus={editing}
        />
        <p className="mt-4 text-sm text-muted-foreground">{copy.notAnAccount}</p>
      </div>
    </Section>
  );
}
