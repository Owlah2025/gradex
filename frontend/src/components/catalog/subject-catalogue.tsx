"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { useSessionResolution, useSessionView } from "@/lib/identity/use-session";
import { subjectDemandAudience } from "@/lib/identity/subject-demand-authority";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { DisplayHeading, Prose } from "@/components/ui/typography";
import { Navbar } from "@/components/layout/navbar";
import { Footer } from "@/components/layout/footer";
import { Container } from "@/components/layout/container";
import { EmptyState } from "@/components/common/empty-state";
import {
  getSubjects,
  listOwnSubjectDemand,
  requestAborted,
  type SubjectAvailability,
  type SubjectListing,
} from "@/lib/api/subject-catalogue";
import {
  getPublicInstitutions,
  getPublicPrograms,
  type InstitutionOption,
  type ProgramOption,
} from "@/lib/api/public-catalog";
import { SubjectCard } from "./subject-card";
import { subjectCopy } from "./subject-copy";

/**
 * The public academic Subject catalogue (D-106 §5).
 *
 * # WHY THIS IS NOT THE COURSE CATALOGUE
 *
 * The Course catalogue lists what Gradex sells. This lists what universities
 * teach — including the Subjects Gradex does not teach yet, which is the whole
 * point. An unserved Subject is the instrument that measures demand: a Student
 * who can find their own course code and say "teach me this" tells Gradex what
 * to build next. A catalogue that hid those Subjects would measure nothing.
 *
 * Selection lives in the URL rather than in component state, so a Student can
 * share a filtered view, reload it, and use the back button — the same contract
 * the Course catalogue's academic filters already hold.
 */

const AVAILABILITY: SubjectAvailability[] = ["all", "served", "unserved"];

/**
 * How many Subjects one page carries.
 *
 * A page, not a ceiling. The catalogue previously asked for 60 against a corpus
 * of 329, which silently hid most of it; raising that number would only move
 * where the cliff falls. Every Subject is reachable by asking for the next page.
 */
const SUBJECTS_PER_PAGE = 24;

function readAvailability(value: string | null): SubjectAvailability {
  return AVAILABILITY.includes(value as SubjectAvailability)
    ? (value as SubjectAvailability)
    : "all";
}

export function SubjectCatalogue() {
  const { locale } = useLocale();
  const copy = subjectCopy[locale];
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const session = useSessionView();
  const resolution = useSessionResolution();
  // Not `session !== null`: that conflates "signed in" with "may register
  // demand", and would offer an actionable control to an Instructor, an Admin,
  // a restricted principal, or an untrusted device -- each of which the server
  // refuses. See subject-demand-authority.
  const audience = subjectDemandAudience(session, resolution);
  // Only an eligible Student has signals of their own to load.
  const authenticated = audience === "ELIGIBLE_STUDENT";

  const institution = searchParams.get("institution") ?? "";
  const program = institution ? (searchParams.get("program") ?? "") : "";
  const availability = readAvailability(searchParams.get("availability"));
  const search = searchParams.get("q") ?? "";

  const [institutions, setInstitutions] = React.useState<InstitutionOption[]>([]);
  const [programs, setPrograms] = React.useState<ProgramOption[]>([]);
  const [programsLoading, setProgramsLoading] = React.useState(false);
  const [programsFailed, setProgramsFailed] = React.useState(false);
  const [subjects, setSubjects] = React.useState<SubjectListing[] | null>(null);
  const [total, setTotal] = React.useState(0);
  const [loadedPages, setLoadedPages] = React.useState(0);
  const [loadingMore, setLoadingMore] = React.useState(false);
  const [failed, setFailed] = React.useState(false);
  // Bumped to re-run the first-page effect after a failure. Without it, "Try
  // again" only rewrote the URL, and when the URL was already correct -- which
  // it is whenever the failure was the network rather than the filters -- React
  // saw identical dependencies and never re-issued the request.
  const [reloadToken, setReloadToken] = React.useState(0);

  /**
   * Which query the catalogue is currently showing.
   *
   * Every request captures this value and checks it again before touching
   * state. An append issued against the previous query resolves after the reset
   * that replaced it, and without this guard its results were spliced into the
   * new list -- Subjects from a different institution, or filtered-out ones,
   * appearing under filters that exclude them. The `live` flag on the reset
   * effect never covered this: it protects the reset's own request, not an
   * append already in flight when the reset began.
   */
  const queryGeneration = React.useRef(0);
  /** Every in-flight catalogue request, so a reset can cancel all of them. */
  const pending = React.useRef(new Set<AbortController>());

  const beginRequest = React.useCallback((): {
    controller: AbortController;
    generation: number;
    settle: () => void;
  } => {
    const controller = new AbortController();
    pending.current.add(controller);
    return {
      controller,
      generation: queryGeneration.current,
      settle: () => pending.current.delete(controller),
    };
  }, []);

  /** Invalidates every outstanding request and starts a new query generation. */
  const invalidateOutstanding = React.useCallback(() => {
    queryGeneration.current += 1;
    for (const controller of pending.current) controller.abort();
    pending.current.clear();
  }, []);
  const [searchDraft, setSearchDraft] = React.useState(search);
  // Which Subjects this Student has already asked for. Empty for anonymous
  // visitors, who have no signals and are never told about anyone else's.
  const [requested, setRequested] = React.useState<Set<string>>(new Set());

  React.useEffect(() => {
    setSearchDraft(search);
  }, [search]);

  React.useEffect(() => {
    let live = true;
    getPublicInstitutions(locale)
      .then((items) => {
        if (live) setInstitutions(items);
      })
      // The filter degrades to "all universities" rather than failing the page:
      // the Subject list is the content, the institution picker is navigation.
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [locale]);

  React.useEffect(() => {
    if (!institution) {
      setPrograms([]);
      setProgramsLoading(false);
      setProgramsFailed(false);
      return;
    }
    let live = true;
    setPrograms([]);
    setProgramsLoading(true);
    setProgramsFailed(false);
    getPublicPrograms(institution, locale)
      .then((items) => {
        if (live) setPrograms(items);
      })
      .catch(() => {
        if (live) setProgramsFailed(true);
      })
      .finally(() => {
        if (live) setProgramsLoading(false);
      });
    return () => {
      live = false;
    };
  }, [institution, locale]);

  // The first page, and every reset. Changing the locale, the institution, the
  // availability, or the search discards what was loaded and starts again at
  // page one -- appending page two of a *different* query would interleave two
  // result sets into one list.
  React.useEffect(() => {
    // Cancel anything still running for the previous query before starting
    // this one, so a late append cannot land on the new result set.
    invalidateOutstanding();
    const { controller, generation, settle } = beginRequest();

    setSubjects(null);
    setLoadedPages(0);
    setLoadingMore(false);
    setFailed(false);
    getSubjects(locale, {
      institution: institution || undefined,
      program: program || undefined,
      search: search || undefined,
      availability,
      pageSize: SUBJECTS_PER_PAGE,
      page: 1,
      signal: controller.signal,
    })
      .then((page) => {
        if (generation !== queryGeneration.current) return;
        setSubjects(page.items);
        setTotal(page.total);
        setLoadedPages(1);
      })
      .catch((error: unknown) => {
        // A cancelled request is not a failure to report: the query it
        // belonged to is gone and something newer is already loading.
        if (requestAborted(error)) return;
        if (generation !== queryGeneration.current) return;
        setFailed(true);
      })
      .finally(settle);

    return () => {
      controller.abort();
      settle();
    };
  }, [locale, institution, program, search, availability, reloadToken, beginRequest, invalidateOutstanding]);

  /**
   * Appends the next page.
   *
   * Deduplicates by `subject_id` on append. The server orders by institution,
   * then served-before-unserved, then code — a stable ordering, but a Course
   * published between two requests moves a Subject across that boundary, and a
   * Subject already on screen must not appear twice because of it.
   */
  const loadMore = React.useCallback(() => {
    if (loadingMore || subjects === null) return;
    const next = loadedPages + 1;
    // An append belongs to the query already on screen, so it joins the current
    // generation rather than starting a new one.
    const { controller, generation, settle } = beginRequest();

    setLoadingMore(true);
    setFailed(false);
    getSubjects(locale, {
      institution: institution || undefined,
      program: program || undefined,
      search: search || undefined,
      availability,
      pageSize: SUBJECTS_PER_PAGE,
      page: next,
      signal: controller.signal,
    })
      .then((page) => {
        // The filters may have changed while this was in flight. Appending now
        // would splice one query's page two into another query's page one.
        if (generation !== queryGeneration.current) return;
        setSubjects((current) => {
          const base = current ?? [];
          const seen = new Set(base.map((item) => item.subject_id));
          return [...base, ...page.items.filter((item) => !seen.has(item.subject_id))];
        });
        setTotal(page.total);
        setLoadedPages(next);
      })
      .catch((error: unknown) => {
        if (requestAborted(error)) return;
        if (generation !== queryGeneration.current) return;
        setFailed(true);
      })
      .finally(() => {
        settle();
        if (generation === queryGeneration.current) setLoadingMore(false);
      });
  }, [
    availability,
    beginRequest,
    institution,
    program,
    loadedPages,
    loadingMore,
    locale,
    search,
    subjects,
  ]);

  React.useEffect(() => {
    if (!authenticated) {
      setRequested(new Set());
      return;
    }
    let live = true;
    listOwnSubjectDemand(locale)
      .then((signals) => {
        if (live) setRequested(new Set(signals.map((signal) => signal.subject_id)));
      })
      // A Student whose own signals fail to load still sees the catalogue; the
      // cards open in the unrequested state and a duplicate request converges
      // on "requested" rather than erroring.
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [authenticated, locale]);

  const applySelection = React.useCallback(
    (next: {
      institution?: string;
      program?: string;
      availability?: SubjectAvailability;
      q?: string;
    }) => {
      const params = new URLSearchParams(searchParams.toString());
      const set = (key: string, value: string | undefined) => {
        // An empty value is removed rather than written empty, so a shared URL
        // never carries a filter that means nothing.
        if (value === undefined || value === "") params.delete(key);
        else params.set(key, value);
      };
      if ("institution" in next) {
        set("institution", next.institution);
        set("program", "");
      }
      if ("program" in next) set("program", next.program);
      if ("availability" in next)
        set("availability", next.availability === "all" ? "" : next.availability);
      if ("q" in next) set("q", next.q);
      const query = params.toString();
      router.replace(query === "" ? (pathname ?? "") : `${pathname}?${query}`, {
        scroll: false,
      });
    },
    [pathname, router, searchParams],
  );

  function onDemandChange(subjectId: string, isRequested: boolean) {
    setRequested((current) => {
      const next = new Set(current);
      if (isRequested) next.add(subjectId);
      else next.delete(subjectId);
      return next;
    });
  }

  const emptyMessage = institution ? copy.emptyForInstitution : copy.empty;

  return (
    <>
      <Navbar />
      <main id="main">
        <Container className="py-10">
          <DisplayHeading>{copy.title}</DisplayHeading>
          <Prose className="mt-3 max-w-2xl">{copy.intro}</Prose>

          <form
            className="mt-8 flex flex-wrap items-end gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              applySelection({ q: searchDraft.trim() });
            }}
          >
            <div className="min-w-[16rem] flex-1">
              <label
                htmlFor="subject-search"
                className="block text-sm font-semibold text-foreground"
              >
                {copy.searchLabel}
              </label>
              <Input
                id="subject-search"
                className="mt-2"
                value={searchDraft}
                placeholder={copy.searchPlaceholder}
                onChange={(event) => setSearchDraft(event.target.value)}
              />
            </div>

            <div>
              <label
                htmlFor="subject-institution"
                className="block text-sm font-semibold text-foreground"
              >
                {copy.institutionLabel}
              </label>
              <select
                id="subject-institution"
                className="mt-2 min-h-11 rounded-md border border-border bg-background px-3 text-sm text-foreground"
                value={institution}
                onChange={(event) =>
                  applySelection({ institution: event.target.value })
                }
                data-testid="subject-institution-filter"
              >
                <option value="">{copy.allInstitutions}</option>
                {institutions.map((option) => (
                  <option key={option.slug} value={option.slug}>
                    {locale === "ar" ? option.name_ar : option.name_en}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label
                htmlFor="subject-program"
                className="block text-sm font-semibold text-foreground"
              >
                {copy.programLabel}
              </label>
              <select
                id="subject-program"
                className="mt-2 min-h-11 rounded-md border border-border bg-background px-3 text-sm text-foreground"
                value={program}
                onChange={(event) => applySelection({ program: event.target.value })}
                disabled={!institution || programsLoading || programsFailed || programs.length === 0}
                data-testid="subject-program-filter"
              >
                <option value="">
                  {programsLoading
                    ? copy.programsLoading
                    : programsFailed
                      ? copy.programsFailed
                      : programs.length === 0 && institution
                        ? copy.noPrograms
                        : copy.allPrograms}
                </option>
                {programs.map((option) => (
                  <option key={option.slug} value={option.slug}>
                    {locale === "ar" ? option.name_ar : option.name_en}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label
                htmlFor="subject-availability"
                className="block text-sm font-semibold text-foreground"
              >
                {copy.availabilityLabel}
              </label>
              <select
                id="subject-availability"
                className="mt-2 min-h-11 rounded-md border border-border bg-background px-3 text-sm text-foreground"
                value={availability}
                onChange={(event) =>
                  applySelection({
                    availability: readAvailability(event.target.value),
                  })
                }
                data-testid="subject-availability-filter"
              >
                <option value="all">{copy.availabilityAll}</option>
                <option value="served">{copy.availabilityServed}</option>
                <option value="unserved">{copy.availabilityUnserved}</option>
              </select>
            </div>

            <Button type="submit">{copy.searchSubmit}</Button>
            {search === "" ? null : (
              <Button
                type="button"
                variant="outline"
                onClick={() => applySelection({ q: "" })}
              >
                {copy.clearSearch}
              </Button>
            )}
          </form>

          <div className="mt-8">
            {failed && subjects === null ? (
              <div className="max-w-lg">
                <Alert tone="error" title={copy.failed} />
                <Button
                  type="button"
                  className="mt-4"
                  // Re-runs the request. The previous handler called
                  // applySelection({}), which only rewrote the URL -- and when
                  // the URL was already correct, which it is whenever the
                  // failure was the network rather than the filters, nothing
                  // re-fetched and the button did nothing at all.
                  onClick={() => setReloadToken((token) => token + 1)}
                  data-testid="subject-retry"
                >
                  {copy.retry}
                </Button>
              </div>
            ) : subjects === null ? (
              <p className="text-sm text-muted-foreground">{copy.loading}</p>
            ) : subjects.length === 0 ? (
              <EmptyState title={emptyMessage} />
            ) : (
              <>
                <p className="text-sm text-muted-foreground" data-testid="subject-result-count">
                  {total} {total === 1 ? copy.resultCount : copy.resultCountPlural}
                </p>
                <div className="mt-4 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
                  {subjects.map((subject) => (
                    <SubjectCard
                      key={subject.subject_id}
                      subject={subject}
                      copy={copy}
                      locale={locale}
                      audience={audience}
                      requested={requested.has(subject.subject_id)}
                      onDemandChange={onDemandChange}
                    />
                  ))}
                </div>

                {/* Progress is stated, not implied: "24 of 329" tells the
                    Student the list is partial, which an unadorned Load More
                    button does not. */}
                <p
                  className="mt-6 text-sm text-muted-foreground"
                  data-testid="subject-shown-count"
                >
                  {copy.showing
                    .replace("{shown}", String(subjects.length))
                    .replace("{total}", String(total))}
                </p>

                {failed ? (
                  <div className="mt-4 max-w-lg">
                    <Alert tone="error" title={copy.failed} />
                  </div>
                ) : null}

                {subjects.length < total ? (
                  <Button
                    type="button"
                    variant="outline"
                    className="mt-4"
                    onClick={loadMore}
                    disabled={loadingMore}
                    data-testid="subject-load-more"
                  >
                    {loadingMore ? copy.loadingMore : copy.loadMore}
                  </Button>
                ) : null}
              </>
            )}
          </div>
        </Container>
      </main>
      <Footer />
    </>
  );
}
