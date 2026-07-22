import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";
import { Container } from "./container";
import { Eyebrow, SectionHeading, Lead } from "@/components/ui/typography";

const sectionVariants = cva("", {
  variants: {
    tone: {
      default: "bg-background",
      muted: "bg-gx-blue-50 dark:bg-card",
      dark: "bg-gx-navy text-white",
    },
    spacing: {
      default: "py-16 md:py-20 lg:py-24",
      tight: "py-12 md:py-16 lg:py-[72px]",
    },
  },
  defaultVariants: { tone: "default", spacing: "default" },
});

interface SectionProps
  extends React.HTMLAttributes<HTMLElement>,
    VariantProps<typeof sectionVariants> {
  /** When set, wraps content in a Container automatically. */
  contained?: boolean;
}

/** A full-bleed page section with consistent vertical rhythm and tone. */
export const Section = React.forwardRef<HTMLElement, SectionProps>(
  ({ className, tone, spacing, contained = true, children, ...props }, ref) => (
    <section
      ref={ref}
      className={cn(sectionVariants({ tone, spacing }), className)}
      {...props}
    >
      {contained ? <Container>{children}</Container> : children}
    </section>
  ),
);
Section.displayName = "Section";

/** Reusable section header: eyebrow + heading + optional lead paragraph. */
export function SectionHeader({
  eyebrow,
  title,
  lead,
  align = "start",
  headingId,
  as = "h2",
  className,
}: {
  eyebrow: string;
  title: string;
  lead?: string;
  align?: "start" | "center";
  headingId?: string;
  as?: "h2" | "h3";
  className?: string;
}) {
  return (
    <div
      className={cn(
        "mb-11 max-w-2xl",
        align === "center" && "mx-auto text-center",
        className,
      )}
    >
      <Eyebrow>{eyebrow}</Eyebrow>
      <SectionHeading as={as} id={headingId} className="mt-3">
        {title}
      </SectionHeading>
      {lead ? <Lead className="mt-3.5">{lead}</Lead> : null}
    </div>
  );
}
