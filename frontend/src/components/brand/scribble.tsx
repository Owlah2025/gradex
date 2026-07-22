import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * Scribble — hand-drawn orange underline that accents exactly one word in a
 * heading (design-system imagery rule). Decorative only.
 */
export function Scribble({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span className={cn("relative inline-block whitespace-nowrap", className)}>
      {children}
      <svg
        viewBox="0 0 300 20"
        preserveAspectRatio="none"
        aria-hidden
        className="absolute -bottom-[0.28em] -start-[2%] h-[0.42em] w-[104%] overflow-visible"
      >
        <defs>
          <linearGradient id="gx-scribble" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="#ff7e4d" />
            <stop offset="100%" stopColor="#f64a1f" />
          </linearGradient>
        </defs>
        <path
          d="M4 13 C 70 4, 150 4, 210 10 C 250 14, 280 12, 296 7"
          fill="none"
          stroke="url(#gx-scribble)"
          strokeWidth={5}
          strokeLinecap="round"
        />
      </svg>
    </span>
  );
}
