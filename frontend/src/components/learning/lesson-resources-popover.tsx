"use client";

import * as React from "react";
import { ChevronDown, Paperclip } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/**
 * One Lesson's downloadable items, behind one control on that Lesson's own row.
 *
 * # WHY A POPOVER AND NOT A LIST
 *
 * Beside a Lesson the contents are a 20rem column. Rendering every file inline there turned a
 * three-Lesson section into a wall in which the Lesson titles — the thing the column exists to
 * offer — were the minority of the text. The files still belong to the Lesson and are still reached
 * from its row; they are simply one keystroke away instead of always open. The Course page, which
 * has the width for it, keeps its inline list.
 *
 * # WHY THE TRIGGER IS A SIBLING OF THE LESSON LINK
 *
 * A button inside an anchor is invalid markup and, worse, activating it would follow the link. The
 * row is a flex container holding two independent controls: the link to the Lesson, and this. So
 * opening the files never navigates, and neither control can swallow the other's keyboard focus.
 *
 * # WHAT IT DOES NOT KNOW
 *
 * The panel's contents are composed on the server and handed in as `children`. This component never
 * sees a file name, a storage key, an Asset Version or a download path — it decides only whether
 * the panel is open. The authorization for each download is minted, per click, by the server route
 * the composed subtree already carries.
 */
export function LessonResourcesPopover({
  label,
  accessibleLabel,
  heading,
  count,
  children,
  className,
}: {
  /** The visible word on the row, e.g. "Resources". */
  label: string;
  /** The control's accessible name, which must also name the Lesson it belongs to. */
  accessibleLabel: string;
  /** The panel's own heading, so the open panel says what it is. */
  heading: string;
  /** How many items the panel holds, shown so the row says the files exist before it is opened. */
  count: string;
  children: React.ReactNode;
  className?: string;
}) {
  const [open, setOpen] = React.useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        aria-label={accessibleLabel}
        data-testid="lesson-resources-trigger"
        className={cn(
          "group inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs font-semibold text-muted-foreground transition-colors",
          "hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          "data-[state=open]:bg-accent data-[state=open]:text-foreground",
          className,
        )}
      >
        <Paperclip aria-hidden className="size-4 shrink-0" />
        {/* The word is worth its width only where there is width to spare. In the contents column
            it competes directly with the Lesson title — the one thing the column exists to show —
            so below `xl` the control is the clip, the count and the chevron, and the word stays in
            the accessible name where it is never truncated. */}
        <span className="hidden whitespace-nowrap xl:inline">{label}</span>
        <span className="tabular-nums">{count}</span>
        <ChevronDown
          aria-hidden
          className="size-3.5 shrink-0 transition-transform duration-base ease-out-brand group-data-[state=open]:rotate-180"
        />
      </PopoverTrigger>
      {/* `Escape`, outside-click dismissal and the return of focus to the trigger are the
          primitive's. Collision handling flips the panel rather than letting it leave the column,
          which at 320px is the difference between a usable panel and one half off-screen. */}
      <PopoverContent
        data-testid="lesson-resources-panel"
        collisionPadding={12}
        className="w-[min(20rem,calc(100vw-2rem))]"
      >
        <p className="font-display text-xs font-bold uppercase tracking-wide text-muted-foreground">
          {heading}
        </p>
        <div className="mt-2">{children}</div>
      </PopoverContent>
    </Popover>
  );
}
