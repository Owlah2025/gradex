import type { PublicPrice } from "@/lib/api/public-catalog";

export function pricePresentation(price: PublicPrice | null | undefined) {
  if (!price || !Number.isSafeInteger(price.minor_units)) return null;
  const regular = typeof price.regular_minor_units === "number" && Number.isSafeInteger(price.regular_minor_units)
    ? price.regular_minor_units
    : price.minor_units;
  const offer = price.offer_minor_units;
  const hasOffer = Number.isSafeInteger(offer) && Number(offer) > 0 && Number(offer) < regular;
  return { regular, effective: hasOffer ? Number(offer) : regular, hasOffer };
}

