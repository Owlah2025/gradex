"use client";

import * as React from "react";
import Link from "next/link";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  useSessionResolution,
  useSessionView,
} from "@/lib/identity/use-session";
import { subjectDemandAudience } from "@/lib/identity/subject-demand-authority";
import { Button } from "@/components/ui/button";
import { DisplayHeading, Prose } from "@/components/ui/typography";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Container } from "@/components/layout/container";
import {
  institutionName,
  listOwnSubjectDemand,
  subjectTitle,
  type SubjectListing,
} from "@/lib/api/subject-catalogue";
import { SubjectDemandAction } from "./subject-demand-action";
import { subjectCopy } from "./subject-copy";

/**
 * One Subject, public, served or not (D-106 §5).
 *
 * The page exists for both states because the unserved state is the one that
 * does the work: it is where a Student learns Gradex does not teach this yet
 * and says they want it. A page that only existed once a Course did would never
 * be reachable at the moment the Student's intent is real.
 *
 * Served, the page's job is to get out of the way and link to the Course.
 * Unserved, it explains the absence plainly and offers demand registration.
 * Neither state invents a product: no price, no placeholder Course, no
 * "coming soon" that implies a commitment nobody has made.
 *
 * The Subject arrives already resolved from the server route, which is what
 * lets a missing one answer a real HTTP 404. This component therefore has no
 * loading, missing, or failed state to render for it — only the interactive
 * demand states below, which are why it stays a client component at all.
 */
export function SubjectDetail({ subject }: { subject: SubjectListing }) {
  const { locale } = useLocale();
  const copy = subjectCopy[locale];
  const session = useSessionView();
  const resolution = useSessionResolution();
  const audience = subjectDemandAudience(session, resolution);
  const authenticated = audience === "ELIGIBLE_STUDENT";

  const [requested, setRequested] = React.useState(false);

  React.useEffect(() => {
    if (!authenticated) {
      setRequested(false);
      return;
    }
    let live = true;
    listOwnSubjectDemand(locale)
      .then((signals) => {
        if (live)
          setRequested(
            signals.some((signal) => signal.subject_id === subject.subject_id),
          );
      })
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [authenticated, locale, subject.subject_id]);

  const backHref = `/${locale}/subjects`;

  return (
    <>
      <Navbar />
      <main id="main">
        <Container className="py-10">
          <Link
            href={backHref}
            className="text-sm font-semibold text-muted-foreground hover:underline"
          >
            {copy.detailBack}
          </Link>

          <article className="mt-6 max-w-2xl">
            <p className="text-sm font-semibold text-muted-foreground">
              <bdi>{institutionName(subject, locale)}</bdi>
            </p>

            <DisplayHeading className="mt-2">
              <bdi>{subjectTitle(subject, locale)}</bdi>
            </DisplayHeading>

            {subject.code ? (
              <p className="mt-3 font-mono text-sm font-bold text-foreground">
                <span className="font-sans font-semibold text-muted-foreground">
                  {copy.code}:{" "}
                </span>
                <bdi>{subject.code}</bdi>
              </p>
            ) : null}

            <p
              className={
                subject.served
                  ? "mt-4 inline-flex rounded-full bg-gx-success-soft px-3 py-1 text-xs font-bold text-gx-navy"
                  : "mt-4 inline-flex rounded-full bg-muted px-3 py-1 text-xs font-bold text-muted-foreground"
              }
              data-testid="subject-availability"
            >
              {subject.served ? copy.served : copy.unserved}
            </p>

            {subject.served ? (
              <section className="mt-8" data-testid="subject-courses">
                <h2 className="font-display text-lg font-bold text-foreground">
                  {copy.coursesHeading}
                </h2>
                <ul className="mt-4 space-y-3">
                  {subject.courses.map((course) => (
                    <li
                      key={course.slug}
                      className="rounded-xl border border-border bg-card p-4 text-card-foreground shadow-sm"
                    >
                      <p className="font-display text-base font-bold text-foreground">
                        <bdi>{course.title}</bdi>
                      </p>
                      <Button
                        asChild
                        className="mt-3"
                        data-testid="subject-open-course"
                      >
                        <Link
                          href={`/${locale}/catalog/${encodeURIComponent(course.slug)}`}
                        >
                          {copy.openCourse}
                        </Link>
                      </Button>
                    </li>
                  ))}
                </ul>
              </section>
            ) : (
              <>
                <Prose className="mt-6">{copy.requestIntro}</Prose>
                <SubjectDemandAction
                  subjectId={subject.subject_id}
                  copy={copy}
                  locale={locale}
                  audience={audience}
                  initiallyRequested={requested}
                  onChange={(_, isRequested) => setRequested(isRequested)}
                />
              </>
            )}
          </article>
        </Container>
      </main>
      <Footer />
    </>
  );
}
