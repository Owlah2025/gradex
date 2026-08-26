"use client";

import { useId, useState } from "react";
import { ChevronDown } from "lucide-react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { PublicCourseDetail } from "@/lib/api/public-catalog";
import { Prose } from "@/components/ui/typography";
import {
  curriculumTotals,
  outlineNeedsDisclosure,
  visibleSections,
} from "./course-detail-presentation";

/**
 * The course outline, at the only depth the public contract supports.
 *
 * `GET /api/v1/catalog/courses/{idOrSlug}` returns each section's title, position and lesson count
 * and stops there — no lesson titles, no lesson types, no per-lesson preview flag, no durations.
 * That is why this is a list and not an accordion: a section that expands to reveal nothing is a
 * control that lies about having content behind it, and inventing lesson rows to fill it would put
 * fabricated curriculum on the page a student uses to judge what they are getting.
 *
 * The disclosure that does exist is over real data — a long outline lists its first
 * `SECTION_PREVIEW_LIMIT` sections and offers the rest — so the control always has something to
 * show.
 */
export function CourseCurriculum({
  sections,
  copy,
  headingLabel,
  lessonsUnit,
}: {
  sections: PublicCourseDetail["sections"];
  copy: Dictionary["courseDetail"];
  /** The catalogue's own name for this section, shared with the list. */
  headingLabel: string;
  /** The catalogue's own plural noun for lessons, shared with the list. */
  lessonsUnit: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const listID = useId();
  const totals = curriculumTotals(sections);
  const disclosable = outlineNeedsDisclosure(sections);
  const shown = visibleSections(sections, expanded);

  return (
    <section
      className="mt-12"
      aria-labelledby="outline"
      data-testid="course-curriculum"
    >
      <h2 id="outline" className="font-display text-2xl font-bold text-foreground">
        {headingLabel}
      </h2>

      {sections.length === 0 ? (
        <Prose className="mt-3 text-[15.5px]" data-testid="course-curriculum-empty">
          {copy.emptyCurriculum}
        </Prose>
      ) : (
        <>
          {/* Inline text rather than a definition list: the number reads before its noun in both
              languages, and no CSS ordering is needed to get there. */}
          <p
            className="mt-3 flex flex-wrap items-baseline gap-x-6 gap-y-1 text-sm text-muted-foreground"
            data-testid="course-curriculum-totals"
          >
            <span>
              <span className="font-display text-base font-bold text-foreground">
                {totals.sections}
              </span>{" "}
              {copy.sectionsLabel}
            </span>
            <span>
              <span className="font-display text-base font-bold text-foreground">
                {totals.lessons}
              </span>{" "}
              {copy.lessonsLabel}
            </span>
          </p>

          <ol
            id={listID}
            className="mt-5 divide-y divide-border overflow-hidden rounded-lg border border-border bg-card"
          >
            {shown.map((section, index) => (
              <li
                key={section.position}
                className="flex flex-wrap items-baseline justify-between gap-x-5 gap-y-1 px-5 py-4"
                data-testid="course-curriculum-section"
              >
                <p className="flex min-w-0 flex-1 flex-col gap-1">
                  <span className="text-xs font-semibold uppercase tracking-[0.06em] text-muted-foreground">
                    {copy.sectionNumber} {index + 1}
                  </span>
                  <span className="font-display text-[16.5px] font-bold leading-snug text-foreground">
                    <bdi>{section.title}</bdi>
                  </span>
                </p>
                <p className="shrink-0 text-sm text-muted-foreground">
                  {section.lesson_count} {lessonsUnit}
                </p>
              </li>
            ))}
          </ol>

          {disclosable ? (
            <button
              type="button"
              onClick={() => setExpanded((open) => !open)}
              aria-expanded={expanded}
              aria-controls={listID}
              data-testid="course-curriculum-disclosure"
              className="mt-4 inline-flex items-center gap-2 rounded-md px-1 py-1 font-display text-sm font-bold text-primary underline-offset-4 hover:underline focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
            >
              {expanded ? copy.showFewerSections : copy.showAllSections}
              {/* Vertical, so it reads the same in both writing directions. */}
              <ChevronDown
                aria-hidden
                className={`size-4 transition-transform duration-base ease-out-brand ${
                  expanded ? "rotate-180" : ""
                }`}
              />
            </button>
          ) : null}
        </>
      )}
    </section>
  );
}
