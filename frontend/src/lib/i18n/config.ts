export const locales = ["en", "ar"] as const;
export type Locale = (typeof locales)[number];

export const defaultLocale: Locale = "ar";

export const localeDir: Record<Locale, "ltr" | "rtl"> = {
  en: "ltr",
  ar: "rtl",
};

/** Label shown on the toggle for the language it switches *to*. */
export const localeSwitchLabel: Record<Locale, string> = {
  en: "ع", // currently English → offer Arabic
  ar: "EN", // currently Arabic → offer English
};

export const STORAGE_KEY = "gradex.locale";
