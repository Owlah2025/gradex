"use client";

import * as React from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import { cn } from "@/lib/utils";

/**
 * The product's one tab set.
 *
 * Radix owns the roving focus, the `aria-selected`/`aria-controls` wiring and the arrow-key
 * behaviour, which are the parts a hand-built tab strip gets wrong.
 *
 * # DIRECTION IS NOT INFERRED — IT MUST BE PASSED
 *
 * `Tabs` resolves direction through `@radix-ui/react-direction`, whose `useDirection` is
 * `localDir || context || "ltr"`. It never reads `document.dir` or a computed style. So a caller
 * that renders Arabic and passes nothing gets **LTR roving focus**: the arrow keys move by DOM
 * order while the tabs are painted right to left, and every arrow press goes the wrong way.
 *
 * Every caller therefore passes `dir` explicitly. It is deliberately not solved with a
 * `DirectionProvider` above the tree, because that would change Radix behaviour for every other
 * surface in the product at the same time — a far larger change than the one bug it fixes.
 *
 * Popover is not the same case and needs no such prop: its alignment is computed by floating-ui,
 * whose `isRTL` reads `getComputedStyle(element).direction`, which the learning shell already
 * sets on the frame and on `main`.
 *
 * The selected tab is marked by weight and an underline on the block edge as well as by tone, so
 * the state survives a monochrome rendering — the same rule the curriculum's current row follows.
 */
const Tabs = TabsPrimitive.Root;

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn(
      // Scrollable rather than wrapping: three tabs fit at 390px, but a longer set must not push
      // the player's width around, and a horizontal scroll inside the strip is contained.
      "flex items-stretch gap-1 overflow-x-auto border-b border-border",
      className,
    )}
    {...props}
  />
));
TabsList.displayName = TabsPrimitive.List.displayName;

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      "relative -mb-px inline-flex min-h-11 shrink-0 items-center whitespace-nowrap border-b-2 border-transparent px-4 font-display text-[15px] font-semibold text-muted-foreground transition-colors",
      "hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
      "data-[state=active]:border-primary data-[state=active]:font-bold data-[state=active]:text-foreground",
      "disabled:pointer-events-none disabled:opacity-60",
      className,
    )}
    {...props}
  />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn(
      "pt-6 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
      className,
    )}
    {...props}
  />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;

export { Tabs, TabsList, TabsTrigger, TabsContent };
