"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { ArrowLeft, ArrowRight, GraduationCap } from "lucide-react";
import { Section } from "@/components/layout/section";
import { SectionHeading } from "@/components/ui/typography";
import { Alert } from "@/components/ui/alert";
import { EmptyState } from "@/components/common/empty-state";
import {
  useCarousel,
  CarouselArrows,
  CourseCard,
  type CourseCardLabels,
} from "@/components/sections/course-carousel";
import { getPublicCourses, type PublicCourse } from "@/lib/api/public-catalog";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";
import { routes } from "@/components/layout/nav-items";
import { useAcademicContext } from "@/components/academic/academic-context-provider";
import {
  catalogueHrefForContext,
  selectionForContext,
} from "@/components/academic/catalogue-context";
import { requestFilters } from "@/components/catalog/academic-filter-state";
import { academicContextNames } from "@/lib/academic/anonymous-context";
import { AcademicContextChips } from "@/components/academic/selected-academic-context";
import { useLandingJourney } from "@/components/landing/landing-journey";

type FeaturedState =
  | { kind: "loading" }
  | { kind: "ready"; courses: PublicCourse[]; total: number }
  | { kind: "failed" };

function courseHref(locale: "ar" | "en", course: PublicCourse): string {
  return `${routes.catalogue(locale)}/${encodeURIComponent(course.slug || course.id)}`;
}

/**
 * The neighbour-push state for one card, resolved to a *physical* screen direction.
 *
 * `index - hovered` is a DOM offset; the visual side it lands on flips under RTL, so the writing
 * direction is folded in here rather than in CSS. The rail's stylesheet then only has to know
 * "left"/"right", and the effect looks identical — physical-left leans left, physical-right leans
 * right — in both English and Arabic. The hovered card itself is `active` (lift, no sideways push).
 */
function pushState(
  index: number,
  hovered: number | null,
  dir: "ltr" | "rtl",
): "active" | "left" | "right" | "left-2" | "right-2" | undefined {
  if (hovered === null) return undefined;
  const offset = index - hovered;
  const rtl = dir === "rtl";
  if (offset === 0) return "active";
  if (offset === -1) return rtl ? "right" : "left";
  if (offset === 1) return rtl ? "left" : "right";
  if (offset === -2) return rtl ? "right-2" : "left-2";
  if (offset === 2) return rtl ? "left-2" : "right-2";
  return undefined;
}

export function FeaturedCourses() {
  const { locale, dir, t } = useLocale();
  const [state, setState] = useState<FeaturedState>({ kind: "loading" });
  const [hovered, setHovered] = useState<number | null>(null);
  const { status, anonymous, source } = useAcademicContext();
  // Present on the landing page, absent anywhere else this strip is mounted. See `useLandingJourney`.
  const journey = useLandingJourney();
  const carousel = useCarousel(
    dir,
    state.kind === "ready" ? state.courses.length : 0,
  );

  // Narrowed by the visitor's own academic context, through the same anonymous catalogue API the
  // catalogue itself uses. A profile-backed Student is left alone here: their profile orders the
  // catalogue rather than narrowing it.
  const filters =
    source === "anonymous" && anonymous
      ? requestFilters(selectionForContext(anonymous))
      : {};
  const filterKey = JSON.stringify(filters);

  useEffect(() => {
    // Waiting for the stored context avoids fetching the unfiltered list first and then visibly
    // replacing it a moment later with the personalised one.
    if (status !== "ready") return;
    let active = true;
    setState({ kind: "loading" });
    getPublicCourses(locale, "", JSON.parse(filterKey))
      .then((result) => {
        // A browsable row rather than a fixed three-up: a bounded slice of the real, ordered
        // response, enough to swipe through while "View all" carries the rest. `total` is the real
        // count behind that link.
        if (active)
          setState({
            kind: "ready",
            courses: result.items.slice(0, 9),
            total: result.total,
          });
      })
      .catch(() => {
        if (active) setState({ kind: "failed" });
      });
    return () => {
      active = false;
    };
  }, [locale, status, filterKey]);

  // Carries the context into the catalogue, so "View all" continues the list the reader is looking
  // at instead of resetting it.
  const browseAllHref =
    source === "anonymous" && anonymous
      ? catalogueHrefForContext(locale, anonymous)
      : routes.catalogue(locale);

  // The strip retitles itself once it is showing a narrowed list. "Start where your semester is" is
  // an invitation and belongs above the general catalogue; above courses chosen by the reader's own
  // university and program it would be describing something else.
  const personalized = source === "anonymous" && anonymous !== null;
  const names = personalized && anonymous ? academicContextNames(anonymous, locale) : null;

  const ready = state.kind === "ready" && state.courses.length > 0;
  const total = state.kind === "ready" ? state.total : null;
  const ViewAllArrow = dir === "rtl" ? ArrowLeft : ArrowRight;
  const viewAllLabel =
    total != null && total > 0
      ? t.courses.viewAllCount.replace("{count}", String(total))
      : t.courses.viewAll;
  const cardLabels: CourseCardLabels = {
    instructor: t.courses.instructor,
    preview: t.courses.previewShort,
    priceGuidance: t.courses.price,
  };

  return (
    <Section aria-labelledby="courses-title">
      <div className="mb-4 flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
          <SectionHeading id="courses-title">
            {personalized ? t.courses.personalizedTitle : t.courses.title}
          </SectionHeading>

          {/* The one secondary action and the rail controls, together at the head of the section.
              View-all is offered once there are courses — and also whenever the reader has named a
              university and program, even if nothing is published for them yet: that reader is
              exactly the one who needs the addressed catalogue, and hiding its only link behind a
              non-empty result leaves the empty state with no way onward. The arrows appear only when
              there is somewhere to scroll, and stay out of the small-screen layout where swiping is
              the real control. */}
          {ready || personalized ? (
            <div className="flex items-center gap-3 sm:gap-4">
              <Link
                href={browseAllHref}
                data-testid="featured-courses-view-all"
                className="group/all inline-flex items-center gap-1.5 whitespace-nowrap rounded-sm font-display text-[14px] font-bold text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              >
                {viewAllLabel}
                <ViewAllArrow
                  aria-hidden
                  className="size-4 transition-transform duration-base ease-out-brand motion-safe:group-hover/all:translate-x-0.5 rtl:motion-safe:group-hover/all:-translate-x-0.5"
                />
              </Link>
              {ready && carousel.scrollable ? (
                <CarouselArrows
                  dir={dir}
                  canPrev={carousel.canPrev}
                  canNext={carousel.canNext}
                  onPrev={carousel.scrollPrev}
                  onNext={carousel.scrollNext}
                  labels={{
                    previous: t.courses.carouselPrevious,
                    next: t.courses.carouselNext,
                  }}
                  className="hidden sm:flex"
                />
              ) : null}
            </div>
          ) : null}
        </div>

        {/* The context that produced this list, and the one control that reopens it. Changing it
            returns the reader to the question they answered rather than restarting the flow. */}
        {personalized && names && journey ? (
          <AcademicContextChips
            testID="featured-courses-context"
            institution={names.institution || anonymous!.institutionSlug}
            program={names.program}
            onChange={journey.requestEdit}
            changeLabel={t.academicContext.change}
            changeAria={t.academicContext.changeAria}
          />
        ) : null}
      </div>

      {state.kind === "loading" && (
        <p aria-live="polite" data-testid="featured-courses-loading">
          {t.courses.loading}
        </p>
      )}
      {state.kind === "failed" && (
        <div data-testid="featured-courses-error">
          <Alert tone="error" title={t.courses.failed} />
        </div>
      )}
      {state.kind === "ready" && state.courses.length === 0 && (
        <EmptyState
          icon={<GraduationCap aria-hidden />}
          title={t.courses.emptyTitle}
          description={t.courses.emptyBody}
        />
      )}
      {ready && (
        <ul
          ref={carousel.scrollerRef}
          data-testid="featured-courses-list"
          aria-label={t.courses.carouselLabel}
          onMouseLeave={() => setHovered(null)}
          className="course-rail no-scrollbar -mx-2 flex snap-x snap-mandatory gap-4 overflow-x-auto px-2 py-5 sm:gap-5"
        >
          {state.courses.map((course, index) => (
            <li
              key={course.id}
              data-push={pushState(index, hovered, dir)}
              onMouseEnter={() => setHovered(index)}
              className="flex shrink-0 basis-[86%] snap-start sm:basis-[46%] lg:basis-[340px]"
            >
              <CourseCard
                course={course}
                href={courseHref(locale, course)}
                locale={locale}
                labels={cardLabels}
              />
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}
