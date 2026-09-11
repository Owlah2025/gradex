const localApiOrigin = "http://127.0.0.1:8080";
const sessionCookieName = "__Host-gradex_session";
const deviceCookieName = "__Host-gradex_device";

export type ProtectedServerRequest = {
  url: string;
  init: RequestInit;
};

function protectedCookies(cookieHeader: string | null): string | null {
  if (!cookieHeader) return null;
  const cookies: string[] = [];
  for (const part of cookieHeader.split(";")) {
    const separator = part.indexOf("=");
    if (separator < 0) continue;
    const name = part.slice(0, separator).trim();
    if (name === sessionCookieName || name === deviceCookieName) {
      cookies.push(`${name}=${part.slice(separator + 1).trim()}`);
    }
  }
  return cookies.length > 0 ? cookies.join("; ") : null;
}

function apiOrigin(): string {
  const configuredOrigin = process.env.GRADEX_API_ORIGIN;
  if (!configuredOrigin && process.env.NODE_ENV === "production") {
    throw new Error("GRADEX_API_ORIGIN is required in production.");
  }
  const parsedOrigin = new URL(configuredOrigin ?? localApiOrigin);
  if (parsedOrigin.protocol !== "http:" && parsedOrigin.protocol !== "https:") {
    throw new Error("GRADEX_API_ORIGIN must use HTTP or HTTPS.");
  }
  return parsedOrigin.origin;
}

export function buildProtectedServerRequest(
  path: string,
  locale: "ar" | "en",
  cookie: string | null,
): ProtectedServerRequest {
  const requestHeaders: Record<string, string> = {
    Accept: "application/json, application/problem+json",
    "Accept-Language": locale,
  };
  const cookies = protectedCookies(cookie);
  if (cookies) requestHeaders.Cookie = cookies;
  return {
    url: new URL(`/api/v1${path}`, apiOrigin()).toString(),
    init: {
      method: "GET",
      credentials: "include",
      cache: "no-store",
      headers: requestHeaders,
    },
  };
}
