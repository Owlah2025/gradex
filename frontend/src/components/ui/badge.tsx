import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-pill font-display text-xs font-bold leading-none",
  {
    variants: {
      variant: {
        default: "bg-gx-blue-50 text-gx-blue-600",
        accent: "bg-gx-orange-50 text-gx-orange-700",
        // `success-strong`, not `success`: this is text. See the token comment in the Tailwind
        // config for why success as text and success as an icon are two different greens.
        success: "bg-gx-success-soft text-gx-success-strong",
        neutral: "bg-muted text-muted-foreground",
      },
      size: {
        default: "px-2.5 py-1",
        sm: "px-2 py-0.5 text-[11px]",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, size, ...props }: BadgeProps) {
  return (
    <span className={cn(badgeVariants({ variant, size }), className)} {...props} />
  );
}

export { Badge, badgeVariants };
