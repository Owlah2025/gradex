import { formatFils } from "@/lib/formatters/currency";
import type { PublicPrice } from "@/lib/api/public-catalog";
import { cn } from "@/lib/utils";
import { pricePresentation } from "./price-presentation";

type PriceDisplayProps = {
  price?: PublicPrice | null;
  locale: "ar" | "en";
  className?: string;
  compact?: boolean;
};

export function PriceDisplay({ price, locale, className, compact = false }: PriceDisplayProps) {
  const presentation = pricePresentation(price);
  if (!presentation) return null;
  const { regular, effective, hasOffer } = presentation;
  const regularLabel = locale === "ar" ? "السعر الأساسي" : "Regular price";
  const offerLabel = locale === "ar" ? "سعر العرض" : "Offer price";

  return (
    <div
      className={cn(
        // `relative` is load-bearing. The screen-reader labels below are
        // `sr-only`, which positions them absolutely; with no positioned
        // ancestor they resolve against the initial containing block and are
        // laid out at their static position. Inside a horizontally scrolled
        // container — the Admin purchase-requests table — that puts them at the
        // scrolled x offset and makes the whole page scroll sideways on a
        // phone. Containing them here keeps the label invisible and inert.
        "relative flex flex-wrap items-baseline gap-x-2 gap-y-1 tabular-nums",
        className,
      )}
      data-testid={hasOffer ? "offer-price" : "regular-price"}
    >
      {hasOffer ? (
        <span className="text-sm text-muted-foreground">
          <span className="sr-only">{regularLabel}: </span>
          <del aria-label={`${regularLabel}: ${formatFils(regular, locale)}`}>
            <bdi>{formatFils(regular, locale)}</bdi>
          </del>
        </span>
      ) : null}
      <span className={cn("font-bold text-foreground", compact ? "text-base" : "text-xl")}>
        <span className="sr-only">{hasOffer ? `${offerLabel}: ` : `${regularLabel}: `}</span>
        <bdi>{formatFils(effective, locale)}</bdi>
      </span>
    </div>
  );
}
