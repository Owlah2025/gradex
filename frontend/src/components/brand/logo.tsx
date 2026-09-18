import * as React from "react";
import Link from "next/link";
import { cn } from "@/lib/utils";
import { BirdMark } from "./bird-mark";
import { Wordmark } from "./wordmark";
import { siteConfig } from "@/config/site";

const DARK_LOGO_SRC = "/media/gradex-logo-dark.webp";
const LIGHT_LOGO_SRC = "/media/gradex-logo-web.webp";
const LOGO_IMAGE_CLASS =
  "h-auto aspect-[15/7] w-[84px] -translate-x-1 -translate-y-1 object-contain rtl:translate-x-1 sm:w-[88px]";

/** Full lockup: bird + wordmark, links home. */
export function Logo({
  className,
  href = "/",
  ariaLabel = `${siteConfig.name} home`,
  imageSrc,
}: {
  className?: string;
  href?: string;
  ariaLabel?: string;
  imageSrc?: string;
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
      {imageSrc ? (
        <>
          <LogoImage src={imageSrc} className="block dark:hidden" />
          <LogoImage src={DARK_LOGO_SRC} className="hidden dark:block" />
        </>
      ) : (
        <>
          <BirdMark className="size-[30px]" />
          <Wordmark />
        </>
      )}
    </Link>
  );
}

/**
 * The GradeX logo as a Student sees it.
 *
 * Student-facing surfaces all show the same real brand asset, light or dark to
 * suit the ground under it, and all of them mean one thing by it: the way back
 * to the public home page.
 *
 * This exists because `Logo` cannot simply default to the real asset. The same
 * component is the logo in the Admin and Instructor headers, and those surfaces
 * are out of scope for the Student polish work — so the default stays exactly
 * where it was and the Student surfaces opt in here instead. One wrapper rather
 * than an `imageSrc` spelled out at each call site, so the asset pair and the
 * destination are stated once.
 */
export function StudentLogo({
  className,
  ariaLabel,
}: {
  className?: string;
  ariaLabel?: string;
}) {
  return <Logo href="/" ariaLabel={ariaLabel} className={className} imageSrc={LIGHT_LOGO_SRC} />;
}

function LogoImage({
  src,
  className,
}: {
  src: string;
  className: string;
}) {
  // The logo is above the fold on the landing page, so request it with the initial render.
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src={src}
      alt="GradeX"
      loading="eager"
      fetchPriority="high"
      className={cn(LOGO_IMAGE_CLASS, className)}
    />
  );
}
