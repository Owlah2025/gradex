import type { Locale } from "@/lib/i18n/config";
import { DISPLAY_TIME_ZONE, formatterLocale, formatTimestamp } from "./datetime";

/**
 * The documented platform fallback when a Student timezone is not known.
 *
 * Re-exported rather than redeclared: the shared date module owns the value now, and two constants
 * spelled "Asia/Kuwait" in two files is how they come to disagree.
 */
export const DEFAULT_DISPLAY_TIME_ZONE = DISPLAY_TIME_ZONE;

export type FormattedLearningExpiry = {
  /** The original RFC 3339 value, retained for the machine-readable attribute. */
  dateTime: string;
  text: string;
};

export function formatLearningExpiry(
  expiresAt: string | null,
  locale: Locale,
  timeZone = DEFAULT_DISPLAY_TIME_ZONE,
): FormattedLearningExpiry | null {
  if (expiresAt === null) return null;
  const text = formatTimestamp(expiresAt, locale, timeZone);
  if (text === null) return null;

  return { dateTime: expiresAt, text };
}

export function formatLearningInteger(value: number, locale: Locale): string {
  return new Intl.NumberFormat(formatterLocale(locale), {
    maximumFractionDigits: 0,
    useGrouping: false,
  }).format(value);
}

export function formatLearningPercent(value: number, locale: Locale): string {
  const sign = locale === "ar" ? "٪" : "%";
  return `${formatLearningInteger(value, locale)}${sign}`;
}

export function formatLearningPositionSeconds(value: number, locale: Locale): string {
  const safeValue = Number.isFinite(value) && value >= 0 ? value : 0;
  return new Intl.NumberFormat(formatterLocale(locale), {
    maximumFractionDigits: 6,
    useGrouping: false,
  }).format(safeValue);
}
