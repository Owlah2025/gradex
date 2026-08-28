import type { Locale } from "@/lib/i18n/config";

/** The documented platform fallback when a reader's timezone is not known. */
export const DISPLAY_TIME_ZONE = "Asia/Kuwait";

/**
 * The locale every rendered date is formatted under.
 *
 * Arabic asks for Arabic-Indic digits explicitly. Without `-u-nu-arab` an Arabic screen renders its
 * dates in Latin numerals beside Arabic words, which is the mixed-numeral look the product avoids
 * everywhere else it prints a figure.
 */
export function formatterLocale(locale: Locale): string {
  return locale === "ar" ? "ar-u-nu-arab" : locale;
}

/**
 * One instant, written the way the product writes instants.
 *
 * This exists because it was not one way. The Student's access expiry, the Instructor's roster and
 * the Admin's price history each formatted a timestamp, and the price history did it with a bare
 * `toLocaleString("ar-KW")` — which meant the same instant appeared in Latin digits and in the
 * reader's own timezone on one Admin screen while appearing in Arabic-Indic digits and in Kuwait
 * time on the next. A date that changes its mind between two screens is a date an Administrator
 * cannot use to reconcile anything.
 */
export function formatTimestamp(
  value: string,
  locale: Locale,
  timeZone = DISPLAY_TIME_ZONE,
): string | null {
  const instant = new Date(value);
  if (!Number.isFinite(instant.getTime())) return null;
  return new Intl.DateTimeFormat(formatterLocale(locale), {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone,
  }).format(instant);
}
