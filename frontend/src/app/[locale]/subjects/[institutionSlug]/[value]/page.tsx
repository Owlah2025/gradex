import * as React from "react";
import { SubjectDetail } from "@/components/catalog/subject-detail";

export default async function SubjectDetailPage({
  params,
}: {
  params: Promise<{ institutionSlug: string; value: string }>;
}) {
  const { institutionSlug, value } = await params;
  return (
    // The demand panel reads the `request` intent parameter, which survives the
    // sign-in journey, so this subtree needs its own Suspense boundary.
    <React.Suspense fallback={null}>
      <SubjectDetail institutionSlug={institutionSlug} value={value} />
    </React.Suspense>
  );
}
