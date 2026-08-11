"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { GraduationCap } from "lucide-react";
import { Section, SectionHeader } from "@/components/layout/section";
import { Reveal } from "@/components/common/reveal";
import { EmptyState } from "@/components/common/empty-state";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { getPublicCourses, type PublicCourse } from "@/lib/api/public-catalog";
import { formatFils } from "@/lib/formatters/currency";
import { useLocale } from "@/lib/i18n/locale-provider";
import { routes } from "@/components/layout/nav-items";

const copy = {
  ar: {
    loading: "جارٍ تحميل المقررات المنشورة…",
    failed: "تعذّر تحميل المقررات المنشورة. حاول مرة أخرى.",
    instructor: "المدرّس",
    preview: "تتوفر معاينة عامة",
    price: "السعر الإرشادي",
  },
  en: {
    loading: "Loading published courses…",
    failed: "Published courses could not be loaded. Try again.",
    instructor: "Instructor",
    preview: "Public preview available",
    price: "Price guidance",
  },
};

type FeaturedState =
  | { kind: "loading" }
  | { kind: "ready"; courses: PublicCourse[] }
  | { kind: "failed" };

function courseHref(locale: "ar" | "en", course: PublicCourse): string {
  return `${routes.catalogue(locale)}/${encodeURIComponent(course.slug || course.id)}`;
}

function PublicCourseCard({ course }: { course: PublicCourse }) {
  const { locale } = useLocale();
  const t = copy[locale];
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
      {course.instructor_display_name && <p className="mt-2 text-sm text-muted-foreground">{t.instructor}: {course.instructor_display_name}</p>}
      {course.has_preview && <p className="mt-3 text-sm text-primary">{t.preview}</p>}
      {course.price && <p dir="ltr" className="mt-auto pt-5 font-mono text-base font-semibold text-foreground">{t.price}: {formatFils(course.price.minor_units, locale)}</p>}
    </Card>
  );
}

export function FeaturedCourses() {
  const { locale, t } = useLocale();
  const [state, setState] = useState<FeaturedState>({ kind: "loading" });

  useEffect(() => {
    let active = true;
    setState({ kind: "loading" });
    getPublicCourses(locale)
      .then((result) => {
        if (active) setState({ kind: "ready", courses: result.items.slice(0, 3) });
      })
      .catch(() => {
        if (active) setState({ kind: "failed" });
      });
    return () => {
      active = false;
    };
  }, [locale]);

  return (
    <Section id="courses" aria-labelledby="courses-title">
      <SectionHeader eyebrow={t.courses.eyebrow} title={t.courses.title} lead={t.courses.subtitle} headingId="courses-title" />

      {state.kind === "loading" && <p aria-live="polite" data-testid="featured-courses-loading">{copy[locale].loading}</p>}
      {state.kind === "failed" && <p role="alert" data-testid="featured-courses-error" className="rounded-lg border border-amber-300 bg-amber-50 p-5 text-amber-950">{copy[locale].failed}</p>}
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
              <Link href={routes.catalogue(locale)}>{t.courses.browseAll}</Link>
            </Button>
          </div>
        </>
      )}
    </Section>
  );
}
