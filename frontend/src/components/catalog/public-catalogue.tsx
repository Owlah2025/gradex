"use client";

import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { catalogueCopy } from "./catalogue-copy";
import { switchLocalePath } from "@/lib/i18n/locale-path";
import { getStudentCourseAccessHistory } from "@/lib/api/access";
import { CourseAccessPanel } from "./course-access-panel";
import {
  courseAccessRelationship,
  type AccessLookup,
} from "./course-access-relationship";
import {
  getPublicCourse,
  getPublicCoursePreview,
  getPublicCourses,
  type PublicCourse,
  type PublicCourseDetail,
} from "@/lib/api/public-catalog";
import { AcademicFilters } from "./academic-filters";
import {
  clearedSelection,
  emptyStateKind,
  hasSelection,
  institutionName,
  programName,
  readSelection,
  requestFilters,
  selectionSearch,
  type CatalogueSelection,
} from "./academic-filter-state";
import type {
  InstitutionOption,
  ProgramOption,
} from "@/lib/api/public-catalog";
import { useAcademicContext } from "@/components/academic/academic-context-provider";
import { AcademicContextSummary } from "@/components/academic/academic-context-summary";
import {
  catalogueHrefForContext,
  contextForSelection,
  selectionForContext,
} from "@/components/academic/catalogue-context";
import {
  academicContextNames,
  sameAcademicContext,
} from "@/lib/academic/anonymous-context";
import { ProblemError } from "@/lib/api/problem";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Alert } from "@/components/ui/alert";
import { DisplayHeading, Prose } from "@/components/ui/typography";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Container } from "@/components/layout/container";
import { EmptyState } from "@/components/common/empty-state";
import { formatFils } from "@/lib/formatters/currency";

function CatalogueSearch({ initialQuery }: { initialQuery: string }) {
  const { locale } = useLocale();
  const t = catalogueCopy[locale];
  const pathname = usePathname();
  const router = useRouter();
  const [query, setQuery] = useState(initialQuery);

  useEffect(() => setQuery(initialQuery), [initialQuery]);

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const parameters = new URLSearchParams();
    if (query !== "") parameters.set("q", query);
    const suffix = parameters.size === 0 ? "" : `?${parameters}`;
    router.push(`${pathname}${suffix}`);
  }

  return (
    <form
      className="mt-8 flex max-w-xl gap-3"
      role="search"
      onSubmit={submitSearch}
    >
      <label className="sr-only" htmlFor="catalogue-search">
        {t.searchLabel}
      </label>
      <Input
        id="catalogue-search"
        type="search"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder={t.searchPlaceholder}
      />
      <Button type="submit">{t.searchSubmit}</Button>
    </form>
  );
}

export function CatalogueList() {
  const { locale, t: dictionary } = useLocale();
  const t = catalogueCopy[locale];
  const pathname = usePathname();
  const router = useRouter();
  const searchParameters = useSearchParams();

  // The URL is the single owner of the selection. Nothing is mirrored into
  // component state, so refresh, a shared link, and browser back/forward are
  // all the same code path rather than three behaviours to keep in step.
  const selection = readSelection(searchParameters.toString());
  const query = selection.query;

  const [state, setState] = useState<{
    items?: PublicCourse[];
    error?: string;
  }>({});
  const [retryCount, setRetryCount] = useState(0);

  const {
    status: contextStatus,
    anonymous,
    profile,
    source: contextSource,
    setAnonymous,
    reconcile,
    refreshNames,
  } = useAcademicContext();

  // The Student's own Program, read from their own profile, used only to order
  // results. An anonymous visitor simply has none: a 401 there is an ordinary
  // state on a public page and never surfaces as an error or hides a Course.
  // The profile is read once by the shared academic-context provider rather
  // than a second time here.
  const relevantProgram = profile?.program_slug ?? "";

  // The live option lists, reported by the filter row. Held so the context bar can name the current
  // selection from real catalogue data rather than from the display cache alone.
  const [institutionOptions, setInstitutionOptions] = useState<
    InstitutionOption[] | null
  >(null);
  // Tagged with the institution they were read for, so a list that arrived for a university the
  // visitor has since moved away from cannot be used to judge the current selection.
  const [programOptions, setProgramOptions] = useState<{
    institution: string;
    items: ProgramOption[];
  } | null>(null);

  // The selection as of this render, reachable from callbacks that must stay referentially stable
  // for the effects inside AcademicFilters. Reading it through a ref is what keeps those callbacks
  // from re-firing the option requests on every keystroke.
  const selectionRef = useRef(selection);
  selectionRef.current = selection;

  /**
   * What the catalogue does about the academic context on arrival, once.
   *
   * A URL that already names an institution is what the visitor came to see — a shared link, a
   * bookmark, a link from the landing page — so it is adopted as the remembered context, which is
   * what keeps the bar above the results from ever describing something they are not filtered by.
   * A URL that names nothing academic is seeded from the remembered context instead, so a Student
   * does not re-pick their university every time they come back.
   *
   * `replace`, not `push`: restoring a remembered preference is not a place the visitor navigated
   * to, and putting it in history would make Back appear to do nothing.
   *
   * Once, deliberately. Re-running this on every URL change made "Show all courses" impossible to
   * complete: clearing writes the empty context and pushes the emptied URL, but the push lands a
   * render later, so a re-running effect saw the *old* URL beside the *new* empty context and
   * adopted the context straight back out of it. After arrival every change comes through
   * `navigate`, which moves both halves in the same update and has no such gap.
   */
  const arrivalHandled = useRef(false);
  useEffect(() => {
    if (contextStatus !== "ready" || arrivalHandled.current) return;
    arrivalHandled.current = true;
    const current = selectionRef.current;
    if (current.institution !== "") {
      const adopted = contextForSelection(current, anonymous);
      if (!sameAcademicContext(adopted, anonymous)) setAnonymous(adopted);
      return;
    }
    // A profile-backed Student is never auto-filtered: their profile ranks the catalogue, it does
    // not narrow it, and pretending otherwise would hide Courses their account never asked to hide.
    if (contextSource !== "anonymous" || anonymous === null) return;
    const restored = { ...selectionForContext(anonymous), query: current.query };
    router.replace(`${pathname}${selectionSearch(restored)}`);
  }, [contextStatus, contextSource, anonymous, pathname, router, setAnonymous]);

  /**
   * Records the option lists, corrects the remembered context, and refreshes its display names.
   *
   * Correcting the *URL* deliberately does not happen here. These callbacks fire whenever a
   * response lands, which is not necessarily after the arrival effect has finished seeding the
   * address bar — a list that arrived first saw an empty selection, found nothing wrong with it,
   * and then a retired university appeared in the URL a moment later with nothing left to notice
   * it. The URL is judged in an effect below, which re-runs whenever either half changes.
   */
  const handleInstitutions = useCallback(
    (items: InstitutionOption[] | null) => {
      setInstitutionOptions(items);
      if (items === null) return;
      reconcile(
        items.map((item) => item.slug),
        null,
      );
      // The live names for what is selected, so the bar never falls back to showing a slug.
      const chosen = items.find(
        (item) => item.slug === selectionRef.current.institution,
      );
      if (chosen)
        refreshNames({
          institution: {
            slug: chosen.slug,
            nameAr: chosen.name_ar,
            nameEn: chosen.name_en,
          },
        });
    },
    [reconcile, refreshNames],
  );

  const handlePrograms = useCallback(
    (items: ProgramOption[] | null) => {
      const institution = selectionRef.current.institution;
      setProgramOptions(items === null ? null : { institution, items });
      if (items === null) return;
      reconcile(
        null,
        items.map((item) => item.slug),
      );
      const chosen = items.find(
        (item) => item.slug === selectionRef.current.program,
      );
      if (chosen)
        refreshNames({
          program: {
            slug: chosen.slug,
            nameAr: chosen.name_ar,
            nameEn: chosen.name_en,
          },
        });
    },
    [reconcile, refreshNames],
  );

  /**
   * A filter the catalogue cannot offer is one the visitor can neither see nor remove, so it is
   * taken out of the address bar.
   *
   * Only the invalid part goes. A retired university takes its program with it, because that
   * program was chosen from *its* list. A program that has gone under a university that still
   * exists leaves the university selected and takes level and Subject with it, because both are
   * read from the study plan the program names.
   *
   * A list that failed to load is `null` and judges nothing: a network failure is not evidence that
   * a university was retired.
   */
  useEffect(() => {
    if (selection.institution === "") return;

    // One effect, and the order inside it matters. As two effects these rules deadlocked: both ran
    // in the same commit, the university rule emptied the address bar, and the program rule — which
    // by design keeps the university — immediately wrote the retired one back. Neither dependency
    // list had changed by then, so nothing re-ran and the invalid filter stayed forever.
    if (
      institutionOptions !== null &&
      !institutionOptions.some((item) => item.slug === selection.institution)
    ) {
      router.replace(
        `${pathname}${selectionSearch({ ...clearedSelection(), query: selection.query })}`,
      );
      return;
    }

    if (selection.program === "") return;
    // Judged only against the list read for this very university.
    if (programOptions === null || programOptions.institution !== selection.institution)
      return;
    if (programOptions.items.some((item) => item.slug === selection.program)) return;
    router.replace(
      `${pathname}${selectionSearch({ ...selection, program: "", level: "", subject: "" })}`,
    );
  }, [institutionOptions, programOptions, selection, pathname, router]);

  const filters = requestFilters(selection, relevantProgram);
  // Serialised so the effect re-runs when the filters change by value rather
  // than by the identity of a freshly built object.
  const filterKey = JSON.stringify(filters);

  useEffect(() => {
    let cancelled = false;
    setState({});
    getPublicCourses(locale, query, JSON.parse(filterKey))
      .then((result) => {
        if (!cancelled) setState({ items: result.items });
      })
      .catch(() => {
        if (!cancelled) setState({ error: t.failed });
      });
    return () => {
      cancelled = true;
    };
  }, [locale, query, filterKey, t.failed, retryCount]);

  /**
   * The single funnel for every change the catalogue makes to its own address.
   *
   * Forgetting the context here, in the same update that pushes the emptied URL, is what makes
   * "Clear filters" and "Show all courses" actually stick: the restore effect re-reads the context
   * on the very next render, and if it were still stored it would immediately put the filters back.
   */
  function navigate(next: CatalogueSelection) {
    setAnonymous(contextForSelection(next, anonymous));
    router.push(`${pathname}${selectionSearch(next)}`);
  }

  /** The localized names for whatever is selected right now, so the bar can never describe something else. */
  const selectedInstitutionName =
    institutionOptions?.find((item) => item.slug === selection.institution)
      ? institutionName(
          institutionOptions.find((item) => item.slug === selection.institution)!,
          locale as "ar" | "en",
        )
      : anonymous?.institutionSlug === selection.institution
        ? academicContextNames(anonymous, locale as "ar" | "en").institution
        : "";
  const selectedProgramName =
    selection.program === ""
      ? ""
      : programOptions?.items.find((item) => item.slug === selection.program)
        ? programName(
            programOptions.items.find((item) => item.slug === selection.program)!,
            locale as "ar" | "en",
          )
        : anonymous?.programSlug === selection.program
          ? academicContextNames(anonymous, locale as "ar" | "en").program
          : "";

  const emptyMessage = {
    "no-courses": t.empty,
    "no-search-results": t.noResults,
    "no-courses-for-institution": t.emptyForInstitution,
    "no-courses-for-program": t.emptyForProgram,
    "no-courses-for-level": t.emptyForLevel,
    "no-courses-for-subject": t.emptyForSubject,
  }[emptyStateKind(selection)];

  return (
    <>
      <Navbar />
      <main id="main" tabIndex={-1} className="py-10 outline-none">
        <Container>
          <DisplayHeading as="h1">{t.catalogue}</DisplayHeading>

          {/* Only rendered when the catalogue is genuinely narrowed. A profile-backed Student is
              told their results are *ordered* around their program — see the relevance note below —
              because that is what actually happened to this list. */}
          {selection.institution !== "" && (
            <AcademicContextSummary
              testID="catalogue-academic-context"
              className="mt-6"
              institution={selectedInstitutionName || selection.institution}
              program={selectedProgramName}
              provenance={dictionary.academicContext.savedOnDevice}
              onChange={() => {
                document.getElementById("catalogue-institution")?.focus();
              }}
              onClear={() => navigate(clearedSelection())}
            />
          )}

          <CatalogueSearch key={query} initialQuery={query} />

          <AcademicFilters
            locale={locale as "ar" | "en"}
            selection={selection}
            onChange={navigate}
            onInstitutionsLoaded={handleInstitutions}
            onProgramsLoaded={handlePrograms}
          />

          {relevantProgram !== "" && selection.program === "" && (
            <p className="mt-4 text-sm text-muted-foreground">{t.relevanceNote}</p>
          )}

          {!state.items && !state.error && (
            <Prose className="mt-8" aria-live="polite">
              {query === "" ? t.loading : t.searching}
            </Prose>
          )}

          {state.error && (
            <div className="mt-8">
              <Alert tone="error" title={state.error}>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setRetryCount((c) => c + 1)}
                >
                  {t.retry}
                </Button>
              </Alert>
            </div>
          )}

          {state.items?.length === 0 && (
            <div className="mt-8" data-testid="catalogue-empty">
              <EmptyState
                title={emptyMessage}
                description={
                  hasAcademicSelection(selection)
                    ? dictionary.academicContext.emptyBody
                    : undefined
                }
                action={
                  hasSelection(selection) ? (
                    <Button
                      variant="outline"
                      onClick={() =>
                        navigate(
                          query !== "" && !hasAcademicSelection(selection)
                            ? { ...selection, query: "" }
                            : clearedSelection(),
                        )
                      }
                    >
                      {hasAcademicSelection(selection)
                        ? t.clearFilters
                        : t.clearSearch}
                    </Button>
                  ) : undefined
                }
              />
            </div>
          )}

          <section
            aria-label={t.catalogue}
            className="mt-8 grid gap-5 md:grid-cols-2"
          >
            {state.items?.map((course) => (
              <Link
                key={course.id}
                href={`/${locale}/catalog/${course.slug}`}
                className="block rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              >
                <Card interactive className="flex h-full flex-col">
                  <CardHeader>
                    <div className="flex flex-wrap gap-2 text-sm">
                      {[
                        course.university,
                        course.major,
                        course.subject,
                        course.study_year,
                      ]
                        .filter(Boolean)
                        .map((term) => (
                          <Badge key={term!.label} variant="neutral">
                            {term!.label}
                            {term!.code ? ` · ${term!.code}` : ""}
                          </Badge>
                        ))}
                    </div>
                    <h2 className="mt-4 font-display text-lg font-bold leading-snug">
                      {course.title}
                    </h2>
                  </CardHeader>
                  <CardContent className="mt-auto">
                    <Prose className="text-sm">
                      {t.instructor}: {course.instructor_display_name}
                    </Prose>
                    {course.price && (
                      <p className="mt-3 text-sm font-semibold text-primary">
                        {t.price}:{" "}
                        <span dir="ltr" className="dir-ltr font-mono">
                          {formatFils(
                            course.price.minor_units,
                            locale as "ar" | "en",
                          )}
                        </span>
                      </p>
                    )}
                    {course.has_preview && (
                      <p className="mt-3 text-sm text-primary">{t.preview}</p>
                    )}
                  </CardContent>
                </Card>
              </Link>
            ))}
          </section>
        </Container>
      </main>
      <Footer />
    </>
  );
}

function hasAcademicSelection(selection: CatalogueSelection): boolean {
  return (
    selection.institution !== "" ||
    selection.program !== "" ||
    selection.subject !== ""
  );
}
