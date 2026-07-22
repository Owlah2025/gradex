import * as React from "react";
import { cn } from "@/lib/utils";

/**
 * EmptyState — shared "nothing here yet" pattern. An empty screen is an
 * invitation to act, so it always leads with a next step (`action`).
 */
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: React.ReactNode;
  title: string;
  description?: string;
  action?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center rounded-lg border border-dashed border-border bg-card px-6 py-14 text-center",
        className,
      )}
    >
      {icon ? (
        <div className="mb-4 flex size-12 items-center justify-center rounded-md bg-gx-blue-50 text-gx-blue-600 [&_svg]:size-6">
          {icon}
        </div>
      ) : null}
      <h3 className="font-display text-xl font-bold text-foreground">{title}</h3>
      {description ? (
        <p className="mt-2 max-w-md text-muted-foreground">{description}</p>
      ) : null}
      {action ? <div className="mt-6">{action}</div> : null}
    </div>
  );
}
