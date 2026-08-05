const localApiOrigin = "http://127.0.0.1:8080";
const sessionCookieName = "__Host-gradex_session";

export type ProtectedServerRequest = {
  url: string;
  init: RequestInit;
};

function sessionCookie(cookieHeader: string | null): string | null {
  if (!cookieHeader) return null;
  for (const part of cookieHeader.split(";")) {
    const separator = part.indexOf("=");
    if (separator < 0) continue;
    const name = part.slice(0, separator).trim();
    if (name === sessionCookieName) return `${sessionCookieName}=${part.slice(separator + 1).trim()}`;
  }
  return null;
}

function apiOrigin(): string {
  const configuredOrigin = process.env.GRADEX_API_ORIGIN ?? localApiOrigin;
  const parsedOrigin = new URL(configuredOrigin);
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
  const session = sessionCookie(cookie);
  if (session) requestHeaders.Cookie = session;
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
