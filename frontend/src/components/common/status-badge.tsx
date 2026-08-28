import * as React from "react";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

/**
 * A domain state, presented so that its colour is never the only thing carrying it.
 *
 * The Courses directory established this pairing in the previous tranche: the state word in a
 * toned `Badge`, and immediately beside it, in words, who the product is waiting on. Elsewhere the
 * product still used colour alone — an amber pill for a pending count, a blue pill against a purple
 * one to distinguish a first publication from an update — which says nothing to a reader who cannot
 * separate the two hues, and nothing at all to a screen reader.
 *
 * This component owns the *presentation* of that pairing and nothing else. It deliberately holds no
 * mapping from any domain enum to a tone: `course-status.ts` decides what an Admin Course state
 * means, and the Instructor roster decides what an access status means, because those are different
 * questions with different answers. One global status component for every domain would have to be
 * told the answer anyway, and would then be the place where the two domains started leaking into
 * each other.
 */
export function StatusBadge({
  tone = "neutral",
  label,
  detail,
  size,
  labelTestID,
  detailTestID,
  className,
}: {
  tone?: BadgeProps["variant"];
  /** The state itself, already translated. */
  label: string;
  /** What the state means for the reader — the half that survives without colour. */
  detail?: string;
  size?: BadgeProps["size"];
  labelTestID?: string;
  detailTestID?: string;
  className?: string;
}) {
  return (
    <span className={cn("inline-flex flex-wrap items-center gap-2", className)}>
      {/* A status pill is a short, fixed vocabulary and must stay on one line: inside a table the
          container scrolls, and a pill that breaks mid-word ("Updat / a publis / cours") reads as
          damage. `Badge` itself stays wrappable — the public catalogue puts whole taxonomy labels
          in one, and those must be allowed to break. */}
      <Badge variant={tone} size={size} className="whitespace-nowrap" data-testid={labelTestID}>
        {label}
      </Badge>
      {detail ? (
        <span data-testid={detailTestID} className="text-xs font-semibold text-muted-foreground">
          {detail}
        </span>
      ) : null}
    </span>
  );
}
