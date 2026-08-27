import type { Dictionary } from "@/lib/i18n/dictionaries/en";

export interface NavItem {
  href: string;
  label: (t: Dictionary) => string;
}

export const routes = {
  catalogue: (locale: "ar" | "en") => `/${locale}/catalog`,
  login: "/login",
  register: "/register",
  dashboard: (locale: "ar" | "en") => `/${locale}/learn/dashboard`,
} as const;

/**
 * The landing page's own sections.
 *
 * These are in-page anchors and only ever resolve on the landing page.
 */
const landingSections: NavItem[] = [
  { href: "#courses", label: (t) => t.nav.courses },
  { href: "#why", label: (t) => t.nav.why },
  { href: "#faq", label: (t) => t.nav.faq },
];

/**
 * The shared header's primary navigation, for the page it is actually on.
 *
 * `Navbar` is not the landing page's header. It is also the header over the
 * public catalogue, over public Course Details, and over every Admin and
 * Instructor workspace screen. On all of those it was rendering `#courses`,
 * `#why` and `#faq` — three controls that look like navigation, are keyboard
 * reachable, are announced as links, and move the reader nowhere at all,
 * because the sections they point at exist on one page in the product.
 *
 * So: the sections where the sections are, and a real route to the catalogue
 * everywhere else. A workspace gets neither — its own navigation row sits
 * directly beneath this header, and a second, unrelated set of links above it
 * is not navigation, it is noise.
 */
export function primaryNavigation(
  pathname: string,
  locale: "ar" | "en",
): NavItem[] {
  if (pathname === "/") return landingSections;
  if (isWorkspacePath(pathname)) return [];
  return [{ href: routes.catalogue(locale), label: (t) => t.nav.courses }];
}

/**
 * The footer's Explore column, for the page it is on.
 *
 * Same problem as the header — the footer sits under the workspace screens too,
 * and offered the landing page's anchors there — but a different answer: a
 * footer column with nothing in it is worse than one useful link, so this never
 * returns empty.
 */
export function exploreNavigation(
  pathname: string,
  locale: "ar" | "en",
): NavItem[] {
  if (pathname === "/") return landingSections;
  return [{ href: routes.catalogue(locale), label: (t) => t.nav.courses }];
}

/**
 * A screen that carries its own workspace navigation directly below the header.
 *
 * Matched on the segment after an optional locale prefix, so `/en/admin/...`
 * and `/instructor/...` are both recognised and `/administration` is not.
 */
export function isWorkspacePath(pathname: string): boolean {
  const segments = pathname.split("/").filter(Boolean);
  const head = segments[0] === "ar" || segments[0] === "en"
    ? segments[1]
    : segments[0];
  return head === "admin" || head === "instructor" || head === "staff";
}
