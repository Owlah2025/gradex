import * as React from "react";
import { cn } from "@/lib/utils";
import { siteConfig } from "@/config/site";

/**
 * Wordmark — always rendered LTR ("Grade" + orange "x"), per the design system.
 */
export function Wordmark({ className }: { className?: string }) {
  return (
    <span
      dir="ltr"
      className={cn(
        "font-display text-[22px] font-extrabold tracking-tight text-foreground",
        className,
      )}
    >
      {siteConfig.wordmark.lead}
      <span className="text-gx-orange">{siteConfig.wordmark.accent}</span>
    </span>
  );
}
