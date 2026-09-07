"use client";

import * as React from "react";
import { GraduationCap, Pencil, X } from "lucide-react";
import { useAcademicContext } from "./academic-context-provider";
import { useAcademicOptions } from "./use-academic-options";
import { ChoiceChip, ChoiceGrid } from "./context-question";
import { UniversityStrip } from "./university-strip";
import {
  institutionName,
  programName,
} from "@/components/catalog/academic-filter-state";
import { useLandingJourney } from "@/components/landing/landing-journey";
import { PERSONALIZE_ANCHOR } from "@/components/landing/anchors";
import {
  academicContext,
  academicContextNames,
  type AnonymousAcademicContext,
} from "@/lib/academic/anonymous-context";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { cn } from "@/lib/utils";

/**
 * The academic question, asked from inside the hero without taking it over.
 *
 * ## Closed by default
 *
 * The landing page loads with the hero sharp and nothing on top of it but one small control at the
 * bottom edge. The question is offered, not imposed: a visitor who came to find out what Gradex is
 * gets to read the headline first, and the onboarding is one press away when they want it.
 *
 * ## Layered, never in flow
 *
 * Everything here is absolutely positioned inside the hero. The trigger, the focus layer and the
 * card add no height, so the hero's geometry is byte-for-byte identical whether the card is closed,
 * open or resolved, and opening it cannot shift a single line of the page.
 *
 * ## The focus effect, and its scope
 *
 * Opening softly defocuses the hero — a 7px backdrop blur and a 20% tint, which is a change of
 * focal plane rather than a scrim. The hero stays plainly visible behind it. The layer is a child
 * of the hero section, so the header above and every section below stay sharp; nothing outside this
 * band is touched.
 *
 * ## What it owns, and what it does not
 *
 * It owns which question is on screen and whether the card is open. It owns nothing else. The
 * option lists come from the public catalogue endpoints through `useAcademicOptions`, the resolved
 * context goes to `AcademicContextProvider` — the same store the catalogue filter row and the
 * courses strip already read — and persistence is that provider's existing device-local storage.
 */

/** Beyond this the grid outgrows the card, so the rest go behind "Show more". */
const UNIVERSITIES_BEFORE_FOLD = 6;

export function HeroAcademicPrompt({ onResolved }: { onResolved?: () => void }) {
  const { locale, t } = useLocale();
  const language = locale as "ar" | "en";
  const copy = t.academicContext;
  const { status, anonymous, profile, source, setAnonymous } = useAcademicContext();
  const journey = useLandingJourney();

  const [open, setOpen] = React.useState(false);
  const [institutionSlug, setInstitutionSlug] = React.useState("");
  const [expanded, setExpanded] = React.useState(false);
  const [advanced, setAdvanced] = React.useState(false);
  const triggerRef = React.useRef<HTMLButtonElement>(null);
  /**
   * Whatever opened the card, so closing can hand focus straight back to it.
   *
   * Not the trigger ref: in the default state the card is opened from one of several university
   * chips in the strip, and returning focus to the start of the row rather than to the chip the
   * reader actually pressed loses their place in it.
   */
  const openerRef = React.useRef<HTMLElement | null>(null);
  const cardRef = React.useRef<HTMLDivElement>(null);

  const { institutions, programs, retryInstitutions, retryPrograms } =
    useAcademicOptions(language, institutionSlug);

  // One university is not a question. Derived from the response rather than from a hardcoded slug,
  // so a second university turns the first step back into a real choice with no code change.
  React.useEffect(() => {
    if (!open || institutions.kind !== "ready") return;
    setInstitutionSlug((current) =>
      current === "" && institutions.items.length === 1
        ? institutions.items[0].slug
        : current,
    );
  }, [open, institutions]);

  /**
   * Opens the card, optionally already past the first question.
   *
   * The strip below passes a slug: pressing a university there *is* answering "which university",
   * so re-asking it inside the card would be the interface ignoring what the reader just told it.
   */
  const openCard = React.useCallback((preselect = "") => {
    openerRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setOpen(true);
    setInstitutionSlug(preselect);
    setAdvanced(preselect !== "");
    setExpanded(false);
  }, []);

  /** Closes without changing the context, and hands focus back to the control that opened it. */
  const close = React.useCallback(() => {
    setOpen(false);
    // `isConnected` because the opener may have been the strip, which the resolved state replaces.
    const opener = openerRef.current;
    if (opener?.isConnected) opener.focus();
    else triggerRef.current?.focus();
  }, []);

  // Escape closes. Bound only while open, so the key is not stolen from the rest of the page.
  React.useEffect(() => {
    if (!open) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.stopPropagation();
        close();
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [open, close]);

  // Focus moves into the question that is waiting, so the card is reachable without hunting for it.
  React.useEffect(() => {
    if (!open) return;
    cardRef.current?.querySelector<HTMLElement>('[role="group"] button')?.focus();
  }, [open, institutions.kind, programs.kind, institutionSlug]);

  // The courses strip below carries a Change control; this is what it reaches. A counter rather
  // than a flag because asking twice in a row has to work.
  const editRequests = journey?.editRequests ?? 0;
  React.useEffect(() => {
    if (editRequests === 0) return;
    openCard();
  }, [editRequests, openCard]);

  const readyInstitutions =
    institutions.kind === "ready" ? institutions.items : [];

  const chosen =
    institutions.kind === "ready"
      ? institutions.items.find((item) => item.slug === institutionSlug)
      : undefined;

  function resolve(program: { slug: string; name_ar: string; name_en: string } | null) {
    if (institutionSlug === "") return;
    // Both languages are cached together: the identity is the slug pair and has to survive a locale
    // switch, so a single-language label cache would be discarded at exactly that moment.
    const next: AnonymousAcademicContext = academicContext(
      institutionSlug,
      program?.slug ?? "",
      {
        institutionAr: chosen?.name_ar ?? "",
        institutionEn: chosen?.name_en ?? "",
        programAr: program?.name_ar ?? "",
        programEn: program?.name_en ?? "",
      },
    );
    setAnonymous(next);
    setOpen(false);
    onResolved?.();
  }

  // A signed-in Student's own profile outranks anything this could ask for, and it is not this
  // component's to overwrite. It offers nothing to them at all.
  if (source === "profile" && profile) return null;
  if (status === "loading") return null;

  const resolved = source === "anonymous" ? anonymous : null;
  const names = resolved ? academicContextNames(resolved, language) : null;

  return (
    <>
      {/**
       * The focus layer.
       *
       * A restrained blur with a light navy tint, not a scrim: the hero reads clearly through it,
       * and it is the card in front that becomes the sharp plane. Absolutely positioned inside the
       * hero, so the header and every section below are untouched.
       */}
      <div
        aria-hidden
        onClick={close}
        className={cn(
          "absolute inset-0 z-20 bg-gx-navy/20 transition-opacity duration-slow ease-out-brand",
          "supports-[backdrop-filter]:backdrop-blur-[7px]",
          open ? "opacity-100" : "pointer-events-none opacity-0",
        )}
      />

      {/* The card. Centred in both writing directions — only its contents flip. */}
      {open ? (
        <div className="pointer-events-none absolute inset-0 z-30 flex items-center justify-center px-5 py-20">
          <div
            ref={cardRef}
            data-testid="hero-academic-prompt"
            className={cn(
              "pointer-events-auto w-full max-w-[30rem] rounded-xl border border-white/20 bg-white/[0.10] p-5 shadow-lg",
              "supports-[backdrop-filter]:backdrop-blur-2xl",
              "motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2 motion-safe:duration-base motion-safe:ease-out-brand",
            )}
          >
            <div className="mb-3.5 flex items-start justify-between gap-3">
              {chosen ? (
                <span
                  data-testid="academic-picker-institution"
                  className="flex min-w-0 items-center gap-2 text-[14px]"
                >
                  <GraduationCap className="size-4 shrink-0 text-gx-orange" aria-hidden />
                  <span className="min-w-0 truncate font-display font-bold text-white">
                    {institutionName(chosen, language)}
                  </span>
                </span>
              ) : (
                <span />
              )}
              <button
                type="button"
                onClick={close}
                aria-label={copy.closePrompt}
                data-testid="hero-academic-close"
                className="-me-1 -mt-1 shrink-0 rounded-md p-1 text-white/60 transition-colors duration-base hover:bg-white/10 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-gx-navy"
              >
                <X className="size-4" aria-hidden />
              </button>
            </div>

            {institutions.kind === "loading" ? (
              <LoadingState
                label={copy.loading}
                testID="academic-picker-loading"
                className="py-2 text-white/70"
              />
            ) : institutions.kind === "failed" ? (
              <ErrorState
                testID="academic-picker-error"
                title={copy.loadFailed}
                retryLabel={copy.retry}
                onRetry={retryInstitutions}
              />
            ) : institutions.items.length === 0 ? (
              <p role="status" className="text-sm text-white/70" data-testid="academic-picker-empty">
                {copy.noInstitutions}
              </p>
            ) : chosen ? (
              <Question
                id="hero-academic-program"
                text={copy.programQuestion}
                appear={advanced}
                testID="academic-picker-program"
              >
                {programs.kind === "loading" ? (
                  <LoadingState
                    label={copy.loadingPrograms}
                    testID="academic-picker-programs-loading"
                    className="py-1 text-white/70"
                  />
                ) : programs.kind === "failed" ? (
                  <ErrorState
                    testID="academic-picker-programs-error"
                    title={copy.programsFailed}
                    retryLabel={copy.retry}
                    onRetry={retryPrograms}
                  />
                ) : (
                  <ChoiceGrid compact>
                    {programs.kind === "ready" &&
                      programs.items.map((option) => (
                        <ChoiceChip
                          key={option.slug}
                          value={option.slug}
                          tone="onDark"
                          size="compact"
                          onSelect={() => resolve(option)}
                        >
                          {programName(option, language)}
                        </ChoiceChip>
                      ))}
                    {/* A university on its own already narrows the catalogue, so "not sure"
                        completes rather than dead-ends. */}
                    <ChoiceChip tone="onDark" size="compact" onSelect={() => resolve(null)}>
                      {copy.anyProgramChoice}
                    </ChoiceChip>
                  </ChoiceGrid>
                )}
              </Question>
            ) : (
              <Question
                id="hero-academic-university"
                text={copy.universityQuestion}
                testID="academic-picker-institution"
              >
                <ChoiceGrid single>
                  {(expanded
                    ? institutions.items
                    : institutions.items.slice(0, UNIVERSITIES_BEFORE_FOLD)
                  ).map((option) => (
                    <ChoiceChip
                      key={option.slug}
                      value={option.slug}
                      tone="onDark"
                      size="compact"
                      onSelect={() => {
                        setInstitutionSlug(option.slug);
                        setAdvanced(true);
                      }}
                    >
                      {institutionName(option, language)}
                    </ChoiceChip>
                  ))}
                </ChoiceGrid>
                {!expanded && institutions.items.length > UNIVERSITIES_BEFORE_FOLD ? (
                  <SubtleButton onClick={() => setExpanded(true)} className="mt-2.5">
                    {copy.showMore}
                  </SubtleButton>
                ) : null}
              </Question>
            )}

            {institutions.kind === "ready" && institutions.items.length > 0 ? (
              <SubtleButton onClick={close} className="mt-4" testID="hero-academic-skip">
                {copy.skipForNow}
              </SubtleButton>
            ) : null}
          </div>
        </div>
      ) : null}

      {/**
       * The entry point, anchored to the hero's bottom edge and centred in both writing directions.
       *
       * `inset-x-0` with a centring row rather than a start/end offset, because this belongs to the
       * whole band and not to the copy column — so it does not swap sides in Arabic.
       *
       * `z-10`, deliberately below the focus layer: when the card opens, this defocuses along with
       * the rest of the hero, leaving the card as the only sharp plane. It carries the anchor id
       * too, because unlike the card it is always on the page for `#personalize` to reach.
       */}
      <div
        id={PERSONALIZE_ANCHOR}
        className={cn(
          /**
           * `z-15`: above the hero's copy column, below the focus layer. Both halves matter.
           *
           * The copy column is `z-10` and stretches the full height of the hero — it is the flex
           * child that grows — so it sits over this band. At an equal `z-10` the column won every
           * hit test and the strip, which rendered and looked correct, could not be pressed at all.
           * Ordering does not settle it either: these are positioned elements with an explicit
           * `z-index`, which are sorted by that index before anything else is consulted.
           *
           * It stops at 15 rather than going above the `z-20` focus layer, because a control that
           * outranks that layer stays sharp while the hero defocuses behind the open card.
           */
          "pointer-events-none absolute inset-x-0 z-[15] flex justify-center",
          // Anchored to the bottom of the hero where the hero fits the screen, and to the bottom of
          // the *first screenful* where it does not. On a phone the hero runs past the viewport —
          // the media sits below the copy — so an entry point pinned to the section's own bottom
          // edge would load below the fold, which is exactly where it must not be.
          "bottom-auto top-[calc(100svh-14.5rem)]",
          "lg:bottom-0 lg:top-auto",
        )}
      >
        {resolved && names ? (
          // The same lift the strip carries. These two occupy one slot in one band, and the
          // resolved control was left against the hero's bottom edge when the strip came up off it.
          <div className="px-5 pb-6 lg:pb-20">
            <button
              ref={triggerRef}
              type="button"
              onClick={() => openCard()}
              data-testid="hero-academic-trigger"
              aria-expanded={open}
              aria-label={copy.changeAria}
              className={cn(
                "pointer-events-auto inline-flex max-w-full items-center gap-2 rounded-pill border px-4 py-2",
                "font-display text-[13px] font-semibold",
                "border-white/20 bg-white/[0.08] text-white/85 shadow-md supports-[backdrop-filter]:backdrop-blur-md",
                "transition-[background-color,border-color,color] duration-base ease-out-brand",
                "hover:border-white/35 hover:bg-white/15 hover:text-white",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-gx-navy",
              )}
            >
              <GraduationCap className="size-4 shrink-0 text-gx-orange" aria-hidden />
              <span data-testid="academic-context-names" className="min-w-0 truncate">
                {names.institution || resolved.institutionSlug}
                {names.program === "" ? "" : ` · ${names.program}`}
              </span>
              <Pencil className="size-3.5 shrink-0 text-white/60" aria-hidden />
            </button>
          </div>
        ) : (
          <div className="pointer-events-auto w-full">
            <UniversityStrip
              institutions={readyInstitutions}
              language={language}
              title={copy.universityQuestion}
              onSelect={(slug) => openCard(slug)}
              // Lifted clear of the hero's bottom edge, where it read as a footer rather than as
              // part of the offer. The padding rather than an offset, so the gradient behind it
              // still runs to the edge instead of ending in a visible seam.
              className="lg:pb-20"
            />
          </div>
        )}
      </div>
    </>
  );
}

function Question({
  id,
  text,
  appear = false,
  testID,
  children,
}: {
  id: string;
  text: string;
  appear?: boolean;
  testID: string;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        appear &&
          "motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2 motion-safe:duration-base motion-safe:ease-out-brand",
      )}
    >
      <p id={id} className="font-display text-[16px] font-bold text-white">
        {text}
      </p>
      <div role="group" aria-labelledby={id} data-testid={testID} className="mt-3.5">
        {children}
      </div>
    </div>
  );
}

/**
 * Every secondary action in this component, in one shape.
 *
 * Deliberately not a `Button`: the shared variants are sized and weighted to read as calls to
 * action, and skip and show-more are the opposite of that.
 */
function SubtleButton({
  onClick,
  children,
  className,
  testID,
}: {
  onClick: () => void;
  children: React.ReactNode;
  className?: string;
  testID?: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={testID}
      className={cn(
        "rounded-sm font-display text-[13px] font-semibold text-white/65 underline-offset-4",
        "transition-colors duration-base hover:text-white hover:underline",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-gx-navy",
        className,
      )}
    >
      {children}
    </button>
  );
}
