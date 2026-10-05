"use client";

import * as React from "react";
import Link from "next/link";
import { ArrowLeft, ArrowRight, BookOpen } from "lucide-react";
import {
  academicContext,
  useAcademicContext,
} from "@/components/academic/academic-context-provider";
import { useAcademicOptions } from "@/components/academic/use-academic-options";
import { EmptyState } from "@/components/common/empty-state";
import { useLandingJourney } from "@/components/landing/landing-journey";
import { Section } from "@/components/layout/section";
import { StudyPlanSubjectCard } from "@/components/sections/study-plan-subject-card";
import {
  CarouselArrows,
  type CourseCardLabels,
  useCarousel,
} from "@/components/sections/course-carousel";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { SectionHeading } from "@/components/ui/typography";
import {
  getSubjects,
  listOwnSubjectDemand,
  requestAborted,
  type SubjectAvailability,
  type SubjectListing,
} from "@/lib/api/subject-catalogue";
import { useLocale } from "@/lib/i18n/locale-provider";
import { subjectDemandAudience } from "@/lib/identity/subject-demand-authority";
import { useSessionResolution, useSessionView } from "@/lib/identity/use-session";

const LANDING_SUBJECT_LIMIT = 12;

type SubjectState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ready"; subjects: SubjectListing[] }
  | { kind: "failed" };

function subjectsHref(
  locale: "ar" | "en",
  institution: string,
  program: string,
): string {
  const parameters = new URLSearchParams();
  if (institution) parameters.set("institution", institution);
  if (program) parameters.set("program", program);
  const query = parameters.toString();
  return `/${locale}/subjects${query ? `?${query}` : ""}`;
}

export function StudyPlanSubjects() {
  const { locale, dir, t } = useLocale();
  const context = useAcademicContext();
  const anonymousContext = context.anonymous;
  const setAnonymousContext = context.setAnonymous;
  const journey = useLandingJourney();
  const session = useSessionView();
  const resolution = useSessionResolution();
  const audience = subjectDemandAudience(session, resolution);

  const [institution, setInstitution] = React.useState("");
  const [program, setProgram] = React.useState("");
  const [availability, setAvailability] =
    React.useState<SubjectAvailability>("all");
  const [subjects, setSubjects] = React.useState<SubjectState>({ kind: "idle" });
  const [subjectAttempt, setSubjectAttempt] = React.useState(0);
  const [requested, setRequested] = React.useState<Set<string>>(new Set());
  // Keep the displayed filters aligned with a context that another landing surface just resolved.
  // The hero and this section mount together, so a one-time initialization misses a later
  // anonymous selection after the section has already rendered. Keying only on the resolved
  // source and slugs also preserves deliberate local filter changes for a profile-backed Student.
  const lastResolvedSelection = React.useRef<string | null>(null);

  React.useEffect(() => {
    if (
      context.status !== "ready" ||
      context.profileStatus !== "ready"
    )
      return;

    const resolvedSelection =
      context.source === "profile"
        ? `profile:${context.profile?.institution_slug ?? ""}:${context.profile?.program_slug ?? ""}`
        : context.source === "anonymous"
          ? `anonymous:${context.anonymous?.institutionSlug ?? ""}:${context.anonymous?.programSlug ?? ""}`
          : "none";
    if (lastResolvedSelection.current === resolvedSelection) return;
    lastResolvedSelection.current = resolvedSelection;

    if (context.source === "profile" && context.profile?.institution_slug) {
      setInstitution(context.profile.institution_slug);
      setProgram(context.profile.program_slug ?? "");
      return;
    }

    setInstitution(context.anonymous?.institutionSlug ?? "");
    setProgram(context.anonymous?.programSlug ?? "");
  }, [
    context.status,
    context.profileStatus,
    context.source,
    context.profile?.institution_slug,
    context.profile?.program_slug,
    context.anonymous?.institutionSlug,
    context.anonymous?.programSlug,
  ]);

  const options = useAcademicOptions(locale, institution);
  const programItems = options.programs.kind === "ready" ? options.programs.items : [];
  const hasPrograms = options.programs.kind === "ready" && programItems.length > 0;
  const canLoadSubjects =
    institution !== "" &&
    options.programs.kind === "ready" &&
    (!hasPrograms || program !== "");

  // A profile or stored selection can outlive a renamed/retired Program. Wait
  // for the authoritative option list, then clear only the stale browsing
  // child; never write to the Student profile.
  React.useEffect(() => {
    if (options.programs.kind !== "ready" || program === "") return;
    if (options.programs.items.some((item) => item.slug === program)) return;
    setProgram("");
    if (
      anonymousContext?.institutionSlug === institution &&
      anonymousContext.programSlug === program
    ) {
      setAnonymousContext(
        academicContext(institution, "", {
          institutionAr: anonymousContext.names.institutionAr,
          institutionEn: anonymousContext.names.institutionEn,
        }),
      );
    }
  }, [
    anonymousContext,
    setAnonymousContext,
    institution,
    options.programs,
    program,
  ]);

  React.useEffect(() => {
    if (!canLoadSubjects) {
      setSubjects({ kind: "idle" });
      return;
    }
    const controller = new AbortController();
    setSubjects({ kind: "loading" });
    getSubjects(locale, {
      institution,
      program: program || undefined,
      availability,
      page: 1,
      pageSize: LANDING_SUBJECT_LIMIT,
      signal: controller.signal,
    })
      .then((page) => {
        // Service status is the backend field. A served Subject without its
        // promised projection is a broken response, never an unserved card.
        if (page.items.some((item) => item.served && !item.primary_course)) {
          throw new Error("served Subject response is missing primary_course");
        }
        setSubjects({ kind: "ready", subjects: page.items });
      })
      .catch((error: unknown) => {
        if (!requestAborted(error)) setSubjects({ kind: "failed" });
      });
    return () => controller.abort();
  }, [availability, canLoadSubjects, institution, locale, program, subjectAttempt]);

  React.useEffect(() => {
    if (audience !== "ELIGIBLE_STUDENT") {
      setRequested(new Set());
      return;
    }
    let active = true;
    listOwnSubjectDemand(locale)
      .then((signals) => {
        if (active) setRequested(new Set(signals.map((signal) => signal.subject_id)));
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [audience, locale]);

  const readySubjects = subjects.kind === "ready" ? subjects.subjects : [];
  const carousel = useCarousel(dir, readySubjects.length);
  const ViewAllArrow = dir === "rtl" ? ArrowLeft : ArrowRight;
  const courseLabels: CourseCardLabels = {
    instructor: t.courses.instructor,
    preview: t.courses.previewShort,
    priceGuidance: t.courses.price,
  };

  function persistBrowsingContext(nextInstitution: string, nextProgram: string) {
    if (!nextInstitution) {
      context.setAnonymous(null);
      return;
    }
    const institutionOption =
      options.institutions.kind === "ready"
        ? options.institutions.items.find((item) => item.slug === nextInstitution)
        : undefined;
    const programOption =
      options.programs.kind === "ready"
        ? options.programs.items.find((item) => item.slug === nextProgram)
        : undefined;
    context.setAnonymous(
      academicContext(nextInstitution, nextProgram, {
        institutionAr: institutionOption?.name_ar ?? "",
        institutionEn: institutionOption?.name_en ?? "",
        programAr: programOption?.name_ar ?? "",
        programEn: programOption?.name_en ?? "",
      }),
    );
  }

  function changeInstitution(next: string) {
    setInstitution(next);
    setProgram("");
    persistBrowsingContext(next, "");
  }

  function changeProgram(next: string) {
    setProgram(next);
    persistBrowsingContext(institution, next);
  }

  function onDemandChange(subjectId: string, isRequested: boolean) {
    setRequested((current) => {
      const next = new Set(current);
      if (isRequested) next.add(subjectId);
      else next.delete(subjectId);
      return next;
    });
  }

  return (
    <Section
      id="study-plan"
      ref={journey?.registerCourses}
      aria-labelledby="study-plan-title"
      tone="muted"
      className="scroll-mt-16 pb-10 md:pb-12 lg:pb-14"
      data-testid="study-plan-subjects"
    >
      <div className="flex flex-col gap-5">
        <div className="flex flex-wrap items-start justify-between gap-x-8 gap-y-4">
          <div className="max-w-3xl">
            <SectionHeading id="study-plan-title">{t.studyPlan.title}</SectionHeading>
            <p className="mt-2 text-pretty text-muted-foreground">{t.studyPlan.subtitle}</p>
          </div>
          {institution ? (
            <Link
              href={subjectsHref(locale, institution, program)}
              className="group/all inline-flex min-h-11 items-center gap-1.5 whitespace-nowrap rounded-sm font-display text-sm font-bold text-primary underline-offset-4 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              data-testid="study-plan-view-all"
            >
              {t.studyPlan.viewAll}
              <ViewAllArrow
                aria-hidden
                className="size-4 transition-transform duration-base ease-out-brand motion-safe:group-hover/all:translate-x-0.5 rtl:motion-safe:group-hover/all:-translate-x-0.5"
              />
            </Link>
          ) : null}
        </div>

        <div className="flex flex-wrap items-end gap-3" data-testid="study-plan-filters">
          <label className="min-w-[13rem] flex-1 text-sm font-semibold text-foreground">
            <span>{t.studyPlan.institutionLabel}</span>
            <select
              className="mt-2 min-h-11 w-full rounded-md border border-border bg-card px-3 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              value={institution}
              onChange={(event) => changeInstitution(event.target.value)}
              disabled={options.institutions.kind !== "ready"}
              data-testid="study-plan-institution"
            >
              <option value="">
                {options.institutions.kind === "loading"
                  ? t.studyPlan.institutionsLoading
                  : t.studyPlan.chooseInstitution}
              </option>
              {options.institutions.kind === "ready"
                ? options.institutions.items.map((item) => (
                    <option key={item.slug} value={item.slug}>
                      {locale === "ar" ? item.name_ar : item.name_en}
                    </option>
                  ))
                : null}
            </select>
          </label>

          <label className="min-w-[13rem] flex-1 text-sm font-semibold text-foreground">
            <span>{t.studyPlan.programLabel}</span>
            <select
              className="mt-2 min-h-11 w-full rounded-md border border-border bg-card px-3 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              value={program}
              onChange={(event) => changeProgram(event.target.value)}
              disabled={options.programs.kind !== "ready" || programItems.length === 0}
              data-testid="study-plan-program"
            >
              <option value="">
                {options.programs.kind === "loading"
                  ? t.studyPlan.programsLoading
                  : t.studyPlan.chooseProgram}
              </option>
              {programItems.map((item) => (
                <option key={item.slug} value={item.slug}>
                  {locale === "ar" ? item.name_ar : item.name_en}
                </option>
              ))}
            </select>
          </label>

          <fieldset className="flex min-h-11 items-center gap-1 rounded-md border border-border bg-card p-1">
            <legend className="sr-only">{t.studyPlan.availabilityLabel}</legend>
            {(["all", "served"] as const).map((value) => (
              <button
                key={value}
                type="button"
                className={
                  availability === value
                    ? "min-h-9 rounded-sm bg-primary px-3 text-sm font-bold text-primary-foreground"
                    : "min-h-9 rounded-sm px-3 text-sm font-semibold text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                }
                aria-pressed={availability === value}
                onClick={() => setAvailability(value)}
              >
                {value === "all"
                  ? t.studyPlan.availabilityAll
                  : t.studyPlan.availabilityServed}
              </button>
            ))}
          </fieldset>
        </div>

        {options.institutions.kind === "failed" ? (
          <div className="max-w-lg">
            <Alert tone="error" title={t.studyPlan.institutionsFailed} />
            <Button type="button" variant="outline" className="mt-3" onClick={options.retryInstitutions}>
              {t.studyPlan.retry}
            </Button>
          </div>
        ) : context.status === "loading" || context.profileStatus === "loading" ? (
          <p aria-live="polite" className="text-sm text-muted-foreground">
            {t.studyPlan.loading}
          </p>
        ) : institution === "" ? (
          <EmptyState
            icon={<BookOpen aria-hidden />}
            title={t.studyPlan.noInstitutionTitle}
            description={t.studyPlan.noInstitutionBody}
          />
        ) : options.programs.kind === "failed" ? (
          <div className="max-w-lg">
            <Alert tone="error" title={t.studyPlan.programsFailed} />
            <Button type="button" variant="outline" className="mt-3" onClick={options.retryPrograms}>
              {t.studyPlan.retry}
            </Button>
          </div>
        ) : options.programs.kind === "loading" ? (
          <p aria-live="polite" className="text-sm text-muted-foreground">
            {t.studyPlan.programsLoading}
          </p>
        ) : hasPrograms && program === "" ? (
          <EmptyState
            icon={<BookOpen aria-hidden />}
            title={t.studyPlan.chooseProgramTitle}
            description={t.studyPlan.chooseProgramBody}
          />
        ) : (
          <>
            {options.programs.kind === "ready" && options.programs.items.length === 0 ? (
              <p className="text-sm text-muted-foreground" data-testid="study-plan-no-programs">
                <strong className="font-semibold text-foreground">{t.studyPlan.noProgramsTitle}.</strong>{" "}
                {t.studyPlan.noProgramsBody}
              </p>
            ) : null}

            {subjects.kind === "loading" ? (
              <p aria-live="polite" className="text-sm text-muted-foreground" data-testid="study-plan-loading">
                {t.studyPlan.loading}
              </p>
            ) : subjects.kind === "failed" ? (
              <div className="max-w-lg" data-testid="study-plan-error">
                <Alert tone="error" title={t.studyPlan.failed} />
                <Button
                  type="button"
                  variant="outline"
                  className="mt-3"
                  onClick={() => setSubjectAttempt((attempt) => attempt + 1)}
                >
                  {t.studyPlan.retry}
                </Button>
              </div>
            ) : subjects.kind === "ready" && subjects.subjects.length === 0 ? (
              <EmptyState title={t.studyPlan.emptyTitle} description={t.studyPlan.emptyBody} />
            ) : subjects.kind === "ready" ? (
              <div>
                {carousel.scrollable ? (
                  <CarouselArrows
                    dir={dir}
                    canPrev={carousel.canPrev}
                    canNext={carousel.canNext}
                    onPrev={carousel.scrollPrev}
                    onNext={carousel.scrollNext}
                    labels={{
                      previous: t.studyPlan.carouselPrevious,
                      next: t.studyPlan.carouselNext,
                    }}
                    className="mb-2 ms-auto hidden sm:flex"
                  />
                ) : null}
                <ul
                  ref={carousel.scrollerRef}
                  className="course-rail no-scrollbar -mx-2 flex snap-x snap-mandatory gap-4 overflow-x-auto px-2 py-4 sm:gap-5"
                  aria-label={t.studyPlan.carouselLabel}
                  data-testid="study-plan-list"
                >
                  {subjects.subjects.map((subject) => (
                    <li
                      key={subject.subject_id}
                      className="flex shrink-0 basis-[82%] snap-start sm:basis-[44%] md:basis-[31%] xl:basis-[22%]"
                      data-testid="study-plan-subject-card"
                      data-served={subject.served ? "true" : "false"}
                      data-subject-code={subject.code ?? ""}
                    >
                      <StudyPlanSubjectCard
                        subject={subject}
                        locale={locale}
                        copy={t.studyPlan}
                        courseLabels={courseLabels}
                        audience={audience}
                        requested={requested.has(subject.subject_id)}
                        onDemandChange={onDemandChange}
                      />
                    </li>
                  ))}
                </ul>
              </div>
            ) : null}
          </>
        )}
      </div>
    </Section>
  );
}
