"use client";

import { useCallback, useEffect, useState } from "react";
import {
  getCoursePriceHistory,
  type PriceChangeRecord,
  type SectionWire,
} from "@/lib/api/catalog";
import { useLocale } from "@/lib/i18n/locale-provider";
import { PricingForm } from "./pricing-form";
import { PricingHistoryTable } from "./pricing-history-table";

type PricingPanelProps = {
  courseID: string;
  sections: SectionWire[];
};

export function PricingPanel({ courseID, sections }: PricingPanelProps) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [history, setHistory] = useState<PriceChangeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadHistory = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setHistory(await getCoursePriceHistory(courseID, locale));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : isAr ? "فشل في تحميل سجل الأسعار" : "Failed to load price history");
    } finally {
      setLoading(false);
    }
  }, [courseID, isAr, locale]);

  useEffect(() => { void loadHistory(); }, [loadHistory]);

  return (
    <section data-testid="review-pricing-panel" className="space-y-4 rounded-lg border border-blue-200 bg-white p-4 dark:border-blue-900 dark:bg-slate-900">
      <div>
        <h3 className="font-semibold text-slate-900 dark:text-slate-100">{isAr ? "تسعير المراجعة المُرسلة" : "Submitted Course Pricing"}</h3>
        <p className="mt-1 text-xs text-slate-500">{isAr ? "اختر الدورة أو قسماً بعنوانه المُرسل؛ تُحفظ الهوية داخلياً." : "Choose the Course or a submitted Section by title; identity is carried internally."}</p>
      </div>
      <PricingForm courseID={courseID} locale={locale} sections={sections} onSuccess={loadHistory} />
      <PricingHistoryTable history={history} isLoading={loading} error={error} locale={locale} sections={sections} />
    </section>
  );
}
