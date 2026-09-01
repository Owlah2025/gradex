"use client";

import * as React from "react";
import { usePathname } from "next/navigation";
import { en, type Dictionary } from "./dictionaries/en";
import { ar } from "./dictionaries/ar";
import {
  defaultLocale,
  localeDir,
  STORAGE_KEY,
  type Locale,
} from "./config";
import { localeFromPath } from "./locale-path";
import { readLocaleCookie, writeLocaleCookie } from "./locale-cookie";

const dictionaries: Record<Locale, Dictionary> = { en, ar };

type LocaleContextValue = {
  locale: Locale;
  dir: "ltr" | "rtl";
  t: Dictionary;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
};

const LocaleContext = React.createContext<LocaleContextValue | null>(null);

/**
 * The active language, resolved once and the same on both sides of hydration.
 *
 * Two things decide it, in this order:
 *
 *  1. The URL, when the route is locale-addressed. `/ar/catalog/…` renders
 *     Arabic and `/en/catalog/…` renders English, always. A saved preference
 *     never overrides an address the visitor is looking at — that was one half
 *     of the reported "language changes while navigating" defect: an English
 *     reader opening a shared `/ar/…` link, or an Arabic reader whose stale
 *     preference re-flipped a page they had explicitly navigated to.
 *
 *  2. The stored preference, for the routes that carry no locale segment — the
 *     landing page, the auth screens, `/staff`.
 *
 * The URL is read with `usePathname`, which is correct during server rendering
 * too, so a locale-addressed route renders the right dictionary on the first
 * byte without a cookie, without a stored value, and without the root layout
 * having to become dynamic to find out.
 *
 * The routes that carry no locale segment — the landing page, the admission
 * screens, `/staff` — are answered by the stored preference as this mounts. See
 * the effect below for why that one corrected frame is preferred to reading the
 * cookie in the root layout.
 */
export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const routeLocale = localeFromPath(pathname);
  const [preferred, setPreferred] = React.useState<Locale>(defaultLocale);

  // The address wins where it says anything; the preference answers everywhere
  // else. Deriving rather than storing this is what keeps the two from drifting
  // apart while the visitor navigates.
  const locale = routeLocale ?? preferred;

  // Visiting an explicit localized URL *is* a language choice, and it is
  // recorded as one. Before this, `/ar/catalog` set the language for that page
  // and nothing else, so following an ordinary link to sign in landed on a
  // screen still rendering the older, unrelated preference.
  React.useEffect(() => {
    if (!routeLocale) return;
    setPreferred(routeLocale);
    persistLocale(routeLocale);
  }, [routeLocale]);

  // On a route the URL does not name, the stored preference is applied as this
  // mounts.
  //
  // This is the one place the language is corrected after the first paint, and
  // it is deliberately scoped to the four screens that carry no locale segment:
  // the landing page, the admission screens, and `/staff`. Removing that frame
  // would mean reading the cookie in the root layout, which makes the root
  // layout dynamic — and a dynamic root layout re-renders and re-serializes on
  // every client navigation in the product. That measured out at roughly twice
  // the soft-navigation time on the Admin review workspace, which is a worse
  // trade than one corrected frame on four screens.
  //
  // Locale-addressed routes never reach this: `routeLocale` already answered,
  // on the server as well as the client, so they have no frame to correct.
  React.useEffect(() => {
    if (routeLocale) return;
    const saved = savedLocale();
    if (!saved || saved === preferred) return;
    setPreferred(saved);
    persistLocale(saved);
  }, [routeLocale, preferred]);

  // Keep `<html lang/dir>` truthful. The document element is outside the React
  // tree the root layout renders, so it is written here rather than derived.
  React.useEffect(() => {
    const root = document.documentElement;
    root.lang = locale;
    root.dir = localeDir[locale];
  }, [locale]);

  const setLocale = React.useCallback((next: Locale) => {
    setPreferred(next);
    persistLocale(next);
  }, []);

  const toggleLocale = React.useCallback(() => {
    setLocale(locale === "en" ? "ar" : "en");
  }, [locale, setLocale]);

  const value = React.useMemo<LocaleContextValue>(
    () => ({
      locale,
      dir: localeDir[locale],
      t: dictionaries[locale],
      setLocale,
      toggleLocale,
    }),
    [locale, setLocale, toggleLocale],
  );

  return (
    <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
  );
}

/**
 * Writes the preference to both stores.
 *
 * The cookie is the one the server reads and therefore the one that matters for
 * the first paint. `localStorage` is kept in step because other code and older
 * sessions still read it, and having the two disagree would reintroduce exactly
 * the drift this is meant to end.
 */
/**
 * The stored preference, if this browser holds one.
 *
 * The cookie is consulted first because it is the one this application writes
 * for the server's benefit and the one a language switch always sets;
 * `localStorage` is the fallback, both for a visitor whose choice predates the
 * cookie and for the case where cookies are refused but storage is not.
 *
 * Read defensively: a hardened or private-mode browser can throw on property
 * access rather than return null, and a language preference is not worth an
 * exception on the way to the first render.
 */
function savedLocale(): Locale | null {
  const fromCookie = readLocaleCookie(
    typeof document === "undefined" ? null : document.cookie,
  );
  if (fromCookie) return fromCookie;
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    return stored === "ar" || stored === "en" ? stored : null;
  } catch {
    return null;
  }
}

function persistLocale(locale: Locale): void {
  writeLocaleCookie(locale);
  try {
    window.localStorage.setItem(STORAGE_KEY, locale);
  } catch {
    // A refused storage is not a failure: the cookie already carries the
    // preference and the server reads that one.
  }
}

export function useLocale() {
  const ctx = React.useContext(LocaleContext);
  if (!ctx) throw new Error("useLocale must be used within a LocaleProvider");
  return ctx;
}
