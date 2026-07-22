import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * Tag — neutral pill for metadata (level, "labs included"), optionally with a
 * leading icon. Distinct from Badge (which is a bolder status marker).
 */
const Tag = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, children, ...props }, ref) => (
  <span
    ref={ref}
    className={cn(
      "inline-flex items-center gap-1.5 rounded-pill bg-muted px-2.5 py-1 text-[12.5px] font-medium text-muted-foreground [&_svg]:size-3.5",
      className,
    )}
    {...props}
  >
    {children}
  </span>
));
Tag.displayName = "Tag";

export { Tag };
