export function bundleDetailHref(locale: "ar" | "en", slugOrID: string): string {
  return `/${locale}/catalog/bundles/${encodeURIComponent(slugOrID)}`;
}

// The copy itself lives in the dictionaries, like every other localized string.
// This only fills the template so the card and the Admin workspace cannot drift
// into two different renderings of the same label.
export function bundleCourseCount(count: number, template: string): string {
  return template.replace("{count}", String(count));
}

