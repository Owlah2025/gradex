"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { Container } from "@/components/layout/container";
import { Eyebrow, SectionHeading } from "@/components/ui/typography";
import { AcademicContextPicker } from "./academic-context-picker";
import { AcademicContextSummary } from "./academic-context-summary";
import { useAcademicContext } from "./academic-context-provider";
import { useLandingJourney } from "@/components/landing/landing-journey";
import {
  academicContextNames,
  type AnonymousAcademicContext,
} from "@/lib/academic/anonymous-context";
import { useLocale } from "@/lib/i18n/locale-provider";
import { LoadingState } from "@/components/common/loading-state";
import { cn } from "@/lib/utils";

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
 * ## What completing it does now
 *
 * It used to navigate to `/[locale]/catalog?institution=…`, which threw away the page the reader
 * was on to show them a list they were three seconds of scrolling away from. The courses section
 * directly below already narrows itself by this exact context, through the same catalogue client
 * and the same filter helpers, so completing the questions now sets the context and takes the
 * reader *down* to those results instead of away to another route.
 *
 * Nothing about the shareable, filtered catalogue URL was lost: it is what "Browse all courses"
 * under the results points at, and it is still what the stored context resolves to everywhere else
 * in the product.
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
  const journey = useLandingJourney();
  const [editing, setEditing] = React.useState(false);
  /**
   * Announced, not drawn.
   *
   * The courses section renders its own visible loading state, so a second spinner here would draw
   * one wait twice. What a screen-reader user has instead is nothing at all — the results are a
   * scroll away and off-screen — so the resolution is announced politely from the surface that
   * caused it.
   */
  const [resolution, setResolution] = React.useState("");

  const requests = journey?.editRequests ?? 0;
  React.useEffect(() => {
    // Zero is the initial value, not a request. Reopening on it would show the questions to every
    // returning visitor on first paint.
    if (requests > 0) setEditing(true);
  }, [requests]);

  /**
   * Counts completions, so the hand-off below can react to a *repeated* one.
   *
   * A visitor who changes their mind and picks a second program produces the same context shape as
   * the first time; keyed on the context itself, the second completion would commit and go nowhere.
   */
  const [completions, setCompletions] = React.useState(0);

  function apply(context: AnonymousAcademicContext) {
    setAnonymous(context);
    setEditing(false);
    setResolution(copy.resolving);
    setCompletions((count) => count + 1);
  }

  /**
   * The hand-off, after the commit that earned it.
   *
   * In an effect rather than in `apply` because the section being scrolled to is re-rendering from
   * the same update: it is retitling itself and re-requesting a narrowed list. Scrolling before
   * that commit lands on the page as it was.
   *
   * Where there is no landing journey — the panel is a client component and could be mounted on a
   * page with no courses below it — the filtered catalogue remains the honest destination.
   */
  React.useEffect(() => {
    if (completions === 0) return;
    if (journey) journey.scrollToCourses();
    else router.push(`/${language}/catalog`);
    // `journey` is a stable memo and `router` is stable; the completion counter is the trigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [completions]);

  /** Still deciding, or reopened to change the answer: the questions are on screen. */
  const asking = status !== "ready" || source === "none" || editing;

  const resolved = () => {
    // The account's own answer. Its institution has no public slug on this contract, so it is
    // displayed and linked to the editor — never turned into a browsing preference behind the
    // Student's back.
    if (source === "profile" && profile) {
      return (
        <AcademicContextSummary
          testID="academic-context-panel-summary"
          institution={profile.institution_name ?? ""}
          program={profile.program_name ?? ""}
          provenance={copy.profileBacked}
          changeHref={`/${language}/learn/academic-profile`}
        />
      );
    }
    if (source === "anonymous" && anonymous) {
      const names = academicContextNames(anonymous, language);
      return (
        <AcademicContextSummary
          testID="academic-context-panel-summary"
          institution={names.institution || anonymous.institutionSlug}
          program={names.program}
          provenance={copy.savedOnDevice}
          onChange={() => setEditing(true)}
          onClear={clearAnonymous}
        />
      );
    }
    return null;
  };

  return (
    /**
     * A section, not an overlay.
     *
     * This was briefly a full-viewport navy scrim with a backdrop blur, dimming the hero to 20% and
     * carrying white type over it. That is a modal, and it read as one: the hero stopped being the
     * section above and became a darkened backdrop, which is the opposite of the continuous journey
     * this is meant to be.
     *
     * So it is a surface of its own again — the page background, a top edge, the shared radius and
     * shadow — and the relationship to the hero is carried entirely by the scroll composition
     * around it. `SectionStack` pins the hero while this rises and covers it; nothing here has to
     * reach up and dim anything for that to read as depth.
     *
     * The only translucency left is on the card inside, where it belongs: a raised surface at 80%
     * over the section it sits on, which is the restrained version of the same idea.
     */
    <section
      id="academic-context"
      aria-labelledby="academic-context-title"
      className="flex flex-col justify-center overflow-hidden rounded-t-xl border-t border-border bg-background py-14 shadow-lg md:py-20 lg:min-h-[calc(100svh-4rem)]"
    >
      {/**
       * Two columns where there is room for two, one where there is not.
       *
       * The question sits opposite the thing that answers it, which is the composition the hero
       * above already establishes — so the two sections read as one page. As a single left-aligned
       * column this left roughly a third of the container empty on a wide screen.
       */}
      <Container className="lg:grid lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1fr)] lg:items-center lg:gap-14 xl:gap-20">
        <div className="max-w-2xl lg:max-w-none">
          <Eyebrow>{copy.eyebrow}</Eyebrow>
          <SectionHeading id="academic-context-title" className="mt-3">
            {asking ? copy.title : copy.summaryTitle}
          </SectionHeading>
          {asking ? (
            <p className="mt-3.5 text-[clamp(1.0625rem,1.6vw,1.25rem)] leading-relaxed text-muted-foreground">
              {copy.lead}
            </p>
          ) : null}
        </div>

        {/**
         * The interactive surface.
         *
         * A raised card rather than a background wash, and that is a contrast decision rather than
         * a stylistic one: `muted-foreground` measures 4.2:1 against the `gx-blue-50` section tone,
         * under AA, so every secondary line here would fail on a tinted band. It meets AA on the
         * card.
         *
         * `supports-[backdrop-filter]` keeps the glass honest — without the blur the surface goes
         * fully opaque rather than leaving text on a semi-transparent panel with nothing diffusing
         * what is behind it.
         */}
        {/**
         * The questions get the raised surface; the answer does not.
         *
         * `AcademicContextSummary` is already a bordered card, so wrapping it in this one produced a
         * card inside a card — two borders, and the names squeezed into a third of the width beside
         * the controls, wrapping "Kuwait University · Computer Engineering" onto three lines with
         * empty space either side of it. Once there is nothing to fill in, there is nothing for the
         * panel to be, so the summary stands on its own.
         */}
        <div
          className={cn(
            "mt-8 max-w-2xl lg:mt-0 lg:max-w-none",
            asking &&
              "rounded-xl border border-border bg-card p-6 shadow-md supports-[backdrop-filter]:bg-card/80 supports-[backdrop-filter]:backdrop-blur-xl sm:p-7",
          )}
        >
          {status === "loading" ? (
            <LoadingState label={copy.loading} />
          ) : asking ? (
            <>
              <AcademicContextPicker
                idPrefix="landing-academic"
                initial={anonymous}
                onResolve={apply}
                onSkip={() => {
                  // Skipping is not the same as choosing nothing: a visitor who was mid-edit keeps
                  // what they already had, and one who never chose is shown the unnarrowed list
                  // below rather than being sent off the page.
                  setEditing(false);
                  if (!anonymous && journey) journey.scrollToCourses();
                  else if (!anonymous) router.push(`/${language}/catalog`);
                }}
                skipLabel={copy.skip}
                autoFocus={editing}
              />
              <p className="mt-6 text-sm text-muted-foreground">{copy.notAnAccount}</p>
            </>
          ) : (
            resolved()
          )}
        </div>
      </Container>

      <p role="status" aria-live="polite" className="sr-only">
        {resolution}
      </p>
    </section>
  );
}
