"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { GraduationCap } from "lucide-react";
import { Section, SectionHeader } from "@/components/layout/section";
import { Reveal } from "@/components/common/reveal";
import { Alert } from "@/components/ui/alert";
import { EmptyState } from "@/components/common/empty-state";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { getPublicCourses, type PublicCourse } from "@/lib/api/public-catalog";
import { formatFils } from "@/lib/formatters/currency";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";
import { useAcademicContext } from "@/components/academic/academic-context-provider";
import {
  catalogueHrefForContext,
  selectionForContext,
} from "@/components/academic/catalogue-context";
import { requestFilters } from "@/components/catalog/academic-filter-state";


type FeaturedState =
  | { kind: "loading" }
  | { kind: "ready"; courses: PublicCourse[] }
  | { kind: "failed" };

function courseHref(locale: "ar" | "en", course: PublicCourse): string {
  return `${routes.catalogue(locale)}/${encodeURIComponent(course.slug || course.id)}`;
}

function PublicCourseCard({ course }: { course: PublicCourse }) {
  const { locale, t } = useLocale();
  const taxonomy = [course.major, course.subject, course.study_year].filter(Boolean);

  return (
    <Card className="flex h-full flex-col p-6">
      {taxonomy.length > 0 && (
        <ul className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          {taxonomy.map((term) => (
            <li key={term!.label} className="rounded-full bg-muted px-2.5 py-1">
              {term!.label}{term!.code ? ` · ${term!.code}` : ""}
            </li>
          ))}
        </ul>
      )}
      <h3 className="mt-4 font-display text-xl font-bold leading-snug text-foreground">
        <Link href={courseHref(locale, course)} className="focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
          {course.title}
        </Link>
      </h3>
      {course.instructor_display_name && <p className="mt-2 text-sm text-muted-foreground">{t.courses.instructor}: {course.instructor_display_name}</p>}
      {course.has_preview && <p className="mt-3 text-sm text-primary">{t.courses.preview}</p>}
      {course.price && <p dir="ltr" className="mt-auto pt-5 font-mono text-base font-semibold text-foreground">{t.courses.price}: {formatFils(course.price.minor_units, locale)}</p>}
    </Card>
  );
}

export function FeaturedCourses() {
  const { locale, t } = useLocale();
  const [state, setState] = useState<FeaturedState>({ kind: "loading" });
  const { status, anonymous, source } = useAcademicContext();

  // Narrowed by the visitor's own academic context, through the same anonymous catalogue API the
  // catalogue itself uses. A profile-backed Student is left alone here: their profile orders the
  // catalogue rather than narrowing it, and this strip is too small to be worth misrepresenting.
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
        if (active) setState({ kind: "ready", courses: result.items.slice(0, 3) });
      })
      .catch(() => {
        if (active) setState({ kind: "failed" });
      });
    return () => {
      active = false;
    };
  }, [locale, status, filterKey]);

  // Carries the context into the catalogue, so "Browse all courses" continues the list the reader
  // is looking at instead of resetting it.
  const browseAllHref =
    source === "anonymous" && anonymous
      ? catalogueHrefForContext(locale, anonymous)
      : routes.catalogue(locale);

  return (
    <Section id="courses" aria-labelledby="courses-title">
      <SectionHeader eyebrow={t.courses.eyebrow} title={t.courses.title} lead={t.courses.subtitle} headingId="courses-title" />

      {state.kind === "loading" && <p aria-live="polite" data-testid="featured-courses-loading">{t.courses.loading}</p>}
      {state.kind === "failed" && (
        <div data-testid="featured-courses-error">
          <Alert tone="error" title={t.courses.failed} />
        </div>
      )}
      {state.kind === "ready" && state.courses.length === 0 && (
        <EmptyState icon={<GraduationCap aria-hidden />} title={t.courses.emptyTitle} description={t.courses.emptyBody} />
      )}
      {state.kind === "ready" && state.courses.length > 0 && (
        <>
          <ul data-testid="featured-courses-list" className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
            {state.courses.map((course, index) => (
              <Reveal as="li" key={course.id} delay={(index % 3) as 0 | 1 | 2}>
                <PublicCourseCard course={course} />
              </Reveal>
            ))}
          </ul>
          <div className="mt-9 flex justify-center">
            <Button asChild variant="outline">
              <Link href={browseAllHref}>{t.courses.browseAll}</Link>
            </Button>
          </div>
        </>
      )}
    </Section>
  );
}
