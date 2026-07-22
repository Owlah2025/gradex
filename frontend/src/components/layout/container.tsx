import * as React from "react";
import { cn } from "@/lib/utils";

/** Centered max-1200px content column with responsive inline padding. */
export const Container = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={cn(
      "mx-auto w-full max-w-container px-5 sm:px-6",
      className,
    )}
    {...props}
  />
));
Container.displayName = "Container";
