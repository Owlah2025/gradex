/**
 * Dates, said the way each locale says them.
 *
 * Three surfaces had grown their own version of this — the review queue and the Courses directory
 * called `toLocaleDateString(locale)` inline, the reports workspace kept a private `Intl` formatter
 * with a different style — so the same timestamp read three different ways depending on which Admin
 * screen you were on.
 *
 * `numberingSystem: "latn"` is deliberate. Arabic's default numbering system in `Intl` is
 * Eastern Arabic numerals, and an operational screen mixes dates with email addresses, counts and
 * identifiers that are all Latin digits; switching numeral systems mid-row is a legibility cost the
 * product does not need to pay. Kuwaiti interfaces overwhelmingly use Latin digits already.
 */
const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(locale: "ar" | "en", style: "date" | "dateTime"): Intl.DateTimeFormat {
  const key = `${locale}:${style}`;
  const existing = formatters.get(key);
  if (existing) return existing;
  const created = new Intl.DateTimeFormat(locale === "ar" ? "ar-KW" : "en-GB", {
    dateStyle: "medium",
    ...(style === "dateTime" ? { timeStyle: "short" as const } : {}),
    numberingSystem: "latn",
  });
  formatters.set(key, created);
  return created;
}

/** A day. Use where the time of day is not part of what the reader is deciding. */
export function formatDate(value: string, locale: "ar" | "en"): string {
  const parsed = new Date(value);
  // A timestamp the server did not send, or sent in a shape this browser cannot read, is not worth
  // rendering as "Invalid Date" in the middle of a table.
  if (Number.isNaN(parsed.getTime())) return "—";
  return formatter(locale, "date").format(parsed);
}

/** A day and a time, for surfaces where the ordering within a day matters. */
export function formatDateTime(value: string, locale: "ar" | "en"): string {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "—";
  return formatter(locale, "dateTime").format(parsed);
}
