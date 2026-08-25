import type {
  AcademicFilters,
  InstitutionOption,
  ProgramOption,
  SubjectOption,
} from "@/lib/api/public-catalog";

/**
 * Academic discovery filter state (T6).
 *
 * The URL is the single owner of this state. Every function here is pure and
 * takes the current query string, so the catalogue has no second copy of the
 * selection to drift out of step with the address bar — which is what makes
 * refresh, a shared link, and browser back/forward all behave the same way
 * without any of the three being special-cased.
 *
 * Nothing here is an authority over anything. These values narrow a public read
 * and rank it; they never grant access, and the Student's academic profile
 * contributes only the ranking hint.
 */

export type CatalogueSelection = {
  institution: string;
  program: string;
  /** Academic level, as recorded by a study plan. Empty means "any". */
  level: string;
  subject: string;
  query: string;
};

/** The query-parameter names, in one place so the client and URL cannot drift. */
export const CATALOGUE_PARAMETERS = {
  institution: "institution",
  program: "program",
  level: "level",
  subject: "subject",
  query: "q",
} as const;

function read(parameters: URLSearchParams, name: string): string {
  return (parameters.get(name) ?? "").trim();
}

/** readSelection is the only way the catalogue learns what is selected. */
export function readSelection(
  search: string | URLSearchParams,
): CatalogueSelection {
  const parameters =
    typeof search === "string" ? new URLSearchParams(search) : search;
  const institution = read(parameters, CATALOGUE_PARAMETERS.institution);
  const program = read(parameters, CATALOGUE_PARAMETERS.program);
  return {
    institution,
    // A Program is meaningless without its University, and a Subject is offered
    // per University. Dropping the dependent values rather than carrying them
    // is what stops a hand-edited or stale URL from asking for a combination
    // the choosers can no longer render.
    program: institution === "" ? "" : program,
    level:
      institution === "" ? "" : read(parameters, CATALOGUE_PARAMETERS.level),
    subject:
      institution === "" ? "" : read(parameters, CATALOGUE_PARAMETERS.subject),
    query: read(parameters, CATALOGUE_PARAMETERS.query),
  };
}

/**
 * applySelection returns the next selection when one chooser changes.
 *
 * Choosing a University clears the Program and Subject beneath it, and choosing
 * a Program clears the Subject, because those options are drawn from the level
 * above. Leaving a stale child selected would silently filter on something the
 * visitor can no longer see in the controls.
 */
export function applySelection(
  current: CatalogueSelection,
  change: Partial<CatalogueSelection>,
): CatalogueSelection {
  const next: CatalogueSelection = { ...current, ...change };
  if (
    change.institution !== undefined &&
    change.institution !== current.institution
  ) {
    next.program = "";
    next.level = "";
    next.subject = "";
  }
  // A level means "level N of this study plan", so changing the Program changes
  // what a carried-over level would mean. Clearing it is the only honest option.
  if (change.program !== undefined && change.program !== current.program) {
    next.level = "";
    next.subject = "";
  }
  if (next.institution === "") {
    next.program = "";
    next.level = "";
    next.subject = "";
  }
  return next;
}

/** clearedSelection is what the reset control produces: everything, including search. */
export function clearedSelection(): CatalogueSelection {
  return { institution: "", program: "", level: "", subject: "", query: "" };
}

export function hasSelection(selection: CatalogueSelection): boolean {
  return (
    selection.institution !== "" ||
    selection.program !== "" ||
    selection.level !== "" ||
    selection.subject !== "" ||
    selection.query !== ""
  );
}

/**
 * selectionSearch renders a selection back into a query string.
 *
 * Empty values are omitted entirely rather than written as blanks, so a cleared
 * catalogue has a clean shareable URL and two equal selections always produce
 * the same link.
 */
export function selectionSearch(selection: CatalogueSelection): string {
  const parameters = new URLSearchParams();
  if (selection.institution !== "")
    parameters.set(CATALOGUE_PARAMETERS.institution, selection.institution);
  if (selection.program !== "")
    parameters.set(CATALOGUE_PARAMETERS.program, selection.program);
  if (selection.level !== "")
    parameters.set(CATALOGUE_PARAMETERS.level, selection.level);
  if (selection.subject !== "")
    parameters.set(CATALOGUE_PARAMETERS.subject, selection.subject);
  if (selection.query !== "")
    parameters.set(CATALOGUE_PARAMETERS.query, selection.query);
  const rendered = parameters.toString();
  return rendered === "" ? "" : `?${rendered}`;
}

/**
 * requestFilters converts a selection into the API filter object.
 *
 * relevantToProgram is supplied separately by the caller from the Student's own
 * academic profile. It is kept out of CatalogueSelection on purpose: it is not
 * a selection, it does not belong in the shared URL, and it must never be
 * mistaken for one of the narrowing filters.
 */
export function requestFilters(
  selection: CatalogueSelection,
  relevantToProgram = "",
): AcademicFilters {
  const filters: AcademicFilters = {};
  if (selection.institution !== "") filters.institution = selection.institution;
  if (selection.program !== "") filters.program = selection.program;
  if (selection.level !== "") filters.level = selection.level;
  if (selection.subject !== "") filters.subject = selection.subject;
  // Ranking is pointless once the visitor has narrowed to that same Program,
  // and it would needlessly make a cacheable response private.
  if (relevantToProgram !== "" && selection.program === "")
    filters.relevantToProgram = relevantToProgram;
  return filters;
}

/** Localized display names. A raw slug or identifier must never reach a reader. */
export function institutionName(
  option: InstitutionOption,
  locale: "ar" | "en",
): string {
  return locale === "ar" ? option.name_ar : option.name_en;
}

export function programName(
  option: ProgramOption,
  locale: "ar" | "en",
): string {
  return locale === "ar" ? option.name_ar : option.name_en;
}

export function programContext(
  option: ProgramOption,
  locale: "ar" | "en",
): string {
  return (
    (locale === "ar" ? option.college_name_ar : option.college_name_en) ?? ""
  );
}

export function subjectName(
  option: SubjectOption,
  locale: "ar" | "en",
): string {
  const title = locale === "ar" ? option.title_ar : option.title_en;
  // The official code is real, public academic vocabulary a Student recognises
  // from their own study plan — unlike an identifier, which is never shown.
  return option.code ? `${option.code} · ${title}` : title;
}

/**
 * emptyStateKind names why a result set is empty, so the catalogue can say
 * something true instead of one generic failure message. An empty catalogue is
 * a valid answer, never an error.
 */
export type EmptyStateKind =
  | "no-courses"
  | "no-courses-for-subject"
  | "no-courses-for-level"
  | "no-courses-for-program"
  | "no-courses-for-institution"
  | "no-search-results";

export function emptyStateKind(selection: CatalogueSelection): EmptyStateKind {
  if (selection.subject !== "") return "no-courses-for-subject";
  if (selection.level !== "") return "no-courses-for-level";
  if (selection.program !== "") return "no-courses-for-program";
  if (selection.institution !== "") return "no-courses-for-institution";
  if (selection.query !== "") return "no-search-results";
  return "no-courses";
}
