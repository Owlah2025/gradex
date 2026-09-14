import { isProblem, ProblemError } from "./problem";

/**
 * One anonymous GET against the public catalogue surface.
 *
 * Shared by the Course catalogue and the Subject catalogue, which had byte-identical
 * copies of this differing only in an error string. Two copies of the public
 * read contract is how one of them quietly stops sending Accept-Language, or
 * starts caching, without the other noticing.
 *
 * No credentials and no CSRF token: every route behind this is anonymous, and
 * sending credentials would make a shared, cacheable response
 * principal-dependent.
 */
export async function publicCatalogRequest<T>(
  path: string,
  locale: "ar" | "en",
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetch(`/api/v1/catalog${path}`, {
    headers: {
      Accept: "application/json, application/problem+json",
      "Accept-Language": locale,
    },
    cache: "no-store",
    // Lets a caller whose query has moved on stop waiting for an answer it can
    // no longer use. An aborted fetch rejects with an AbortError, which callers
    // must distinguish from a real failure.
    signal,
  });
  const body: unknown = await response.json();
  if (!response.ok)
    throw isProblem(body)
      ? new ProblemError(body)
      : new Error("Public catalogue request failed");
  return body as T;
}
