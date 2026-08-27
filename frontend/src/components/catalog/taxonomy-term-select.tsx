"use client";

import type { TaxonomyKind, TaxonomyTerm } from "@/lib/api/catalog";
import { Select } from "@/components/ui/select";

type TaxonomyTermSelectProps = {
  kind: TaxonomyKind;
  locale: "ar" | "en";
  terms: TaxonomyTerm[];
  value: string;
  onChange: (termID: string) => void;
  disabled?: boolean;
};

export function taxonomyTermLabel(term: TaxonomyTerm, locale: "ar" | "en"): string {
  return locale === "ar" ? term.label_ar : term.label_en;
}

export function TaxonomyTermSelect({
  kind,
  locale,
  terms,
  value,
  onChange,
  disabled = false,
}: TaxonomyTermSelectProps) {
  const isAr = locale === "ar";
  const matchingTerms = terms.filter((term) => term.kind === kind);
  const label = kind === "MAJOR" ? (isAr ? "التخصص" : "Major") : (isAr ? "المادة" : "Subject");

  return (
    <label className="block text-xs font-semibold text-foreground">
      {label}
      <Select
        controlSize="sm"
        className="mt-1"
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">{isAr ? `اختر ${label}` : `Select ${label}`}</option>
        {matchingTerms.map((term) => (
          <option key={term.id} value={term.id}>
            {taxonomyTermLabel(term, locale)}{term.academic_code ? ` (${term.academic_code})` : ""}
          </option>
        ))}
      </Select>
    </label>
  );
}
