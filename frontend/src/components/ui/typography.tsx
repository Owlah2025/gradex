import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * Typography primitives — the type scale from the design system, as components
 * so every screen renders headings identically. Use `as` to keep the semantic
 * heading level correct regardless of visual size.
 */

export function Eyebrow({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn(
        "font-display text-[13px] font-bold uppercase tracking-[0.08em] text-primary",
        className,
      )}
      {...props}
    />
  );
}

type HeadingProps = React.HTMLAttributes<HTMLHeadingElement> & {
  as?: "h1" | "h2" | "h3" | "h4";
};

export function DisplayHeading({
  className,
  as: Tag = "h1",
  ...props
}: HeadingProps) {
  return (
    <Tag
      className={cn(
        "font-display font-extrabold leading-[1.12] text-foreground [text-wrap:balance]",
        "text-[clamp(2.5rem,6vw,4.25rem)]",
        className,
      )}
      {...props}
    />
  );
}

export function SectionHeading({
  className,
  as: Tag = "h2",
  ...props
}: HeadingProps) {
  return (
    <Tag
      className={cn(
        "font-display font-bold leading-tight text-foreground [text-wrap:balance]",
        "text-[clamp(1.75rem,3.4vw,2.5rem)]",
        className,
      )}
      {...props}
    />
  );
}

export function Lead({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn(
        "text-[clamp(1.0625rem,1.6vw,1.25rem)] leading-relaxed text-muted-foreground",
        className,
      )}
      {...props}
    />
  );
}

export function Prose({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn("text-[16.5px] leading-relaxed text-muted-foreground", className)}
      {...props}
    />
  );
}
