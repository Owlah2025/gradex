"use client";

import * as React from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { InstitutionOption } from "@/lib/api/public-catalog";
import { institutionName } from "@/components/catalog/academic-filter-state";
import { LoadingState } from "@/components/common/loading-state";
import { Button } from "@/components/ui/button";
import { universityLogos } from "@/config/university-logos";
import { cn } from "@/lib/utils";
import type { OptionsState } from "./use-academic-options";

/**
 * The universities Gradex covers, offered as the way in.
 *
 * Two jobs, and the second is the one that matters. It answers "does Gradex know *my* university?"
 * before the visitor has pressed anything — the question a student actually arrives with — and it
 * is the shortcut: pressing an institution opens the academic card already past the first question,
 * on the major step.
 *
 * The list is the real one. It comes from the same `getPublicInstitutions` response the card itself
 * uses, so the strip cannot advertise a university the catalogue does not carry, and a new
 * institution appears here the moment the catalogue has it.
 */
export function UniversityStrip({
  institutions,
  language,
  copy,
  onSelect,
  onRetry,
  className,
}: {
  institutions: OptionsState<InstitutionOption>;
  language: "ar" | "en";
  copy: {
    title: string;
    loading: string;
    loadFailed: string;
    retry: string;
    noInstitutions: string;
    railPrevious: string;
    railNext: string;
  };
  onSelect: (slug: string) => void;
  onRetry: () => void;
  className?: string;
}) {
  return (
    <div
      data-testid="hero-academic-strip"
      className={cn(
        // A gradient foot rather than a panel.
        //
        // The strip sits at the bottom of the hero, which on a phone is where the lesson media is —
        // and a 13px title in white/75 over the bright player measures under 2:1. The band fades
        // the hero's own navy up behind the strip so the title has ground everywhere it can land,
        // without drawing a surface around it.
        "w-full bg-gradient-to-t from-gx-navy from-45% via-gx-navy/90 to-transparent pb-5 pt-14",
        className,
      )}
    >
      {/* The hero's own eyebrow colour, so the question reads as part of the brand's voice rather
          than as a caption under it. */}
      <p className="text-center font-display text-[13px] font-bold tracking-[0.01em] text-gx-blue-200">
        {copy.title}
      </p>

      <div className="mt-2.5 min-h-[3.75rem]">
        {institutions.kind === "loading" ? (
          <LoadingState
            label={copy.loading}
            testID="hero-academic-loading"
            className="flex min-h-[3.75rem] items-center justify-center py-0 text-center text-white/70"
          />
        ) : institutions.kind === "failed" ? (
          <div
            role="alert"
            data-testid="hero-academic-error"
            className="flex min-h-[3.75rem] flex-wrap items-center justify-center gap-x-3 gap-y-2 px-5 text-center text-sm text-white/70"
          >
            <span>{copy.loadFailed}</span>
            <Button
              type="button"
              variant="onDark"
              size="sm"
              onClick={onRetry}
              className="h-8 px-3 text-xs"
            >
              {copy.retry}
            </Button>
          </div>
        ) : institutions.items.length === 0 ? (
          <p
            role="status"
            data-testid="hero-academic-empty"
            className="flex min-h-[3.75rem] items-center justify-center px-5 text-center text-sm text-white/70"
          >
            {copy.noInstitutions}
          </p>
        ) : (
          <UniversityOptions
            institutions={institutions.items}
            language={language}
            onSelect={onSelect}
            copy={{ railPrevious: copy.railPrevious, railNext: copy.railNext }}
          />
        )}
      </div>
    </div>
  );
}

/**
 * How far from an edge still counts as being at it.
 *
 * Sub-pixel layout means a rail scrolled fully to one end routinely reports a
 * fractional gap, and comparing for equality leaves the control at that end
 * enabled with nothing left to reveal.
 */
const EDGE_TOLERANCE_PX = 1;

/** How long a press takes to move the rail one chip. */
const RAIL_STEP_DURATION_MS = 280;

/**
 * How far a chip is from the rail's leading edge, as a physical distance.
 *
 * "Leading" is whichever edge the reader's script starts from: the left in
 * English, the right in Arabic. The result is signed and physical, so adding it
 * to `scrollLeft` brings that chip to the front in either script — which is what
 * lets the rest of this component avoid deciding what `scrollLeft` means in a
 * right-to-left container, a question browsers do not answer the same way.
 */
function leadingOffset(rail: Element, chip: Element): number {
  const viewport = rail.getBoundingClientRect();
  const box = chip.getBoundingClientRect();
  return getComputedStyle(rail).direction === "rtl"
    ? box.right - viewport.right
    : box.left - viewport.left;
}

function UniversityOptions({
  institutions,
  language,
  onSelect,
  copy,
}: {
  institutions: InstitutionOption[];
  language: "ar" | "en";
  onSelect: (slug: string) => void;
  copy: { railPrevious: string; railNext: string };
}) {
  const railRef = React.useRef<HTMLDivElement | null>(null);
  /**
   * Which chip is against the rail's leading edge.
   *
   * Held rather than derived on demand, because the rail's own position cannot
   * answer the question while it is still moving: half way through a press no
   * chip is at the edge yet. It is re-read from the layout whenever the rail
   * comes to rest under its own steam.
   */
  const leadRef = React.useRef(0);
  /** The in-flight press, so a second one replaces it instead of fighting it. */
  const animationRef = React.useRef<number | null>(null);
  const [overflowing, setOverflowing] = React.useState(false);
  const [atLeftEnd, setAtLeftEnd] = React.useState(true);
  const [atRightEnd, setAtRightEnd] = React.useState(true);

  /**
   * Reads the rail's position from where its contents actually are on screen.
   *
   * Deliberately geometric rather than arithmetic. `scrollLeft` does not mean one
   * thing across browsers in a right-to-left container — some count up from the
   * visual left, others down from zero into negative numbers — so anything
   * derived from its value alone is a guess about the engine. Comparing the row's
   * own bounding box against the viewport's is true in both directions by
   * construction, and needs no branch on the language.
   */
  const measure = React.useCallback(() => {
    const rail = railRef.current;
    const row = rail?.firstElementChild;
    if (!rail || !row) return;
    const viewport = rail.getBoundingClientRect();
    const contents = row.getBoundingClientRect();
    setOverflowing(contents.width - viewport.width > EDGE_TOLERANCE_PX);
    setAtLeftEnd(contents.left >= viewport.left - EDGE_TOLERANCE_PX);
    setAtRightEnd(contents.right <= viewport.right + EDGE_TOLERANCE_PX);
  }, []);

  /** Which chip the rail is currently resting against, read from the layout. */
  const readLead = React.useCallback(() => {
    const rail = railRef.current;
    const row = rail?.firstElementChild;
    if (!rail || !row) return 0;
    let nearest = 0;
    let best = Infinity;
    [...row.children].forEach((chip, index) => {
      const offset = Math.abs(leadingOffset(rail, chip));
      if (offset < best) {
        best = offset;
        nearest = index;
      }
    });
    return nearest;
  }, []);

  React.useEffect(() => {
    const rail = railRef.current;
    if (!rail) return;
    measure();
    // Also re-reads which chip is in front after a scroll the reader performed
    // themselves — a swipe, a trackpad, a chip focused by keyboard. Done here
    // rather than on `scrollend`, which this element does not reliably emit.
    const onScroll = () => {
      measure();
      if (animationRef.current === null) leadRef.current = readLead();
    };
    rail.addEventListener("scroll", onScroll, { passive: true });
    // Fonts landing, the viewport changing, and the institution list arriving all
    // change whether this overflows, and none of them fire a scroll event.
    const observer = new ResizeObserver(measure);
    observer.observe(rail);
    if (rail.firstElementChild) observer.observe(rail.firstElementChild);
    return () => {
      rail.removeEventListener("scroll", onScroll);
      observer.disconnect();
      if (animationRef.current !== null) cancelAnimationFrame(animationRef.current);
    };
  }, [measure, readLead, institutions]);

  /**
   * Moves the rail one chip toward the given physical side.
   *
   * `toward` is a screen direction, not an inline one: -1 always reveals what is
   * further to the reader's left and +1 what is further to their right, in both
   * scripts.
   *
   * The movement is driven here rather than handed to the browser, because
   * neither way of asking survives this rail. `scrollIntoView` is pulled back by
   * mandatory snapping to wherever it started unless it lands exactly on a snap
   * point, and `scrollTo({behavior:"smooth"})` is snapped to a position of the
   * engine's choosing part way through. Both fail by doing nothing, which reads
   * as a broken control rather than as a bug.
   *
   * So snapping is suspended for the duration of the press and restored at the
   * end, where the rail is already resting exactly on a chip's edge and there is
   * nothing left for it to correct. Touch and trackpad scrolling are untouched
   * and keep snapping throughout.
   *
   * The distance is a physical delta between two rectangles, so it carries its
   * own direction and the arithmetic never has to know the script.
   */
  const step = React.useCallback(
    (toward: -1 | 1) => {
      const rail = railRef.current;
      const row = rail?.firstElementChild;
      if (!rail || !row) return;
      const chips = [...row.children];
      if (chips.length === 0) return;

      // A physical direction is not an index direction. The chips run from the
      // edge the script starts at, so in Arabic the rail begins on the right and
      // the control on the reader's left is the one that advances through the
      // list. Without this the left control sat at index zero asking for index
      // minus one, and did nothing at all.
      const rtl = getComputedStyle(rail).direction === "rtl";
      const index = Math.min(
        Math.max(leadRef.current + (rtl ? -toward : toward), 0),
        chips.length - 1,
      );
      leadRef.current = index;

      const from = rail.scrollLeft;
      const to = from + leadingOffset(rail, chips[index]);
      if (animationRef.current !== null) cancelAnimationFrame(animationRef.current);
      rail.style.scrollSnapType = "none";

      const started = performance.now();
      const tick = (now: number) => {
        const progress = Math.min(1, (now - started) / RAIL_STEP_DURATION_MS);
        // Ease-out cubic: quick to leave, gentle to arrive.
        const eased = 1 - Math.pow(1 - progress, 3);
        rail.scrollLeft = from + (to - from) * eased;
        if (progress < 1) {
          animationRef.current = requestAnimationFrame(tick);
          return;
        }
        animationRef.current = null;
        rail.style.scrollSnapType = "";
        measure();
      };
      animationRef.current = requestAnimationFrame(tick);
    },
    [measure],
  );

  return (
    // The three slots are placed physically, so the control on the reader's left
    // stays on the left when the page flips to Arabic. The rail inside keeps the
    // document's own direction, so the universities themselves still read in the
    // right order and the row still starts where the reader starts.
    <div
      dir="ltr"
      data-testid="hero-academic-rail"
      className="mx-auto flex w-full items-center gap-2 px-5 sm:max-w-[720px]"
    >
      <RailControl
        side="left"
        label={language === "ar" ? copy.railNext : copy.railPrevious}
        hidden={!overflowing}
        disabled={atLeftEnd}
        onPress={() => step(-1)}
      />

      <div
        ref={railRef}
        dir={language === "ar" ? "rtl" : "ltr"}
        className="flex min-w-0 flex-1 snap-x snap-mandatory overflow-x-auto [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {/* `mx-auto` centres a row that fits and is inert once it does not. */}
        <ul className="mx-auto flex items-center gap-2">
          {institutions.map((option) => (
            <li key={option.slug} className="shrink-0 snap-start">
              <UniversityChip
                option={option}
                language={language}
                onSelect={() => onSelect(option.slug)}
              />
            </li>
          ))}
        </ul>
      </div>

      <RailControl
        side="right"
        label={language === "ar" ? copy.railPrevious : copy.railNext}
        hidden={!overflowing}
        disabled={atRightEnd}
        onPress={() => step(1)}
      />
    </div>
  );
}

/**
 * One end of the rail.
 *
 * Absent rather than disabled when the rail does not overflow: a permanently
 * dead pair of arrows beside a row that fits is an offer the screen cannot
 * honour. It stays mounted but disabled at an end, because that end is reachable
 * again the moment the reader scrolls back.
 */
function RailControl({
  side,
  label,
  hidden,
  disabled,
  onPress,
}: {
  side: "left" | "right";
  label: string;
  hidden: boolean;
  disabled: boolean;
  onPress: () => void;
}) {
  if (hidden) return null;
  const Icon = side === "left" ? ChevronLeft : ChevronRight;
  return (
    <button
      type="button"
      onClick={onPress}
      disabled={disabled}
      aria-label={label}
      data-testid={`hero-academic-rail-${side}`}
      className={cn(
        // The chips' own vocabulary on the hero: a translucent white face over
        // the navy, not a new surface colour.
        "hidden size-11 shrink-0 place-items-center rounded-full border sm:grid",
        "border-white/20 bg-white/[0.08] text-white/85 supports-[backdrop-filter]:backdrop-blur-md",
        "transition-[background-color,border-color,color,opacity] duration-base ease-out-brand",
        "hover:border-white/35 hover:bg-white/15 hover:text-white",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-gx-navy",
        "disabled:pointer-events-none disabled:opacity-30",
      )}
    >
      <Icon className="size-5" aria-hidden />
    </button>
  );
}

function UniversityChip({
  option,
  language,
  onSelect,
}: {
  option: InstitutionOption;
  language: "ar" | "en";
  onSelect: () => void;
}) {
  const logo = universityLogos[option.slug];
  const name = institutionName(option, language);
  const mark = monogram(option.name_en);
  /**
   * A lettermark that repeats the label beside it is noise, not a mark.
   *
   * It happens when an institution's registered English name *is* its acronym, which the catalogue
   * is free to return — and in English the label is then the same string the mark would carry. The
   * chip drops the slot for that one case rather than printing "GUST GUST".
   */
  const showMark = logo !== undefined || mark.toLowerCase() !== name.trim().toLowerCase();

  return (
    <button
      type="button"
      onClick={onSelect}
      data-value={option.slug}
      className={cn(
        "group flex items-center gap-3 rounded-pill border py-2 pe-4 ps-2",
        // Brand blue rather than plain white glass. On the navy band a neutral chip reads as
        // chrome; carrying the ramp makes the row read as Gradex offering something.
        "border-gx-blue-500/30 bg-gx-blue-500/10 supports-[backdrop-filter]:backdrop-blur-md",
        "transition-[background-color,border-color] duration-base ease-out-brand",
        "hover:border-gx-blue-300/70 hover:bg-gx-blue-500/25",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-gx-navy",
      )}
    >
      {showMark ? (
      <span className="flex h-10 min-w-10 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-gx-blue-500/25 px-2">
        {logo ? (
          // Decorative: the institution's name sits beside it and is the accessible label already.
          // A plain <img>: these are small local vector marks in a 40px slot, so `next/image` would
          // add a loader and a layout wrapper to optimise artwork that is already optimal.
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={logo.src}
            alt=""
            aria-hidden
            className="size-full object-contain p-1"
            style={logo.scale ? { transform: `scale(${logo.scale})` } : undefined}
          />
        ) : (
          <span
            aria-hidden
            dir="ltr"
            className="font-display text-[10px] font-extrabold tracking-tight text-gx-blue-100"
          >
            {mark}
          </span>
        )}
      </span>
      ) : null}
      <span className="whitespace-nowrap font-display text-sm font-semibold text-white/90 transition-colors duration-base group-hover:text-white">
        {name}
      </span>
    </button>
  );
}

/** Words that carry no identity, so they never contribute an initial. */
const STOP_WORDS = new Set(["of", "the", "for", "and", "in", "at"]);

/**
 * A typographic stand-in for a logo Gradex does not have a licensed copy of.
 *
 * Built from the institution's own registered English name and nothing else — an abbreviation the
 * university already uses ("GUST", "AUK") is kept whole, and a full name is reduced to its
 * initials. It is deliberately a lettermark on a neutral surface rather than anything shaped like a
 * crest: inventing a mark for a real university would be worse than showing none.
 */
function monogram(nameEn: string): string {
  const words = nameEn.trim().split(/\s+/).filter(Boolean);
  // Already an acronym the institution goes by.
  if (words.length === 1) return words[0].slice(0, 5).toUpperCase();
  const initials = words
    .filter((word) => !STOP_WORDS.has(word.toLowerCase()))
    .map((word) => word[0])
    .join("");
  return initials.slice(0, 5).toUpperCase();
}
