"use client";

import * as React from "react";
import * as PopoverPrimitive from "@radix-ui/react-popover";
import { cn } from "@/lib/utils";

/**
 * The product's one popover.
 *
 * # WHY A PRIMITIVE RATHER THAN A LOCAL PANEL
 *
 * The first surface to need one is the Course contents, where a Lesson row carries its own
 * downloads. That panel has to trap nothing, close on `Escape`, return focus to the control that
 * opened it, and be reachable from the keyboard beside a link it must never trigger. All of that is
 * Radix's `Popover`, and none of it is worth reimplementing per surface — the same argument the
 * accordion and the sheet already settled here.
 *
 * # DIRECTION
 *
 * `align="start"` means the reading edge in both scripts, and it does so without a locale branch —
 * but not because Radix infers anything. Placement is floating-ui's, and its `isRTL` reads
 * `getComputedStyle(element).direction`. The learning shell sets `dir` on the frame and again on
 * `main`, so the computed direction under the trigger is already correct.
 *
 * This is worth stating precisely because the sibling `Tabs` primitive is the opposite case: its
 * roving focus comes from `@radix-ui/react-direction`, which reads no DOM at all and defaults to
 * LTR, so it must be told the direction explicitly.
 */
const Popover = PopoverPrimitive.Root;
const PopoverTrigger = PopoverPrimitive.Trigger;
const PopoverAnchor = PopoverPrimitive.Anchor;

const PopoverContent = React.forwardRef<
  React.ElementRef<typeof PopoverPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof PopoverPrimitive.Content>
>(({ className, align = "start", sideOffset = 6, ...props }, ref) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Content
      ref={ref}
      align={align}
      sideOffset={sideOffset}
      className={cn(
        "z-50 w-72 rounded-lg border border-border bg-popover p-3 text-popover-foreground shadow-lg outline-none",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
        className,
      )}
      {...props}
    />
  </PopoverPrimitive.Portal>
));
PopoverContent.displayName = PopoverPrimitive.Content.displayName;

export { Popover, PopoverTrigger, PopoverAnchor, PopoverContent };
