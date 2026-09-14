"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useLocale } from "@/lib/i18n/locale-provider";
import { useSessionView } from "@/lib/identity/use-session";
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
  type SubjectAvailability,
  type SubjectListing,
} from "@/lib/api/subject-catalogue";
import {
  getPublicInstitutions,
  type InstitutionOption,
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
  const authenticated = session !== null;

  const institution = searchParams.get("institution") ?? "";
  const availability = readAvailability(searchParams.get("availability"));
  const search = searchParams.get("q") ?? "";

  const [institutions, setInstitutions] = React.useState<InstitutionOption[]>([]);
  const [subjects, setSubjects] = React.useState<SubjectListing[] | null>(null);
  const [total, setTotal] = React.useState(0);
  const [failed, setFailed] = React.useState(false);
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
    let live = true;
    setSubjects(null);
    setFailed(false);
    getSubjects(locale, {
      institution: institution || undefined,
      search: search || undefined,
      availability,
      pageSize: 60,
    })
      .then((page) => {
        if (!live) return;
        setSubjects(page.items);
        setTotal(page.total);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, [locale, institution, search, availability]);

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
    (next: { institution?: string; availability?: SubjectAvailability; q?: string }) => {
      const params = new URLSearchParams(searchParams.toString());
      const set = (key: string, value: string | undefined) => {
        // An empty value is removed rather than written empty, so a shared URL
        // never carries a filter that means nothing.
        if (value === undefined || value === "") params.delete(key);
        else params.set(key, value);
      };
      if ("institution" in next) set("institution", next.institution);
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
            {failed ? (
              <div className="max-w-lg">
                <Alert tone="error" title={copy.failed} />
                <Button
                  type="button"
                  className="mt-4"
                  onClick={() => applySelection({})}
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
                      authenticated={authenticated}
                      requested={requested.has(subject.subject_id)}
                      onDemandChange={onDemandChange}
                    />
                  ))}
                </div>
              </>
            )}
          </div>
        </Container>
      </main>
      <Footer />
    </>
  );
}
