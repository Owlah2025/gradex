import type { Locale } from "@/lib/i18n/config";

/** The documented platform fallback when a Student timezone is not known. */
export const DEFAULT_DISPLAY_TIME_ZONE = "Asia/Kuwait";

export type FormattedLearningExpiry = {
  /** The original RFC 3339 value, retained for the machine-readable attribute. */
  dateTime: string;
  text: string;
};

function formatterLocale(locale: Locale): string {
  return locale === "ar" ? "ar-u-nu-arab" : locale;
}

export function formatLearningExpiry(
  expiresAt: string | null,
  locale: Locale,
  timeZone = DEFAULT_DISPLAY_TIME_ZONE,
): FormattedLearningExpiry | null {
  if (expiresAt === null) return null;
  const instant = new Date(expiresAt);
  if (!Number.isFinite(instant.getTime())) return null;

  return {
    dateTime: expiresAt,
    text: new Intl.DateTimeFormat(formatterLocale(locale), {
      dateStyle: "medium",
      timeStyle: "short",
      timeZone,
    }).format(instant),
  };
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
