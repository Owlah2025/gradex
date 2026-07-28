export function formatFils(
  fils: number | null | undefined,
  locale: "ar" | "en",
): string {
  if (fils === null || fils === undefined) {
    return locale === "ar" ? "غير مخصص" : "Unpriced";
  }
  const kwd = (fils / 1000).toFixed(3);
  return locale === "ar" ? `${kwd} د.ك` : `${kwd} KWD`;
}
