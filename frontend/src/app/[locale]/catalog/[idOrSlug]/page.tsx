import { CatalogueDetail } from "@/components/catalog/public-catalogue";
export default async function CatalogueDetailPage({ params }: { params: Promise<{ idOrSlug: string }> }) {
  const { idOrSlug } = await params;
  return <CatalogueDetail idOrSlug={idOrSlug} />;
}
