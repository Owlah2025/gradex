import type { Dictionary } from "@/lib/i18n/dictionaries/en";

export interface NavItem {
  href: string;
  label: (t: Dictionary) => string;
}

/** Primary nav. On the landing page these resolve to in-page section anchors;
 *  the same hrefs work as routes once those screens exist. */
export const navItems: NavItem[] = [
  { href: "#courses", label: (t) => t.nav.courses },
  { href: "#why", label: (t) => t.nav.why },
  { href: "#instructors", label: (t) => t.nav.instructors },
  { href: "#faq", label: (t) => t.nav.faq },
];

export const routes = {
  courses: "/courses",
  login: "/login",
  register: "/register",
  dashboard: "/dashboard",
} as const;
