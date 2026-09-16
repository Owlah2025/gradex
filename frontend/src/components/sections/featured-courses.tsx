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
import { routes } from "@/components/layout/nav-items";

type FeaturedState =
  | { kind: "loading" }
  | { kind: "ready"; courses: PublicCourse[] }
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
  const carousel = useCarousel(
    dir,
    state.kind === "ready" ? state.courses.length : 0,
  );

  useEffect(() => {
    let active = true;
    setState({ kind: "loading" });
    getPublicCourses(locale)
      .then((result) => {
        // A browsable row rather than a fixed three-up: a bounded slice of the real, ordered
        // response, enough to swipe through while "View all" carries the rest.
        if (active)
          setState({
            kind: "ready",
            courses: result.items.slice(0, 9),
          });
      })
      .catch(() => {
        if (active) setState({ kind: "failed" });
      });
    return () => {
      active = false;
    };
  }, [locale]);

  const ready = state.kind === "ready" && state.courses.length > 0;
  const ViewAllArrow = dir === "rtl" ? ArrowLeft : ArrowRight;
  const viewAllLabel = t.courses.viewAll;
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
            {t.courses.title}
          </SectionHeading>

          {/* The secondary catalogue link and rail controls share one compact action group. */}
          {ready ? (
            <div className="flex items-center gap-3 sm:gap-4">
              <Link
                href={routes.catalogue(locale)}
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
        <p className="max-w-2xl text-pretty text-muted-foreground">{t.courses.subtitle}</p>
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
      {ready && state.courses.length === 1 ? (
        <div className="grid gap-8 lg:grid-cols-[340px_minmax(0,1fr)] lg:items-center lg:gap-16">
          <ul data-testid="featured-courses-list" aria-label={t.courses.carouselLabel} className="max-w-[340px]">
            <li className="flex">
              <CourseCard
                course={state.courses[0]}
                href={courseHref(locale, state.courses[0])}
                locale={locale}
                labels={cardLabels}
              />
            </li>
          </ul>
          <div className="max-w-xl lg:py-8" data-testid="single-course-companion">
            <h3 className="text-balance font-display text-2xl font-bold leading-tight text-foreground md:text-3xl">
              {t.courses.singleTitle}
            </h3>
            <p className="mt-3 max-w-prose text-pretty leading-7 text-muted-foreground">
              {t.courses.singleBody}
            </p>
            <Link
              href="#study-plan"
              className="mt-5 inline-flex min-h-11 items-center gap-2 font-display text-sm font-bold text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              {t.courses.exploreSubjects}
              <ViewAllArrow aria-hidden className="size-4" />
            </Link>
          </div>
        </div>
      ) : ready && (
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
