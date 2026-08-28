import {
  academicContext,
  emptyAcademicContextNames,
  type AnonymousAcademicContext,
} from "../../lib/academic/anonymous-context";
import {
  clearedSelection,
  readSelection,
  selectionSearch,
  type CatalogueSelection,
} from "../catalog/academic-filter-state";

/**
 * The one place the browsing preference and the catalogue URL are translated into each other.
 *
 * Both directions live here so they cannot disagree, and neither side has to learn the other's
 * vocabulary: `academic-filter-state` keeps owning the query-parameter names, and
 * `anonymous-context` keeps owning the stored shape.
 *
 * Only institution and program cross this boundary. Level and Subject exist in the URL and are
 * never persisted — a level means "level N *of this study plan*", so it is re-chosen whenever the
 * program changes, and a Subject is a search rather than an identity.
 */

/** The catalogue selection a stored context asks for. */
export function selectionForContext(
  context: AnonymousAcademicContext,
): CatalogueSelection {
  return {
    ...clearedSelection(),
    institution: context.institutionSlug,
    program: context.programSlug,
  };
}

/** Where a context sends a visitor: the catalogue, already narrowed, with a shareable URL. */
export function catalogueHrefForContext(
  locale: "ar" | "en",
  context: AnonymousAcademicContext,
): string {
  return `/${locale}/catalog${selectionSearch(selectionForContext(context))}`;
}

/**
 * The context a catalogue selection implies, or `null` when it implies none.
 *
 * This is what makes the existing filter row a way to *change* the academic context rather than a
 * second, competing copy of it: every navigation the catalogue performs runs through here, so
 * choosing a university persists it and clearing the filters forgets it. Without that, "Clear
 * filters" would be undone by the next page load restoring the very context it just dropped.
 *
 * Cached display names are carried over only while the slug they describe is unchanged. A name kept
 * beside a different slug would be a label describing something else, which is the one thing this
 * cache must never become.
 */
export function contextForSelection(
  selection: CatalogueSelection,
  previous: AnonymousAcademicContext | null,
): AnonymousAcademicContext | null {
  if (selection.institution === "") return null;
  const names = emptyAcademicContextNames();
  if (previous && previous.institutionSlug === selection.institution) {
    names.institutionAr = previous.names.institutionAr;
    names.institutionEn = previous.names.institutionEn;
    if (previous.programSlug === selection.program) {
      names.programAr = previous.names.programAr;
      names.programEn = previous.names.programEn;
    }
  }
  return academicContext(selection.institution, selection.program, names);
}

/**
 * Whether a catalogue URL says anything academic at all.
 *
 * A URL that names no institution, program, level or Subject is the one case where a stored context
 * may seed the address bar. A URL that names any of them was asked for — by a link, by the filter
 * row, or by the back button — and is left exactly as it is.
 */
export function urlCarriesAcademicSelection(
  search: string | URLSearchParams,
): boolean {
  const selection = readSelection(search);
  return (
    selection.institution !== "" ||
    selection.program !== "" ||
    selection.level !== "" ||
    selection.subject !== ""
  );
}
