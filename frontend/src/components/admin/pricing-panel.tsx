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
  /**
   * Reports whether the Course carries an Admin-set launch price.
   *
   * The server refuses publication without one (`COURSE_PRICE_REQUIRED`), and this panel is already
   * reading the price history the answer lives in. Reporting it upward lets the review workspace
   * name the blocker next to the Approve control instead of only after a refusal. `null` means the
   * history has not been read yet, which is deliberately distinct from "no price".
   */
  onLaunchPriceKnown?: (hasLaunchPrice: boolean | null) => void;
};

/**
 * A launch price is a Course-level price change. The server's own test is the existence of a
 * `course_price_changes` row with no Section (`catalog/pricing.go`), so a record without a
 * `section_id` is exactly that row.
 */
function hasLaunchPrice(history: PriceChangeRecord[]): boolean {
  return history.some((record) => !record.section_id);
}

export function PricingPanel({ courseID, sections, onLaunchPriceKnown }: PricingPanelProps) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [history, setHistory] = useState<PriceChangeRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const loadHistory = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const records = await getCoursePriceHistory(courseID, locale);
      setHistory(records);
      onLaunchPriceKnown?.(hasLaunchPrice(records));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : isAr ? "فشل في تحميل سجل الأسعار" : "Failed to load price history");
      // An unreadable history is not evidence that no price exists, so nothing is asserted.
      onLaunchPriceKnown?.(null);
    } finally {
      setLoading(false);
    }
  }, [courseID, isAr, locale, onLaunchPriceKnown]);

  useEffect(() => { void loadHistory(); }, [loadHistory]);

  return (
    <section data-testid="review-pricing-panel" className="space-y-4 rounded-lg border border-blue-200 bg-white p-4 dark:border-blue-900 dark:bg-slate-900">
      <div>
        <h3 className="font-semibold text-slate-900 dark:text-slate-100">{isAr ? "تسعير المراجعة المُرسلة" : "Submitted Course Pricing"}</h3>
        <p className="mt-1 text-xs text-slate-500">{isAr ? "اختر المقرر أو قسماً بعنوانه المُرسل؛ تُحفظ الهوية داخلياً." : "Choose the Course or a submitted Section by title; identity is carried internally."}</p>
      </div>
      <PricingForm courseID={courseID} locale={locale} sections={sections} onSuccess={loadHistory} />
      <PricingHistoryTable history={history} isLoading={loading} error={error} locale={locale} sections={sections} />
    </section>
  );
}
