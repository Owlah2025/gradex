import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * BirdMark — the Gradex origami bird (a student taking flight). Decorative by
 * default; pass an accessible `title` only when it stands alone as the logo.
 */
export function BirdMark({
  className,
  title,
}: {
  className?: string;
  title?: string;
}) {
  return (
    <svg
      viewBox="0 0 100 100"
      fill="none"
      role={title ? "img" : "presentation"}
      aria-hidden={title ? undefined : true}
      aria-label={title}
      className={cn("size-8", className)}
    >
      {title ? <title>{title}</title> : null}
      <path d="M50 22 L72 58 L50 51 L28 58 Z" fill="#4f7cff" />
      <path d="M50 51 L72 58 L57 76 Z" fill="#1e4ed8" />
      <path d="M50 51 L28 58 L43 76 Z" fill="#0d1b2a" />
      <path d="M72 58 L84 55 L77 64 Z" fill="#ff7e4d" />
    </svg>
  );
}
