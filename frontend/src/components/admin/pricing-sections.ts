import type { SectionWire } from "@/lib/api/catalog";

export function submittedSectionLabel(section: SectionWire, locale: "ar" | "en"): string {
  const titles = locale === "ar"
    ? `${section.title_ar} — ${section.title_en}`
    : `${section.title_en} — ${section.title_ar}`;
  return `${locale === "ar" ? "القسم" : "Section"} ${section.position}: ${titles}`;
}

export function pricingScopeLabel(
  sectionID: string,
  sections: SectionWire[],
  locale: "ar" | "en",
): string {
  const section = sections.find((candidate) => candidate.id === sectionID);
  if (section) return submittedSectionLabel(section, locale);
  return locale === "ar" ? "قسم غير متاح" : "Unavailable Section";
}
