"use client";

import { useCallback, useEffect, useState } from "react";
import { getTaxonomyTerms, type TaxonomyTerm } from "@/lib/api/catalog";
import { useLocale } from "@/lib/i18n/locale-provider";
import { TaxonomyOverrideForm } from "./taxonomy-override-form";
import { TaxonomyTermManagement } from "./taxonomy-term-management";

export function TaxonomyControls({ courseID }: { courseID: string }) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [terms, setTerms] = useState<TaxonomyTerm[]>([]);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setError(null);
    try {
      setTerms(await getTaxonomyTerms(locale));
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : isAr ? "تعذر تحميل المصطلحات" : "Unable to load taxonomy terms");
    }
  }, [isAr, locale]);

  useEffect(() => { void refresh(); }, [refresh]);

  return (
    <section className="space-y-4 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="border-b border-slate-200 pb-3 dark:border-slate-800"><h2 className="text-base font-bold text-slate-900 dark:text-slate-100">{isAr ? "إدارة تصنيف الدورة" : "Course Taxonomy Administration"}</h2><p className="mt-1 text-xs text-slate-500">{isAr ? "إدارة القاموس وتجاوز مراجعة دورة محددة مع الحفاظ على هوية المراجعة الصريحة." : "Manage the vocabulary and override one named Course revision without implicit revision selection."}</p></div>
      {error ? <p className="text-xs text-rose-600">{error}</p> : <><TaxonomyTermManagement terms={terms} refresh={refresh} /><TaxonomyOverrideForm courseID={courseID} terms={terms} /></>}
    </section>
  );
}
