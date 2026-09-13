"use client";

import * as React from "react";
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
          />
        )}
      </div>
    </div>
  );
}

function UniversityOptions({
  institutions,
  language,
  onSelect,
}: {
  institutions: InstitutionOption[];
  language: "ar" | "en";
  onSelect: (slug: string) => void;
}) {
  return (
    // `mx-auto` centres a short row but leaves an overflowing row reachable from its inline edge.
    <div className="flex snap-x snap-mandatory overflow-x-auto [-ms-overflow-style:none] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
      <ul className="mx-auto flex max-w-container items-center gap-2 px-5">
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
