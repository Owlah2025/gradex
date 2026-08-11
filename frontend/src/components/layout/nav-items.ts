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
  { href: "#faq", label: (t) => t.nav.faq },
];

export const routes = {
  catalogue: (locale: "ar" | "en") => `/${locale}/catalog`,
  login: "/login",
  register: "/register",
  dashboard: (locale: "ar" | "en") => `/${locale}/learn/dashboard`,
} as const;
