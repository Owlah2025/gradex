import { apiOrigin } from "./learning-server-request";
import type { SubjectListing } from "./subject-catalogue";

/**
 * Server-side public Subject resolution.
 *
 * # WHY THIS EXISTS SEPARATELY FROM THE CLIENT READ
 *
 * A Subject that does not exist must answer a real HTTP 404, not a 200 carrying
 * a "not found" message. The distinction is not cosmetic: a shared link to a
 * retired Subject, a mistyped code, and a crawler all deserve the status the
 * protocol has for exactly this, and answering 200 tells search engines and
 * monitoring that a missing page is a healthy one.
 *
 * Resolving on the client cannot produce that status, because the document has
 * already been sent by the time the fetch resolves. So the route resolves the
 * Subject on the server and calls `notFound()`, and the interactive demand
 * control stays a nested client component below it.
 *
 * This read is anonymous and forwards no cookie. Public Subject discovery is
 * the same for every visitor, and sending credentials would make the response
 * principal-dependent for no gain.
 */
export async function fetchSubjectOnServer(
  institutionSlug: string,
  value: string,
  locale: "ar" | "en",
): Promise<SubjectListing | null> {
  const url = new URL(
    `/api/v1/catalog/subjects/${encodeURIComponent(institutionSlug)}/${encodeURIComponent(value)}`,
    apiOrigin(),
  );

  const response = await fetch(url, {
    method: "GET",
    cache: "no-store",
    headers: {
      Accept: "application/json, application/problem+json",
      "Accept-Language": locale,
    },
  });

  // 404 is the answer, not a failure: the caller turns it into notFound().
  if (response.status === 404) return null;
  if (!response.ok) {
    throw new Error(`Subject request failed with status ${response.status}`);
  }
  return (await response.json()) as SubjectListing;
}
