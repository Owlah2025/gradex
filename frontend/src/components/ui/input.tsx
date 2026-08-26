import * as React from "react";
import { cn } from "@/lib/utils";
import { controlClasses, type ControlSize } from "./control";

export const Input = React.forwardRef<
  HTMLInputElement,
  React.InputHTMLAttributes<HTMLInputElement> & { controlSize?: ControlSize }
>(({ className, controlSize = "default", ...props }, ref) => (
  <input ref={ref} className={cn(controlClasses(controlSize), className)} {...props} />
));
Input.displayName = "Input";
