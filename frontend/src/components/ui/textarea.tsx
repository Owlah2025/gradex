import * as React from "react";
import { cn } from "@/lib/utils";
import { controlClasses } from "./control";

export const Textarea = React.forwardRef<
  HTMLTextAreaElement,
  React.TextareaHTMLAttributes<HTMLTextAreaElement>
>(({ className, rows = 3, ...props }, ref) => (
  <textarea
    ref={ref}
    rows={rows}
    className={cn(controlClasses("auto"), "min-h-24 py-3 leading-6", className)}
    {...props}
  />
));
Textarea.displayName = "Textarea";
