/**
 * The price, split into the parts a premium price row wants to weight differently.
 *
 * The amount and the unit are returned separately so a surface can set "12.500" large and "KWD"
 * quiet without re-deriving the KWD-from-fils arithmetic or the Arabic unit. `formatFils` remains
 * the single flat string for everywhere that just needs the price in one span.
 */
export type FilsParts =
  | { priced: true; amount: string; unit: string }
  | { priced: false; label: string };

export function formatFilsParts(
  fils: number | null | undefined,
  locale: "ar" | "en",
): FilsParts {
  if (fils === null || fils === undefined) {
    return { priced: false, label: locale === "ar" ? "غير مخصص" : "Unpriced" };
  }
  return {
    priced: true,
    amount: (fils / 1000).toFixed(3),
    unit: locale === "ar" ? "د.ك" : "KWD",
  };
}

export function formatFils(
  fils: number | null | undefined,
  locale: "ar" | "en",
): string {
  const parts = formatFilsParts(fils, locale);
  return parts.priced ? `${parts.amount} ${parts.unit}` : parts.label;
}
