import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * The operational table used by Admin and Instructor screens.
 *
 * These are styled table parts, not a data grid. The product has three hand-written tables — the
 * review queue, the Instructor roster, the pricing history — and they disagreed about everything a
 * reader notices: one used `border-e` between every cell including the last, one used no vertical
 * rules at all; header rows were slate-100 in one place and a bare bottom border in another; cell
 * padding ran from `p-3` to `px-3 py-2`. What none of them disagreed about was their *columns*,
 * which are different in each case and known at the call site. A configurable `DataGrid` would have
 * had to re-express that knowledge as configuration to gain nothing.
 *
 * Overflow is owned here rather than left to each caller. A table that exceeds its container scrolls
 * inside `TableContainer`; the page itself never scrolls sideways. This is also why these tables are
 * not replaced with stacked cards below `sm`: a roster is read by comparing rows down a column, and
 * a card wall destroys exactly that.
 */
export function TableContainer({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("overflow-x-auto rounded-lg border border-border bg-card", className)}>
      {children}
    </div>
  );
}

export function Table({
  children,
  className,
  ...props
}: React.TableHTMLAttributes<HTMLTableElement>) {
  return (
    <table className={cn("w-full border-collapse text-start text-sm", className)} {...props}>
      {children}
    </table>
  );
}

/**
 * The table's accessible name. Visually hidden by default: the surrounding section already carries
 * a visible heading, and repeating it above the table is noise for sighted readers while its
 * absence leaves the table unnamed for everyone else.
 */
export function TableCaption({
  children,
  visible = false,
  className,
}: {
  children: React.ReactNode;
  visible?: boolean;
  className?: string;
}) {
  return (
    <caption
      className={visible ? cn("pb-3 text-start text-sm text-muted-foreground", className) : "sr-only"}
    >
      {children}
    </caption>
  );
}

export function TableHead({
  children,
  className,
  ...props
}: React.HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <thead
      className={cn(
        "border-b border-border bg-muted/50 text-xs font-semibold uppercase tracking-wide text-muted-foreground",
        className,
      )}
      {...props}
    >
      {children}
    </thead>
  );
}

export function TableBody({
  children,
  className,
  ...props
}: React.HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <tbody className={cn("divide-y divide-border", className)} {...props}>
      {children}
    </tbody>
  );
}

export function TableRow({
  children,
  className,
  interactive = false,
  ...props
}: React.HTMLAttributes<HTMLTableRowElement> & { interactive?: boolean }) {
  return (
    <tr
      className={cn(interactive && "transition-colors hover:bg-accent/50", className)}
      {...props}
    >
      {children}
    </tr>
  );
}

/**
 * A header cell. `scope` is required rather than optional — the review queue's `<th>` elements
 * carried none, which leaves the association between a cell and its column up to the screen
 * reader's guess.
 */
export function TableHeaderCell({
  children,
  scope,
  className,
  ...props
}: React.ThHTMLAttributes<HTMLTableCellElement> & { scope: "col" | "row" }) {
  return (
    <th
      scope={scope}
      className={cn(
        "px-4 py-3 text-start align-top",
        scope === "row" && "font-medium normal-case tracking-normal text-foreground",
        className,
      )}
      {...props}
    >
      {children}
    </th>
  );
}

export function TableCell({
  children,
  className,
  ...props
}: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td className={cn("px-4 py-3 text-start align-top text-foreground", className)} {...props}>
      {children}
    </td>
  );
}

/**
 * Placeholder rows that hold the table's shape while the first page is in flight.
 *
 * Decorative: the rows are `aria-hidden` and the announcement belongs to a `LoadingState` beside
 * the table. Rendering the real column count is the point — the header stays measurable, so the
 * columns do not resize when the data lands.
 */
export function TableSkeletonRows({ columns, rows = 4 }: { columns: number; rows?: number }) {
  return (
    <tbody aria-hidden className="divide-y divide-border">
      {Array.from({ length: rows }, (_, rowIndex) => (
        <tr key={rowIndex}>
          {Array.from({ length: columns }, (_, cellIndex) => (
            <td key={cellIndex} className="px-4 py-3">
              <div className="h-4 animate-pulse rounded bg-muted" />
            </td>
          ))}
        </tr>
      ))}
    </tbody>
  );
}
