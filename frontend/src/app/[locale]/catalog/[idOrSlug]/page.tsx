import { CatalogueDetail } from "@/components/catalog/public-catalogue";
export default function CatalogueDetailPage({ params }: { params: { idOrSlug: string } }) { return <CatalogueDetail idOrSlug={params.idOrSlug} />; }
