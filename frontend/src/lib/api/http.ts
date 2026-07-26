import { isProblem, ProblemError } from "./problem";

const apiBase = "/api/v1";

let anonymousCSRFToken: string | null = null;
let bootstrapRequest: Promise<string> | null = null;

export async function ensureAnonymousBrowser(): Promise<string> {
  if (anonymousCSRFToken) return anonymousCSRFToken;
  if (!bootstrapRequest) {
    bootstrapRequest = fetch(`${apiBase}/session/bootstrap`, {
      method: "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "application/json, application/problem+json" },
    })
      .then(readJSON<{ csrf_token: string }>)
      .then((body) => {
        anonymousCSRFToken = body.csrf_token;
        return body.csrf_token;
      })
      .finally(() => {
        bootstrapRequest = null;
      });
  }
  return bootstrapRequest;
}

export async function getJSON<T>(
  path: string,
  language: "ar" | "en",
): Promise<T> {
  await ensureAnonymousBrowser();
  const response = await fetch(`${apiBase}${path}`, {
    method: "GET",
    credentials: "same-origin",
    cache: "no-store",
    headers: {
      Accept: "application/json, application/problem+json",
      "Accept-Language": language,
    },
  });
  return readJSON<T>(response);
}

export async function postJSON<T>(
  path: string,
  body: unknown,
  language: "ar" | "en",
): Promise<T> {
  const csrf = await ensureAnonymousBrowser();
  const response = await fetch(`${apiBase}${path}`, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: {
      Accept: "application/json, application/problem+json",
      "Accept-Language": language,
      "Content-Type": "application/json",
      "X-CSRF-Token": csrf,
    },
    body: JSON.stringify(body),
  });
  return readJSON<T>(response);
}

async function readJSON<T>(response: Response): Promise<T> {
  const body: unknown = await response.json().catch(() => null);
  if (response.ok) return body as T;
  if (isProblem(body)) throw new ProblemError(body);
  throw new Error("The server returned an unreadable response.");
}
