"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ProblemError } from "@/lib/api/problem";
import { getStudentCourseAccessHistory } from "@/lib/api/access";
import { getPublicCourse, type PublicCourseDetail } from "@/lib/api/public-catalog";
import { useAcademicContext } from "@/components/academic/academic-context-provider";
import { catalogueHrefForContext } from "@/components/academic/catalogue-context";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Container } from "@/components/layout/container";
import { Breadcrumbs } from "@/components/layout/breadcrumbs";
import { routes } from "@/components/layout/nav-items";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState, SkeletonBlock } from "@/components/common/loading-state";
import { Button } from "@/components/ui/button";
import { catalogueCopy } from "./catalogue-copy";
import { CourseAccessSummary, MobileAccessBar } from "./course-access-summary";
import { CourseCurriculum } from "./course-curriculum";
import {
  BackToCatalogue,
  CourseAcademicContext,
  CourseHero,
  CourseInstructor,
} from "./course-detail-sections";
import { CoursePreview } from "./course-preview";
import type { AccessLookup } from "./course-access-relationship";

/**
 * How the read finished.
 *
 * `missing` and `failed` are kept apart because they are different facts about the world and lead
 * the reader somewhere different. They are also the *only* two the contract permits: the public
 * catalogue answers 404 for a course that is unpublished, suspended, retired or simply absent,
 * deliberately and identically, so that an anonymous visitor cannot probe which one it is. This page
 * therefore never claims to know why a course is gone, and never shows a student the lifecycle
 * vocabulary the Admin workspace uses.
 */
type DetailState =
  | { status: "loading" }
  | { status: "ready"; course: PublicCourseDetail }
  | { status: "missing" }
  | { status: "failed" };

/**
 * The public Course Details page.
 *
 * It reads two things and keeps them independent: the public Course, which every visitor may see,
 * and the signed-in Student's own access records, which only say what *this* reader may do next. A
 * 401 on the second is the ordinary anonymous state on a public page — it never hides the Course and
 * never surfaces as an error.
 */
export function CourseDetail({
  idOrSlug,
  routeLocale,
}: {
  idOrSlug: string;
  /**
   * The language this URL names, which is the language the catalogue is read in.
   *
   * Deliberately not `useLocale().locale` for the two reads below. The provider hydrates at the
   * default locale and only corrects itself to the route's language in a post-mount effect, so an
   * effect keyed on it runs once per language and every English visitor fetched the Course, and
   * their own access records, twice. The route segment is known before the first render and is the
   * very thing the provider goes on to agree with.
   */
  routeLocale: "ar" | "en";
}) {
  const { locale, t: dictionary } = useLocale();
  const copy = dictionary.courseDetail;
  const catalogue = catalogueCopy[locale];

  // Tranche C personalisation, carried through the visit: a student who arrived from a catalogue
  // narrowed to their university and program returns to that same narrowed catalogue.
  const { anonymous, source: contextSource } = useAcademicContext();
  const backHref =
    contextSource === "anonymous" && anonymous
      ? catalogueHrefForContext(locale, anonymous)
      : `/${locale}/catalog`;

  const [state, setState] = useState<DetailState>({ status: "loading" });
  const [lookup, setLookup] = useState<AccessLookup | null>(null);
  const [accessAttempt, setAccessAttempt] = useState(0);
  const [courseAttempt, setCourseAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setState({ status: "loading" });
    getPublicCourse(idOrSlug, routeLocale)
      .then((course) => {
        if (!cancelled) setState({ status: "ready", course });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setState(
          error instanceof ProblemError && error.problem.status === 404
            ? { status: "missing" }
            : { status: "failed" },
        );
      });
    return () => {
      cancelled = true;
    };
  }, [idOrSlug, routeLocale, courseAttempt]);

  // The Student's own access records, read separately from the public Course.
  //
  // Any failure other than 401 resolves to UNAVAILABLE rather than to "no access", so a transient
  // outage cannot tell an entitled Student they have nothing.
  useEffect(() => {
    let cancelled = false;
    setLookup(null);
    getStudentCourseAccessHistory(routeLocale)
      .then((history) => {
        if (!cancelled) setLookup({ status: "loaded", items: history?.items ?? [] });
      })
      .catch((cause: unknown) => {
        if (cancelled) return;
        const anonymousReader =
          cause instanceof ProblemError && cause.problem.status === 401;
        setLookup(anonymousReader ? { status: "anonymous" } : { status: "failed" });
      });
    return () => {
      cancelled = true;
    };
  }, [routeLocale, accessAttempt]);

  return (
    <>
      <Navbar />
      <main id="main" tabIndex={-1} className="py-8 outline-none sm:py-10">
        <Container>
          {/* Until the Course is known there is no hierarchy to describe, so the
              single back link stands in. Once it loads, the breadcrumb says the
              same thing and two more: where this page sits, and that "up" is
              somewhere the reader can actually go.

              The Courses crumb reuses the academic-context-aware href, so
              stepping up returns to the filtered catalogue the visitor was
              browsing rather than to an unfiltered one. */}
          {state.status === "ready" ? (
            <Breadcrumbs
              locale={routeLocale}
              label={dictionary.nav.breadcrumb}
              items={[
                { label: dictionary.nav.home, href: routes.home(routeLocale) },
                { label: dictionary.nav.courses, href: backHref },
                { label: state.course.title },
              ]}
            />
          ) : (
            <BackToCatalogue href={backHref} label={dictionary.academicContext.backToCatalogue} />
          )}

          {state.status === "loading" ? <CourseDetailSkeleton label={copy.loading} /> : null}

          {state.status === "missing" ? (
            <div data-testid="course-detail-unavailable">
              <EmptyState
                className="mt-10"
                title={copy.unavailableTitle}
                description={copy.unavailableBody}
                action={
                  <Button asChild variant="outline">
                    <Link href={backHref}>
                      {dictionary.academicContext.backToCatalogue}
                    </Link>
                  </Button>
                }
              />
            </div>
          ) : null}

          {state.status === "failed" ? (
            <ErrorState
              className="mt-10 max-w-2xl"
              testID="course-detail-failed"
              title={copy.loadFailedTitle}
              detail={copy.loadFailedBody}
              retryLabel={catalogue.retry}
              onRetry={() => setCourseAttempt((attempt) => attempt + 1)}
            />
          ) : null}

          {state.status === "ready" ? (
            <>
              {/* DOM order is reading order in both layouts: hero, then the access card, then the
                  course itself. On a phone that is the vertical order; from `lg` up the card moves
                  into a second column spanning both rows, beside the title it belongs to. Nothing is
                  reordered visually behind a screen reader's back. */}
              <article className="mt-6 lg:grid lg:grid-cols-[minmax(0,1fr)_21rem] lg:items-start lg:gap-x-10">
                <CourseHero
                  course={state.course}
                  copy={copy}
                  className="lg:col-start-1 lg:row-start-1"
                />

                <CourseAccessSummary
                  course={state.course}
                  lookup={lookup}
                  copy={copy}
                  accessLabels={dictionary.access}
                  priceLabel={catalogue.price}
                  loadingLabel={dictionary.access.loading}
                  locale={locale}
                  onRetry={() => setAccessAttempt((attempt) => attempt + 1)}
                />

                <div className="lg:col-start-1 lg:row-start-2">
                  <section className="mt-10" aria-labelledby="course-about-heading">
                    <h2
                      id="course-about-heading"
                      className="font-display text-2xl font-bold text-foreground"
                    >
                      {copy.aboutHeading}
                    </h2>
                    <p
                      className="mt-4 max-w-2xl whitespace-pre-wrap text-[16.5px] leading-relaxed text-foreground"
                      data-testid="course-detail-description"
                    >
                      {state.course.description}
                    </p>
                  </section>

                  {state.course.has_preview ? (
                    <CoursePreview
                      courseID={state.course.id}
                      locale={locale}
                      copy={copy}
                      watchLabel={catalogue.watchPreview}
                      failureLabel={catalogue.previewFailed}
                      retryLabel={catalogue.retry}
                    />
                  ) : null}

                  <CourseAcademicContext course={state.course} copy={copy} />

                  <CourseCurriculum
                    sections={state.course.sections}
                    copy={copy}
                    headingLabel={catalogue.outline}
                    lessonsUnit={catalogue.lessons}
                  />

                  <CourseInstructor course={state.course} copy={copy} />
                </div>
              </article>

              <MobileAccessBar
                label={copy.accessJump}
                priceLabel={catalogue.price}
                price={state.course.price}
                locale={locale}
              />
            </>
          ) : null}
        </Container>
      </main>
      <Footer />
    </>
  );
}

/**
 * The page's own shape, held while it loads.
 *
 * A single centred spinner would collapse the layout to nothing and then push a full page into
 * existence underneath the reader. This keeps the hero, the two columns and the outline roughly
 * where they will land, so arriving content settles rather than jumps.
 */
function CourseDetailSkeleton({ label }: { label: string }) {
  return (
    <div
      className="mt-6 lg:grid lg:grid-cols-[minmax(0,1fr)_21rem] lg:items-start lg:gap-x-10"
      data-testid="course-detail-skeleton"
    >
      <LoadingState label={label} visuallyHidden />
      <div aria-hidden className="lg:col-start-1 lg:row-start-1">
        <div className="h-4 w-32 animate-pulse rounded-pill bg-muted" />
        <div className="mt-4 h-10 w-3/4 animate-pulse rounded-md bg-muted" />
        <div className="mt-4 h-4 w-52 animate-pulse rounded-pill bg-muted" />
      </div>
      <div aria-hidden className="mt-8 lg:col-start-2 lg:row-span-2 lg:row-start-1 lg:mt-0">
        <div className="h-72 animate-pulse rounded-lg bg-muted" />
      </div>
      <div className="mt-10 lg:col-start-1 lg:row-start-2">
        <SkeletonBlock rows={2} />
        <SkeletonBlock className="mt-10" rows={3} />
      </div>
    </div>
  );
}
