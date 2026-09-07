"use client";

import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * One question, and the answers to it.
 *
 * The group is a real `role="group"` labelled by the question, so a screen reader announces
 * "Where do you study?" before the first option instead of reading a run of unrelated buttons. The
 * question is a `<p>` and not a heading on purpose: the section already owns the heading level, and
 * inserting an `h3` per step would make the outline of the page change as the reader answers.
 *
 * `appear` is the reveal. The next question arrives with a 12px rise over 260ms — inside the band
 * where a transition reads as the interface responding rather than performing — and is skipped
 * entirely under `prefers-reduced-motion`, where the answer simply appears.
 */
export function ContextQuestion({
  id,
  question,
  hint,
  appear = false,
  children,
  className,
}: {
  id: string;
  question: string;
  hint?: string;
  appear?: boolean;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        appear &&
          "motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-3 motion-safe:duration-slow motion-safe:ease-out-brand",
        className,
      )}
    >
      <p
        id={id}
        className="font-display text-[clamp(1.15rem,2.2vw,1.4rem)] font-bold leading-snug text-foreground"
      >
        {question}
      </p>
      {hint ? <p className="mt-1.5 text-sm text-muted-foreground">{hint}</p> : null}
      <div role="group" aria-labelledby={id} className="mt-4">
        {children}
      </div>
    </div>
  );
}

/**
 * The answers, as things to press rather than a list to open.
 *
 * `auto-fit` with a floor rather than a fixed column count: the launch catalogue has one university
 * and five programs, and a two-column grid asked at that volume produces a lonely half-row. The
 * floor is 13rem so a program name wraps at most once, and every target clears the 44px minimum on
 * a phone without a separate mobile layout.
 */
export function ChoiceGrid({
  compact = false,
  single = false,
  children,
}: {
  /** Tighter columns for the hero card, where the whole component has to stay short. */
  compact?: boolean;
  /**
   * One option per row.
   *
   * For lists whose labels are long enough that any column split elides them — university names,
   * which run to "الجامعة الأمريكية في الكويت" and came out as "الجامعة الأمريكية في الك…" in two
   * columns. An option list may never hide the option the reader is looking for.
   */
  single?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "grid gap-2",
        single
          ? "grid-cols-1"
          : // 12rem, not 10: at 10 a three-column split turned "Computer Engineering" into
            // "Computer Engin…".
            compact
            ? "grid-cols-[repeat(auto-fit,minmax(12rem,1fr))]"
            : "grid-cols-[repeat(auto-fit,minmax(13rem,1fr))] gap-2.5",
      )}
    >
      {children}
    </div>
  );
}

/**
 * A single answer.
 *
 * A real `<button>` with `aria-pressed`, which is the honest shape: pressing it changes the state
 * of the page rather than navigating, and the pressed state is what a screen reader needs in order
 * to say the choice was taken. Radio semantics were the alternative and were rejected — they oblige
 * this to reimplement arrow-key roving focus, and every option here is reachable by Tab already.
 *
 * The selected state carries the brand blue on its own soft surface rather than a tint of the page
 * background, because a border-only "selected" is invisible at a glance on a wall of chips, and
 * colour alone would not survive a reader who cannot see it — hence the check as well.
 */
export function ChoiceChip({
  selected = false,
  detail,
  value,
  tone = "surface",
  size = "default",
  onSelect,
  children,
}: {
  selected?: boolean;
  /**
   * Which ground this chip is painted on.
   *
   * `onDark` is the navy hero band, and it borrows its language from the `onDark` button variant
   * that already exists for exactly that band — white at low opacity for the surface and the
   * border, not a second palette.
   */
  tone?: "surface" | "onDark";
  /** `compact` is the hero prompt, where the chip must not compete with the headline beside it. */
  size?: "default" | "compact";
  /** A second line — the college a program belongs to. Never an identifier. */
  detail?: string;
  /**
   * The public slug this option carries, exposed for assertions.
   *
   * The reader never sees it — the label is the name — but the guarantee that these options are
   * public slugs and never raw identifiers is one worth being able to check, and a `<select>` used
   * to make it checkable for free through its option values.
   */
  value?: string;
  onSelect: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      data-value={value}
      onClick={onSelect}
      className={cn(
        "group flex items-center gap-3 rounded-md border text-start font-display font-semibold leading-snug",
        "transition-[background-color,border-color,box-shadow,transform] duration-base ease-out-brand",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        "active:scale-[0.99]",
        size === "compact"
          ? "min-h-[2.75rem] px-3.5 py-2 text-[14px]"
          : "min-h-[3.75rem] px-4 py-3.5 text-[15px]",
        tone === "onDark"
          ? cn(
              "focus-visible:ring-offset-gx-navy",
              selected
                ? "border-gx-orange bg-white/15 text-white"
                : "border-white/20 bg-white/[0.07] text-white/90 hover:border-white/40 hover:bg-white/15 hover:text-white",
            )
          : cn(
              "focus-visible:ring-offset-background",
              selected
                ? "border-primary bg-gx-blue-50 text-gx-blue-600 shadow-sm dark:bg-primary/15 dark:text-foreground"
                : "border-border bg-card text-foreground hover:border-gx-blue-300 hover:bg-accent",
            ),
      )}
    >
      <span className="min-w-0 flex-1">
        <span className="block truncate">{children}</span>
        {detail ? (
          <span
            className={cn(
              "mt-0.5 block truncate text-[13px] font-normal",
              tone === "onDark" ? "text-white/60" : "text-muted-foreground",
            )}
          >
            {detail}
          </span>
        ) : null}
      </span>
    </button>
  );
}
