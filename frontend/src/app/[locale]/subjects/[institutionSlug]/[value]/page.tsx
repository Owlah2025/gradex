import * as React from "react";
import { notFound } from "next/navigation";
import { SubjectDetail } from "@/components/catalog/subject-detail";
import { fetchSubjectOnServer } from "@/lib/api/subject-catalogue-server";

/**
 * The Subject detail route.
 *
 * The Subject is resolved here, on the server, so a Subject that does not exist
 * answers a real HTTP 404 through `notFound()` rather than a 200 carrying a
 * "not found" message. A client-side resolution cannot do that: the document
 * has already been sent by the time the fetch settles.
 *
 * Everything interactive — the demand control and its states — stays a client
 * component below, receiving the already-resolved Subject as a prop.
 */
export default async function SubjectDetailPage({
  params,
}: {
  params: Promise<{ locale: string; institutionSlug: string; value: string }>;
}) {
  const { locale, institutionSlug, value } = await params;
  // The route segment is not a validated union, so anything that is not the
  // Arabic locale reads as English rather than being trusted into a fetch.
  const language = locale === "ar" ? "ar" : "en";

  const subject = await fetchSubjectOnServer(institutionSlug, value, language);
  if (subject === null) notFound();

  return (
    // The demand panel reads the `request` intent parameter, which survives the
    // sign-in journey, so this subtree needs its own Suspense boundary.
    <React.Suspense fallback={null}>
      <SubjectDetail subject={subject} />
    </React.Suspense>
  );
}
