import * as React from "react";
import { cn } from "@/lib/utils";
import { controlClasses, type ControlSize } from "./control";

/**
 * A native `<select>`, wearing the design system.
 *
 * Fourteen files hand-rolled one, most of them with `p-2 border rounded text-xs bg-white
 * dark:bg-slate-900` — a control that is visibly not the `Input` next to it, has no focus ring, and
 * hardcodes both of its background colours instead of taking the surface token.
 *
 * It stays a native select. The one thing a custom listbox would buy is styling the options, and
 * the things it would cost — keyboard behaviour, mobile pickers, form semantics — are the things
 * operational screens actually depend on.
 */
export const Select = React.forwardRef<
  HTMLSelectElement,
  React.SelectHTMLAttributes<HTMLSelectElement> & { controlSize?: ControlSize }
>(({ className, controlSize = "default", children, ...props }, ref) => (
  <select ref={ref} className={cn(controlClasses(controlSize), "pe-8", className)} {...props}>
    {children}
  </select>
));
Select.displayName = "Select";
