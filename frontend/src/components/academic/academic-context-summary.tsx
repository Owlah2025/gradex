"use client";

import * as React from "react";
import { GraduationCap } from "lucide-react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";

/**
 * "Courses for Kuwait University · Computer Science", plus the ways out.
 *
 * Deliberately presentational: it takes two already-localized names and knows nothing about slugs,
 * identifiers, or where the context came from. That is what lets one component serve both an
 * anonymous browsing preference and a signed-in Student's saved profile without either being able
 * to borrow the other's identity.
 *
 * The provenance line under the names is not decoration. An anonymous context lives in one browser
 * and is not on the visitor's account, and saying so is the difference between a true statement and
 * an implied promise that Gradex has saved something it has not.
 *
 * `onClear` is offered wherever dropping the context is meaningful. A personalisation the reader
 * cannot undo is a trap, not a preference.
 */
export function AcademicContextSummary({
  institution,
  program,
  provenance,
  onChange,
  changeHref,
  onClear,
  changeLabel,
  className,
  testID,
}: {
  /** Already localized. Never a slug the reader is asked to recognise. */
  institution: string;
  program: string;
  /** Where this context comes from, in the reader's own language. */
  provenance: string;
  onChange?: () => void;
  /** Used instead of `onChange` where changing means going somewhere — the profile editor. */
  changeHref?: string;
  onClear?: () => void;
  changeLabel?: string;
  className?: string;
  testID?: string;
}) {
  const { t } = useLocale();
  const copy = t.academicContext;

  return (
    <div
      data-testid={testID}
      className={[
        "flex flex-wrap items-center gap-x-5 gap-y-3 rounded-lg border border-border bg-card px-5 py-4",
        className ?? "",
      ]
        .join(" ")
        .trim()}
    >
      <div className="flex min-w-0 flex-1 items-start gap-3">
        <span
          aria-hidden
          className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-md bg-gx-blue-50 text-gx-blue-600 [&_svg]:size-5"
        >
          <GraduationCap />
        </span>
        <div className="min-w-0">
          <p className="text-sm text-muted-foreground">{copy.showingFor}</p>
          <p
            className="font-display text-base font-bold leading-snug text-foreground"
            data-testid="academic-context-names"
          >
            {institution}
            {program === "" ? "" : ` · ${program}`}
          </p>
          <p
            className="mt-0.5 text-xs text-muted-foreground"
            data-testid="academic-context-provenance"
          >
            {provenance}
          </p>
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {changeHref ? (
          <Button asChild variant="outline" size="sm">
            <a href={changeHref} data-testid="academic-context-change">
              {changeLabel ?? copy.change}
            </a>
          </Button>
        ) : onChange ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onChange}
            aria-label={copy.changeAria}
            data-testid="academic-context-change"
          >
            {changeLabel ?? copy.change}
          </Button>
        ) : null}
        {onClear ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onClear}
            data-testid="academic-context-clear"
          >
            {copy.showAll}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
