"use client";

import { formatFils } from "@/lib/formatters/currency";
import { type PriceChangeRecord, type SectionWire } from "@/lib/api/catalog";
import { pricingScopeLabel } from "./pricing-sections";

export interface PricingHistoryTableProps {
  history: PriceChangeRecord[];
  isLoading: boolean;
  error: string | null;
  locale: "ar" | "en";
  sections: SectionWire[];
}

export function PricingHistoryTable({
  history,
  isLoading,
  error,
  locale,
  sections,
}: PricingHistoryTableProps) {
  const isAr = locale === "ar";

  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-foreground">
        {isAr ? "سجل الأسعار التاريخي" : "Price Audit History"}
      </h4>

      {isLoading ? (
        <p className="text-xs text-muted-foreground italic py-4">
          {isAr ? "جاري تحميل سجل الأسعار..." : "Loading price history..."}
        </p>
      ) : error ? (
        <p className="text-xs text-destructive font-medium py-2">{error}</p>
      ) : history.length === 0 ? (
        <p className="text-xs text-muted-foreground italic py-4">
          {isAr ? "لا يوجد سجل تغييرات أسعار بعد." : "No price history recorded yet."}
        </p>
      ) : (
        <div className="overflow-x-auto border rounded-lg">
          <table className="w-full text-xs text-start">
            <thead className="bg-muted text-muted-foreground font-semibold border-b">
              <tr>
                <th className="p-2 border-e">{isAr ? "النطاق" : "Scope"}</th>
                <th className="p-2 border-e">{isAr ? "السعر السابق" : "Old Price"}</th>
                <th className="p-2 border-e">{isAr ? "السعر الجديد" : "New Price"}</th>
                <th className="p-2 border-e">{isAr ? "السبب" : "Reason"}</th>
                <th className="p-2">{isAr ? "التاريخ" : "Timestamp"}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200 dark:divide-slate-800">
              {history.map((rec) => (
                <tr key={rec.id} className="font-mono text-[11px]">
                  <td className="p-2 border-e font-sans">
                    {rec.section_id
                      ? pricingScopeLabel(rec.section_id, sections, locale)
                      : isAr
                        ? "المقرر"
                        : "Course"}
                  </td>
                  <td className="p-2 border-e text-muted-foreground">
                    {formatFils(rec.old_value_minor_units, locale)}
                  </td>
                  {/* Weight, not colour. This is the new price beside the old one — emphasis, not
                      a success — and green on a bare card is only proved in the light theme. The
                      pricing summary reached the same conclusion for the same reason. */}
                  <td className="p-2 border-e font-semibold text-foreground">
                    {formatFils(rec.new_value_minor_units, locale)}
                  </td>
                  <td className="p-2 border-e font-sans">{rec.reason}</td>
                  <td className="p-2 text-muted-foreground font-sans">
                    {new Date(rec.changed_at).toLocaleString(isAr ? "ar-KW" : "en-KW")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
