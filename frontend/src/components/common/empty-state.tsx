import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * EmptyState — shared "nothing here yet" pattern. An empty screen is an invitation to act, so it
 * always leads with a next step (`action`) where one exists.
 *
 * Two densities, because two different readers arrive here. A visitor meeting an empty public
 * surface is being persuaded, and the `default` density has the room for that. An Admin meeting an
 * empty queue is being informed — usually of good news — and a centred fourteen-unit card with a
 * decorative chip spends a screenful of an operational page saying "nothing to do". `compact` says
 * the same thing in a bordered strip, aligned with the rows it stands in for.
 *
 * `description` is where "this is expected" gets said. A queue that is empty because the work is
 * done reads very differently from one that is empty because a filter excluded everything, and the
 * component cannot know which — the caller must.
 *
 * `headingLevel` is the caller's for the same reason. Most of these stand inside a section that
 * already has its own heading, where `h3` is right; a few stand directly under the page title,
 * where `h3` skips a level and leaves a reader moving by headings to guess what the missing `h2`
 * would have been. The component cannot see where it was placed, so the default stays `3` and the
 * screens that sit one level up say so.
 */
export function EmptyState({
  icon,
  title,
  description,
  action,
  density = "default",
  headingLevel = 3,
  className,
  testID,
}: {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  density?: "default" | "compact";
  /** The level this state's title sits at. `2` when it stands directly under the page's `h1`. */
  headingLevel?: 2 | 3 | 4;
  className?: string;
  /** Named for the same reason `LoadingState` and `ErrorState` are: a test asserting which of the
      three a screen is showing has to be able to tell them apart. */
  testID?: string;
}) {
  const compact = density === "compact";
  const Heading = `h${headingLevel}` as const;
  return (
    <div
      data-testid={testID}
      className={cn(
        "rounded-lg border border-dashed border-border bg-card",
        compact
          ? "flex flex-wrap items-center gap-x-4 gap-y-3 px-5 py-6 text-start"
          : "flex flex-col items-center px-6 py-14 text-center",
        className,
      )}
    >
      {icon ? (
        <div
          className={cn(
            "flex items-center justify-center rounded-md bg-gx-blue-50 text-gx-blue-600",
            compact ? "size-9 [&_svg]:size-5" : "mb-4 size-12 [&_svg]:size-6",
          )}
        >
          {icon}
        </div>
      ) : null}
      <div className={compact ? "min-w-0 flex-1" : "contents"}>
        <Heading
          className={cn(
            "font-display font-bold text-foreground",
            compact ? "text-base leading-snug" : "text-xl",
          )}
        >
          {title}
        </Heading>
        {description ? (
          <p
            className={cn(
              "text-muted-foreground",
              compact ? "mt-1 text-sm" : "mt-2 max-w-md",
            )}
          >
            {description}
          </p>
        ) : null}
      </div>
      {action ? <div className={compact ? undefined : "mt-6"}>{action}</div> : null}
    </div>
  );
}
