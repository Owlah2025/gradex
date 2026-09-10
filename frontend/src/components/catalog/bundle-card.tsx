import Link from "next/link";
import { Layers3 } from "lucide-react";
import type { PublicBundle } from "@/lib/api/public-catalog";
import { ThumbnailImage } from "./thumbnail-image";
import { PriceDisplay } from "./price-display";
import { bundleCourseCount, bundleDetailHref } from "./bundle-presentation";
import { useLocale } from "@/lib/i18n/locale-provider";

export function BundleCard({ bundle, locale }: { bundle: PublicBundle; locale: "ar" | "en" }) {
  const { t } = useLocale();
  const countLabel = bundleCourseCount(bundle.course_count, t.bundles.courseCount);
  const viewLabel = t.bundles.view;
  return (
    <article className="flex h-full flex-col rounded-xl border border-border bg-card p-5 text-card-foreground shadow-sm" data-testid="bundle-card">
      <div className="relative h-32" aria-hidden>
        {bundle.members.slice(0, 3).map((member, index) => (
          <div
            key={member.course_id}
            className="absolute top-0 h-28 w-[58%] overflow-hidden rounded-lg bg-muted ring-2 ring-card"
            style={{ insetInlineStart: `${index * 18}%`, zIndex: index + 1 }}
          >
            {member.thumbnail?.card_url ? (
              <ThumbnailImage src={member.thumbnail.card_url} className="h-full w-full object-cover" />
            ) : (
              <div className="flex h-full items-center justify-center bg-muted">
                <Layers3 className="size-7 text-muted-foreground" />
              </div>
            )}
          </div>
        ))}
        {bundle.members.length === 0 ? (
          <div className="flex h-28 items-center justify-center rounded-lg bg-muted">
            <Layers3 className="size-8 text-muted-foreground" />
          </div>
        ) : null}
      </div>
      <p className="mt-3 text-sm font-semibold text-muted-foreground">{countLabel}</p>
      <h3 className="mt-1 text-balance font-display text-xl font-bold text-foreground"><bdi>{bundle.title}</bdi></h3>
      <PriceDisplay price={bundle.price} locale={locale} className="mt-4" compact />
      <Link
        href={bundleDetailHref(locale, bundle.slug || bundle.id)}
        className="mt-5 inline-flex min-h-11 items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-bold text-primary-foreground hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      >
        {viewLabel}
      </Link>
    </article>
  );
}
