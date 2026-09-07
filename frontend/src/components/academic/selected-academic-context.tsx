"use client";

import * as React from "react";
import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * An answered question, kept on screen.
 *
 * The step does not disappear when it is answered — it collapses into the answer, with the check
 * that says so and, where there is a choice left to revisit, the one control that reopens it. That
 * is the difference between a flow the reader can see the shape of and a form that swallows what
 * they just told it.
 *
 * `onChange` is omitted rather than disabled where changing is meaningless — a single-university
 * catalogue has nothing to reopen — because a control that is present and does nothing is worse
 * than one that was never offered.
 */
export function SelectedAnswer({
  label,
  value,
  onChange,
  changeLabel,
  changeAria,
  testID,
}: {
  /** What was asked, in two or three words. */
  label: string;
  /** Already localized. Never a slug. */
  value: string;
  onChange?: () => void;
  changeLabel?: string;
  changeAria?: string;
  testID?: string;
}) {
  return (
    <div
      data-testid={testID}
      className="flex items-center gap-3 rounded-md border border-border bg-card/60 px-3.5 py-2.5"
    >
      <span
        aria-hidden
        className="flex size-6 shrink-0 items-center justify-center rounded-pill bg-gx-success-soft text-gx-success-strong [&_svg]:size-3.5"
      >
        <Check strokeWidth={3} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-xs text-muted-foreground">{label}</span>
        <span className="block truncate font-display text-[15px] font-bold leading-snug text-foreground">
          {value}
        </span>
      </span>
      {onChange && changeLabel ? (
        <button
          type="button"
          onClick={onChange}
          aria-label={changeAria}
          data-testid="academic-context-change"
          className={cn(
            "shrink-0 rounded-sm px-2 py-1 font-display text-[13px] font-bold text-primary underline-offset-4",
            "transition-colors duration-base hover:underline",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
          )}
        >
          {changeLabel}
        </button>
      ) : null}
    </div>
  );
}

/**
 * The resolved context, compact, for the surface showing its results.
 *
 * Two chips and one control. It is not the same component as the panel's own summary card, and
 * deliberately so: this one sits above a list of courses that already explains itself, so repeating
 * "Showing courses for" above it would be the third time the page says the same thing.
 */
export function AcademicContextChips({
  institution,
  program,
  onChange,
  changeLabel,
  changeAria,
  className,
  testID,
}: {
  institution: string;
  program: string;
  onChange: () => void;
  changeLabel: string;
  changeAria: string;
  className?: string;
  testID?: string;
}) {
  return (
    <div
      data-testid={testID}
      className={cn("flex flex-wrap items-center gap-x-2.5 gap-y-2", className)}
    >
      <span
        data-testid="academic-context-names"
        className="flex flex-wrap items-center gap-x-2.5 gap-y-2"
      >
        <Chip>{institution}</Chip>
        {program === "" ? null : <Chip>{program}</Chip>}
      </span>
      <button
        type="button"
        onClick={onChange}
        aria-label={changeAria}
        data-testid="academic-context-change"
        className={cn(
          "rounded-sm px-2 py-1 font-display text-[13px] font-bold text-primary underline-offset-4",
          "transition-colors duration-base hover:underline",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        )}
      >
        {changeLabel}
      </button>
    </div>
  );
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-pill border border-gx-blue-100 bg-gx-blue-50 px-3 py-1 font-display text-[13px] font-semibold text-gx-blue-600 dark:border-primary/25 dark:bg-primary/15 dark:text-foreground">
      {children}
    </span>
  );
}
