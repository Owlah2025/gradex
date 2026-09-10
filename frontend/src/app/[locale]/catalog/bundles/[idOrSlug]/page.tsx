import { BundleDetail } from "@/components/catalog/bundle-detail";

export default async function BundleDetailPage({ params }: { params: Promise<{ locale: string; idOrSlug: string }> }) {
  const { locale: requestedLocale, idOrSlug } = await params;
  const locale = requestedLocale === "en" ? "en" : "ar";
  return <BundleDetail idOrSlug={idOrSlug} routeLocale={locale} />;
}

