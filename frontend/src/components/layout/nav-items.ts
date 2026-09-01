import type { Dictionary } from "@/lib/i18n/dictionaries/en";

export interface NavItem {
  href: string;
  label: (t: Dictionary) => string;
}

/**
 * The landing page is not locale-addressed.
 *
 * There is no `/[locale]/page.tsx`, so `/ar` and `/en` are not routes and
 * prefixing this would manufacture a 404. The language is carried instead by
 * the persisted preference, which every visit to a `/[locale]/…` route writes —
 * so arriving here from `/ar/catalog` renders Arabic without the address having
 * to say so.
 *
 * It is a function of the locale anyway, so that every caller asks the same
 * question and a future locale-addressed landing page changes one line rather
 * than every header, footer, and logo in the product.
 */
export function homeHref(_locale: "ar" | "en"): string {
  return "/";
}

export const routes = {
  home: homeHref,
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

/** What the header may know about who is reading, without knowing who they are. */
export type NavigationAudience = {
  /**
   * The visitor holds a Student session.
   *
   * Deliberately narrower than "signed in": My Learning is a Student surface,
   * and offering it to an Admin or to a principal whose role the session did
   * not name would assert something about them the session never said.
   */
  studentSession?: boolean;
};

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
 * So: the sections where the sections are, and real routes everywhere else. A
 * workspace gets neither — its own navigation row sits directly beneath this
 * header, and a second, unrelated set of links above it is not navigation, it
 * is noise.
 *
 * Off the landing page the set now leads with Home. The catalogue was the only
 * route offered, which left the logo as the sole way back to the start of the
 * product — discoverable if you already know that a wordmark is a link, and
 * invisible if you do not. My Learning joins it for a Student, because for a
 * signed-in Student that is the page they actually want and it was reachable
 * only from the account controls at the far end of the bar.
 */
export function primaryNavigation(
  pathname: string,
  locale: "ar" | "en",
  audience: NavigationAudience = {},
): NavItem[] {
  if (pathname === "/") return landingSections;
  if (isWorkspacePath(pathname)) return [];
  const items: NavItem[] = [
    { href: routes.home(locale), label: (t) => t.nav.home },
    { href: routes.catalogue(locale), label: (t) => t.nav.courses },
  ];
  if (audience.studentSession) {
    items.push({ href: routes.dashboard(locale), label: (t) => t.nav.myLearning });
  }
  return items;
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
  return [
    { href: routes.home(locale), label: (t) => t.nav.home },
    { href: routes.catalogue(locale), label: (t) => t.nav.courses },
  ];
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
