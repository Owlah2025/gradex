import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * "This region is still loading", said once, in words.
 *
 * Every workspace screen had its own answer: a centred 12-unit box with a border on the review
 * queue, a bare paragraph on the Courses directory, an `<li>` inside the list on the lifecycle
 * workspace. Two of the three announced nothing to a screen reader, and the bordered box changed
 * the page height when it was replaced by content.
 *
 * The label is a required prop rather than a default. A shared component that ships its own English
 * fallback is how untranslated copy gets into a bilingual product without anyone noticing.
 *
 * Pass `visuallyHidden` when a skeleton already carries the visual signal — the status still needs
 * to be announced, but it must not be drawn twice.
 */
export function LoadingState({
  label,
  visuallyHidden = false,
  className,
  testID,
}: {
  label: string;
  visuallyHidden?: boolean;
  className?: string;
  testID?: string;
}) {
  return (
    <p
      role="status"
      aria-live="polite"
      data-testid={testID}
      className={
        visuallyHidden ? "sr-only" : cn("py-6 text-sm text-muted-foreground", className)
      }
    >
      {label}
    </p>
  );
}

/**
 * A placeholder that holds the space its content will occupy.
 *
 * Reserving the height is the point: the alternative these screens used was rendering nothing until
 * the response arrived, so content appeared by pushing the rest of the page down.
 *
 * Decorative — `aria-hidden`. The announcement belongs to a `LoadingState` beside it.
 */
export function SkeletonBlock({
  className,
  rows = 3,
}: {
  className?: string;
  rows?: number;
}) {
  return (
    <div aria-hidden className={cn("space-y-3", className)}>
      {Array.from({ length: rows }, (_, index) => (
        <div key={index} className="h-16 animate-pulse rounded-lg bg-muted" />
      ))}
    </div>
  );
}
