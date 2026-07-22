"use client";

import * as React from "react";
import { en, type Dictionary } from "./dictionaries/en";
import { ar } from "./dictionaries/ar";
import {
  defaultLocale,
  localeDir,
  STORAGE_KEY,
  type Locale,
} from "./config";

const dictionaries: Record<Locale, Dictionary> = { en, ar };

type LocaleContextValue = {
  locale: Locale;
  dir: "ltr" | "rtl";
  t: Dictionary;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
};

const LocaleContext = React.createContext<LocaleContextValue | null>(null);

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = React.useState<Locale>(defaultLocale);

  // Restore the user's saved choice after mount (avoids hydration mismatch).
  React.useEffect(() => {
    const saved = window.localStorage.getItem(STORAGE_KEY) as Locale | null;
    if (saved === "en" || saved === "ar") setLocaleState(saved);
  }, []);

  // Keep <html lang/dir> in sync with the active locale.
  React.useEffect(() => {
    const root = document.documentElement;
    root.lang = locale;
    root.dir = localeDir[locale];
  }, [locale]);

  const setLocale = React.useCallback((next: Locale) => {
    setLocaleState(next);
    window.localStorage.setItem(STORAGE_KEY, next);
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

export function useLocale() {
  const ctx = React.useContext(LocaleContext);
  if (!ctx) throw new Error("useLocale must be used within a LocaleProvider");
  return ctx;
}
