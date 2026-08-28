"use client";

import { formatFils } from "@/lib/formatters/currency";
import { formatTimestamp } from "@/lib/formatters/datetime";
import { type PriceChangeRecord, type SectionWire } from "@/lib/api/catalog";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@/components/ui/table";
import { pricingScopeLabel } from "./pricing-sections";

export interface PricingHistoryTableProps {
  history: PriceChangeRecord[];
  isLoading: boolean;
  error: string | null;
  locale: "ar" | "en";
  sections: SectionWire[];
}

/**
 * The audit trail of every price an Admin has set on a Course.
 *
 * This was the last table in the product still hand-written. The shared table parts were built
 * naming three tables to absorb — the review queue, the Instructor roster, and this one — and this
 * one was missed. What it cost was not decoration. Its five `<th>` elements carried no `scope`, so
 * nothing associated a figure with the column it belongs to: a reader moving through the row by
 * ear heard five bare numbers and had to remember which was the old price and which was the new.
 * The table also had no accessible name at all, which is what makes a table findable in the first
 * place. Both come from the shared parts now, and the scroll container comes with them, so a wide
 * history scrolls inside its own box instead of pushing the pricing panel sideways.
 *
 * The row is still deliberately monospaced. Prices are read down a column and compared against each
 * other, and proportional digits make that comparison harder than it needs to be; the labels that
 * are words rather than figures opt back into the body face.
 */
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
        <TableContainer>
          <Table className="text-xs">
            <TableCaption>
              {isAr
                ? "سجل تغييرات أسعار هذا المقرر، الأحدث أولاً."
                : "Every recorded price change on this course, most recent first."}
            </TableCaption>
            <TableHead>
              <TableRow>
                <TableHeaderCell scope="col">{isAr ? "النطاق" : "Scope"}</TableHeaderCell>
                <TableHeaderCell scope="col">
                  {isAr ? "السعر السابق" : "Old Price"}
                </TableHeaderCell>
                <TableHeaderCell scope="col">
                  {isAr ? "السعر الجديد" : "New Price"}
                </TableHeaderCell>
                <TableHeaderCell scope="col">{isAr ? "السبب" : "Reason"}</TableHeaderCell>
                <TableHeaderCell scope="col">{isAr ? "التاريخ" : "Timestamp"}</TableHeaderCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {history.map((rec) => (
                <TableRow key={rec.id}>
                  <TableHeaderCell scope="row" className="text-xs">
                    {rec.section_id
                      ? pricingScopeLabel(rec.section_id, sections, locale)
                      : isAr
                        ? "المقرر"
                        : "Course"}
                  </TableHeaderCell>
                  <TableCell className="font-mono text-[11px] text-muted-foreground">
                    {formatFils(rec.old_value_minor_units, locale)}
                  </TableCell>
                  {/* Weight, not colour. This is the new price beside the old one — emphasis, not
                      a success — and green on a bare card is only proved in the light theme. The
                      pricing summary reached the same conclusion for the same reason. */}
                  <TableCell className="font-mono text-[11px] font-semibold">
                    {formatFils(rec.new_value_minor_units, locale)}
                  </TableCell>
                  <TableCell>{rec.reason}</TableCell>
                  <TableCell className="text-muted-foreground">
                    {formatTimestamp(rec.changed_at, locale)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </div>
  );
}
