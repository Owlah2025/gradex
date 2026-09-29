"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { getPublicInstructorProfile, type PublicInstructorProfile } from "@/lib/api/instructor-profile";
import { ProblemError } from "@/lib/api/problem";
import { catalogueCopy } from "@/components/catalog/catalogue-copy";
import { CourseCard } from "@/components/sections/course-carousel";
import { instructorInitials } from "@/components/catalog/course-detail-presentation";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { Container } from "@/components/layout/container";
import { Footer } from "@/components/layout/footer";
import { Navbar } from "@/components/layout/navbar";
import { useLocale } from "@/lib/i18n/locale-provider";

export function PublicInstructorPage({ slug, routeLocale }: { slug: string; routeLocale: "ar" | "en" }) {
  const { t } = useLocale();
  const copy = t.publicInstructor;
  const [profile, setProfile] = useState<PublicInstructorProfile | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "missing" | "failed">("loading");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setState("loading");
    getPublicInstructorProfile(slug, routeLocale)
      .then((next) => {
        if (cancelled) return;
        setProfile(next);
        setState("ready");
      })
      .catch((cause: unknown) => {
        if (cancelled) return;
        setState(cause instanceof ProblemError && cause.problem.status === 404 ? "missing" : "failed");
      });
    return () => {
      cancelled = true;
    };
  }, [attempt, routeLocale, slug]);

  if (state === "loading") {
    return (
      <>
        <Navbar />
        <main id="main" className="py-16"><Container><LoadingState label={copy.loading} /></Container></main>
        <Footer />
      </>
    );
  }

  if (state === "missing") {
    return (
      <>
        <Navbar />
        <main id="main" className="py-16"><Container><EmptyState title={copy.unavailableTitle} description={copy.unavailableBody} action={<Button asChild variant="outline"><Link href={"/" + routeLocale + "/catalog"}>{catalogueCopy[routeLocale].catalogue}</Link></Button>} /></Container></main>
        <Footer />
      </>
    );
  }

  if (state === "failed" || !profile) {
    return (
      <>
        <Navbar />
        <main id="main" className="py-16"><Container><ErrorState title={copy.failed} retryLabel={copy.retry} onRetry={() => setAttempt((current) => current + 1)} /></Container></main>
        <Footer />
      </>
    );
  }

  const headline = routeLocale === "ar" ? profile.headline_ar || profile.headline_en : profile.headline_en || profile.headline_ar;
  const bio = routeLocale === "ar" ? profile.bio_ar || profile.bio_en : profile.bio_en || profile.bio_ar;
  const initials = instructorInitials(profile.display_name);
  const catalogue = catalogueCopy[routeLocale];

  return (
    <>
      <Navbar />
      <main id="main" className="pb-16 outline-none" dir={routeLocale === "ar" ? "rtl" : "ltr"}>
        <section className="bg-gx-navy text-white">
          <Container>
            <div className="grid gap-8 py-14 md:grid-cols-[auto_minmax(0,1fr)] md:items-center md:py-20">
              <Avatar size="lg" className="size-24 border-4 border-white/15 text-3xl md:size-32" aria-hidden>
                <AvatarFallback>{initials}</AvatarFallback>
              </Avatar>
              <div className="max-w-3xl">
                <p className="text-sm font-semibold text-white/65">{copy.profileLabel}</p>
                <h1 className="mt-2 font-display text-4xl font-bold tracking-[-0.025em] text-white text-balance sm:text-5xl"><bdi>{profile.display_name}</bdi></h1>
                {headline ? <p className="mt-4 max-w-2xl text-xl leading-8 text-white/85"><bdi>{headline}</bdi></p> : null}
              </div>
            </div>
          </Container>
        </section>

        <Container>
          <div className="grid gap-12 py-12 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <div>
              {bio ? (
                <section aria-labelledby="instructor-biography-heading">
                  <h2 id="instructor-biography-heading" className="font-display text-2xl font-bold text-foreground">{copy.biographyLabel}</h2>
                  <p className="mt-4 max-w-3xl whitespace-pre-wrap text-[17px] leading-8 text-foreground"><bdi>{bio}</bdi></p>
                </section>
              ) : null}
              <section className="mt-12" aria-labelledby="instructor-courses-heading">
                <h2 id="instructor-courses-heading" className="font-display text-2xl font-bold text-foreground">{copy.coursesTitle}</h2>
                {profile.courses.length > 0 ? (
                  <ul className="mt-6 grid gap-5 sm:grid-cols-2">
                    {profile.courses.map((course) => (
                      <li key={course.id}>
                        <CourseCard
                          course={course}
                          href={"/" + routeLocale + "/catalog/" + encodeURIComponent(course.slug)}
                          locale={routeLocale}
                          labels={{ instructor: catalogue.instructor, preview: catalogue.preview, priceGuidance: catalogue.price }}
                        />
                      </li>
                    ))}
                  </ul>
                ) : <EmptyState className="mt-5" density="compact" title={copy.noCourses} />}
              </section>
            </div>

            <aside className="lg:pt-1">
              <section aria-labelledby="instructor-expertise-heading" className="rounded-lg border border-border bg-card p-5">
                <h2 id="instructor-expertise-heading" className="font-display text-lg font-bold text-foreground">{copy.expertiseTitle}</h2>
                {profile.expertise.length > 0 ? (
                  <ul className="mt-4 flex flex-wrap gap-2">
                    {profile.expertise.map((item) => (
                      <li key={item.id}>
                        <Badge variant="neutral"><bdi>{item.official_code ? item.official_code + " · " : ""}{routeLocale === "ar" ? item.title_ar : item.title_en}</bdi></Badge>
                      </li>
                    ))}
                  </ul>
                ) : <p className="mt-3 text-sm text-muted-foreground">—</p>}
              </section>
            </aside>
          </div>
        </Container>
      </main>
      <Footer />
    </>
  );
}
