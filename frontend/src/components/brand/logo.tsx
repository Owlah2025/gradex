import * as React from "react";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { BirdMark } from "./bird-mark";
import { Wordmark } from "./wordmark";
import { siteConfig } from "@/config/site";

/** Full lockup: bird + wordmark, links home. */
export function Logo({
  className,
  href = "/",
  ariaLabel = `${siteConfig.name} home`,
}: {
  className?: string;
  href?: string;
  ariaLabel?: string;
}) {
  return (
    <Link
      href={href}
      aria-label={ariaLabel}
      className={cn(
        "inline-flex items-center gap-2.5 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        className,
      )}
    >
      <BirdMark className="size-[30px]" />
      <Wordmark />
    </Link>
  );
}
