import { locales, type Locale } from "./config";

/**
 * The one place the visitor's language choice is written down.
 *
 * A cookie rather than `localStorage` because the server has to be able to read
 * it. The previous implementation restored the saved language in an effect
 * after hydration, which meant the first paint of every screen without a
 * `/[locale]/…` segment — the landing page, sign in, register, verify — was
 * rendered in the default language and then replaced. That is the language
 * flash, and no amount of client-side care removes it: by the time the browser
 * can read `localStorage`, the wrong markup has already been sent.
 */
export const localeCookieName = "gradex_locale";

/** A year. The choice is a preference, not a session fact. */
export const localeCookieMaxAgeSeconds = 60 * 60 * 24 * 365;

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (locales as readonly string[]).includes(value);
}

/**
 * Writes the preference where both the browser and the server can see it.
 *
 * `SameSite=Lax` because the value must survive an ordinary top-level
 * navigation back into Gradex — following a link from an email, for instance —
 * and `Lax` is the strongest setting that does. It carries no authority and no
 * personal data, so it is not `__Host-` prefixed and does not need to be:
 * nothing is granted by presenting it.
 */
export function writeLocaleCookie(locale: Locale): void {
  if (typeof document === "undefined") return;
  const secure = window.location.protocol === "https:" ? "; Secure" : "";
  document.cookie =
    `${localeCookieName}=${locale}; Path=/; Max-Age=${localeCookieMaxAgeSeconds}; SameSite=Lax${secure}`;
}

/** Reads the preference from a raw cookie header or `document.cookie`. */
export function readLocaleCookie(cookieHeader: string | undefined | null): Locale | null {
  if (!cookieHeader) return null;
  for (const part of cookieHeader.split(";")) {
    const [name, ...rest] = part.trim().split("=");
    if (name !== localeCookieName) continue;
    const value = rest.join("=");
    if (isLocale(value)) return value;
  }
  return null;
}
