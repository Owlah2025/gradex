import { locales, type Locale } from "./config";

function isLocaleSegment(segment: string | undefined): segment is Locale {
  return segment !== undefined && (locales as readonly string[]).includes(segment);
}

/**
 * The locale a path addresses, or `null` when the path is not locale-prefixed.
 *
 * Only `/[locale]/…` routes exist under a locale segment — the landing page, the auth screens, and
 * `/staff` are served without one.
 */
export function localeFromPath(pathname: string | null): Locale | null {
  const segment = (pathname ?? "/").split("/")[1];
  return isLocaleSegment(segment) ? segment : null;
}

/**
 * The equivalent path under `next`, or `null` when this route has no locale-addressed equivalent.
 *
 * Returning `null` is a real answer, not a failure: `/`, `/login`, and `/staff` are not
 * locale-prefixed and there is no `/[locale]/page.tsx`, so prefixing them would manufacture a 404.
 * Callers switch the dictionary in place for those routes instead of navigating.
 *
 * Only the locale *segment* is replaced. Substring replacement would corrupt any path containing
 * those two letters — `/ar/catalog/arabic-101`, `/en/learn/courses/<id>` — which is exactly the
 * hazard this helper exists to remove. Query state is carried across so a language switch never
 * silently discards the visitor's search.
 */
export function switchLocalePath(
  pathname: string | null,
  search: string | null | undefined,
  next: Locale,
): string | null {
  const path = pathname && pathname.startsWith("/") ? pathname : `/${pathname ?? ""}`;
  const segments = path.split("/");
  if (!isLocaleSegment(segments[1])) return null;

  segments[1] = next;
  const rebuilt = segments.join("/") || `/${next}`;
  return `${rebuilt}${normalizeSearch(search)}`;
}

/** Accepts `?q=x`, `q=x`, `""`, `null`, or `undefined` and returns a suffix safe to concatenate. */
function normalizeSearch(search: string | null | undefined): string {
  if (!search) return "";
  const trimmed = search.startsWith("?") ? search.slice(1) : search;
  return trimmed === "" ? "" : `?${trimmed}`;
}

/**
 * The locale-addressed form of an application path, when one exists.
 *
 * `/catalog` under `ar` is `/ar/catalog`. A path that is already locale-
 * addressed is re-addressed rather than double-prefixed, which is the bug a
 * naive concatenation produces the second time a link is built.
 */
export function localePath(path: string, locale: Locale): string {
  const normalized = path.startsWith("/") ? path : `/${path}`;
  const segments = normalized.split("/");
  if (isLocaleSegment(segments[1])) {
    segments[1] = locale;
    return segments.join("/");
  }
  return `/${locale}${normalized === "/" ? "" : normalized}`;
}
