import {
  getCountries,
  getCountryCallingCode,
  parsePhoneNumberFromString,
  type CountryCode,
} from "libphonenumber-js/mobile";

/**
 * The Student's WhatsApp contact number, on the browser side (D-117).
 *
 * The backend is the authority: it validates and normalizes every number to
 * E.164 before storing it, and it is the only thing that decides whether a
 * number is acceptable. This module exists so a Student hears "that is not a
 * mobile number" while typing rather than after a round trip, and so every
 * surface displays and links a stored number the same way.
 *
 * The "mobile" metadata build is deliberate: it accepts only numbers that can
 * receive mobile messages, which is what the backend accepts too, at a fraction
 * of the size of the full metadata.
 */

/** Gradex serves Kuwaiti universities first. Any other country is one choice away. */
export const defaultContactRegion: CountryCode = "KW";

/** Shown first, in this order, before every other country alphabetically. */
const preferredRegions: readonly CountryCode[] = ["KW", "SA", "AE", "QA", "BH", "OM", "EG", "JO"];

export type ContactRegionOption = {
  region: CountryCode;
  callingCode: string;
  label: string;
};

const arabicIndic = /[٠-٩]/g;
const easternArabicIndic = /[۰-۹]/g;

/** An Arabic keyboard types Arabic-Indic digits. They are the same number. */
export function asciiDigits(value: string): string {
  return value
    .replace(arabicIndic, (digit) => String(digit.charCodeAt(0) - 0x0660))
    .replace(easternArabicIndic, (digit) => String(digit.charCodeAt(0) - 0x06f0));
}

/**
 * Every country a Student can choose, labelled in the page's language.
 *
 * `Intl.DisplayNames` supplies the localized country name, so no list of
 * country names is shipped. Where the runtime lacks it, the region code is the
 * label, which is still unambiguous next to its calling code.
 */
export function contactRegionOptions(locale: "ar" | "en"): ContactRegionOption[] {
  let names: Intl.DisplayNames | null = null;
  try {
    names = new Intl.DisplayNames([locale], { type: "region" });
  } catch {
    names = null;
  }
  const label = (region: CountryCode) => names?.of(region) ?? region;
  const option = (region: CountryCode): ContactRegionOption => ({
    region,
    callingCode: getCountryCallingCode(region),
    label: label(region),
  });
  const preferred = preferredRegions.map(option);
  const rest = getCountries()
    .filter((region) => !preferredRegions.includes(region))
    .map(option)
    .sort((left, right) => left.label.localeCompare(right.label, locale));
  return [...preferred, ...rest];
}

export function isContactRegion(value: string): value is CountryCode {
  return (getCountries() as string[]).includes(value);
}

/**
 * The E.164 form of what the Student typed, or `null` when it is not a mobile
 * number. A number typed with "+" or "00" carries its own country and the
 * selected region is ignored; otherwise the selected region supplies it.
 */
export function normalizeContactNumber(input: string, region: CountryCode): string | null {
  let value = asciiDigits(input).trim();
  if (!value || value.length > 32) return null;
  if (!/^\+?[0-9 ().-]+$/.test(value)) return null;
  if (value.startsWith("00")) value = `+${value.slice(2)}`;
  const parsed = parsePhoneNumberFromString(value, region);
  if (!parsed || !parsed.isValid() || parsed.ext) return null;
  return isE164ContactNumber(parsed.number) ? parsed.number : null;
}

/** The stored shape. Mirrors the backend and the database CHECK constraint. */
export function isE164ContactNumber(value: string | null | undefined): value is string {
  return typeof value === "string" && /^\+[1-9][0-9]{6,14}$/.test(value);
}

/** "+965 5001 2345" for display; the raw value where it cannot be parsed. */
export function formatContactNumber(e164: string): string {
  const parsed = parsePhoneNumberFromString(e164);
  return parsed ? parsed.formatInternational() : e164;
}

/** The region and national digits of a stored number, to prefill an edit. */
export function splitContactNumber(e164: string | null | undefined): {
  region: CountryCode;
  national: string;
} {
  if (!isE164ContactNumber(e164)) return { region: defaultContactRegion, national: "" };
  const parsed = parsePhoneNumberFromString(e164);
  if (!parsed?.country) return { region: defaultContactRegion, national: e164 };
  return { region: parsed.country, national: parsed.nationalNumber };
}

/**
 * The direct chat link for a stored number, or `null`. A broken link is worse
 * than none, so a value that is not E.164 never produces one and callers
 * render a "not provided" state instead.
 */
export function contactChatLink(e164: string | null | undefined): string | null {
  return isE164ContactNumber(e164) ? `https://wa.me/${e164.slice(1)}` : null;
}
