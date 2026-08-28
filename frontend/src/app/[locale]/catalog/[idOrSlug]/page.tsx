import { CourseDetail } from "@/components/catalog/course-detail";

export default async function CatalogueDetailPage({
  params,
}: {
  params: Promise<{ locale: string; idOrSlug: string }>;
}) {
  const { locale: requestedLocale, idOrSlug } = await params;
  // The same narrowing the language-addressable Student routes use. This URL *is* the language
  // authority here, which is what lets the page read the catalogue in the right language on its
  // first attempt rather than reading it twice.
  const locale = requestedLocale === "en" ? "en" : "ar";
  return <CourseDetail idOrSlug={idOrSlug} routeLocale={locale} />;
}
