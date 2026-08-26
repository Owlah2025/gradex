"use client";

import * as React from "react";
import * as AccordionPrimitive from "@radix-ui/react-accordion";
import { ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";

const Accordion = AccordionPrimitive.Root;

const AccordionItem = React.forwardRef<
  React.ElementRef<typeof AccordionPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof AccordionPrimitive.Item>
>(({ className, ...props }, ref) => (
  <AccordionPrimitive.Item
    ref={ref}
    className={cn(
      "overflow-hidden rounded-lg border border-border bg-card shadow-sm",
      className,
    )}
    {...props}
  />
));
AccordionItem.displayName = "AccordionItem";

/**
 * `headingLevel` exists because a disclosure is a heading, and which heading depends on where the
 * accordion stands. Radix wraps every trigger in an `h3`, which is right for a FAQ sitting directly
 * under an `h2`, and wrong for a Course curriculum whose sections are the page's own second level —
 * and wrong again for the same curriculum rendered beside a Lesson, where they are its third.
 * Leaving it unset keeps Radix's `h3` exactly as every existing caller already gets it.
 */
const AccordionTrigger = React.forwardRef<
  React.ElementRef<typeof AccordionPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof AccordionPrimitive.Trigger> & {
    headingLevel?: "h2" | "h3" | "h4";
  }
>(({ className, children, headingLevel, ...props }, ref) => {
  const trigger = (
    <AccordionPrimitive.Trigger
      ref={ref}
      className={cn(
        "flex flex-1 items-center justify-between gap-4 px-5 py-5 text-start font-display text-[17px] font-bold text-foreground transition-colors hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset [&[data-state=open]>svg]:rotate-180",
        className,
      )}
      {...props}
    >
      {children}
      <ChevronDown
        className="size-5 shrink-0 text-primary transition-transform duration-base ease-out-brand"
        aria-hidden
      />
    </AccordionPrimitive.Trigger>
  );
  if (!headingLevel) {
    return <AccordionPrimitive.Header className="flex">{trigger}</AccordionPrimitive.Header>;
  }
  return (
    <AccordionPrimitive.Header asChild>
      {React.createElement(headingLevel, { className: "flex" }, trigger)}
    </AccordionPrimitive.Header>
  );
});
AccordionTrigger.displayName = "AccordionTrigger";

const AccordionContent = React.forwardRef<
  React.ElementRef<typeof AccordionPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof AccordionPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <AccordionPrimitive.Content
    ref={ref}
    className="overflow-hidden text-[15.5px] leading-relaxed text-muted-foreground data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down"
    {...props}
  >
    <div className={cn("px-5 pb-5 pt-0", className)}>{children}</div>
  </AccordionPrimitive.Content>
));
AccordionContent.displayName = "AccordionContent";

export { Accordion, AccordionItem, AccordionTrigger, AccordionContent };
