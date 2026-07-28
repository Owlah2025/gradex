"use client";

import React, { useEffect, useState, useCallback } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  getCoursePriceHistory,
  type PriceChangeRecord,
} from "@/lib/api/catalog";
import { PricingForm } from "./pricing-form";
import { PricingHistoryTable } from "./pricing-history-table";

export interface PricingModalProps {
  courseID: string;
  courseTitleAr?: string;
  courseTitleEn?: string;
  onClose: () => void;
}

export function PricingModal({
  courseID,
  courseTitleAr,
  courseTitleEn,
  onClose,
}: PricingModalProps) {
  const { locale } = useLocale();
  const isAr = locale === "ar";

  const displayTitle = isAr
    ? courseTitleAr || courseID
    : courseTitleEn || courseID;

  const [history, setHistory] = useState<PriceChangeRecord[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(true);
  const [historyError, setHistoryError] = useState<string | null>(null);

  const fetchHistory = useCallback(async () => {
    setIsLoadingHistory(true);
    setHistoryError(null);
    try {
      const data = await getCoursePriceHistory(courseID, locale);
      setHistory(data);
    } catch (err: unknown) {
      const msg =
        err instanceof Error
          ? err.message
          : isAr
            ? "فشل في تحميل سجل الأسعار"
            : "Failed to load price history";
      setHistoryError(msg);
    } finally {
      setIsLoadingHistory(false);
    }
  }, [courseID, isAr, locale]);

  useEffect(() => {
    fetchHistory();
  }, [fetchHistory]);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="pricing-modal-title"
      className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50"
    >
      <div className="bg-white dark:bg-slate-900 rounded-xl p-6 max-w-2xl w-full space-y-6 shadow-xl border max-h-[90vh] overflow-y-auto">
        <div className="flex justify-between items-center border-b pb-3">
          <div>
            <h3 id="pricing-modal-title" className="text-lg font-bold text-slate-900 dark:text-slate-100">
              {isAr ? "إدارة التسعير وسجل التغييرات" : "Pricing Management & Audit Log"}
            </h3>
            <p className="text-xs text-slate-500">
              {displayTitle} ({courseID})
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={isAr ? "إغلاق" : "Close"}
            className="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-lg font-bold"
          >
            ×
          </button>
        </div>

        <PricingForm
          courseID={courseID}
          locale={locale}
          onSuccess={fetchHistory}
        />

        <PricingHistoryTable
          history={history}
          isLoading={isLoadingHistory}
          error={historyError}
          locale={locale}
        />

        <div className="flex justify-end pt-2 border-t">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 rounded text-xs font-medium"
          >
            {isAr ? "إغلاق" : "Close"}
          </button>
        </div>
      </div>
    </div>
  );
}
