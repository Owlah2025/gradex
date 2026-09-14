import * as React from "react";
import { SubjectCatalogue } from "@/components/catalog/subject-catalogue";

export default function SubjectsPage() {
  return (
    // SubjectCatalogue reads the institution, availability, and q query
    // parameters, so it needs a Suspense boundary to stay statically
    // renderable — the same shape the login screen uses for returnTo.
    <React.Suspense fallback={null}>
      <SubjectCatalogue />
    </React.Suspense>
  );
}
