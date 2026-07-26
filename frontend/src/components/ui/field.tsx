import * as React from "react";
import { cn } from "@/lib/utils";

export function Field({
  label,
  htmlFor,
  hint,
  error,
  children,
  className,
}: {
  label: string;
  htmlFor: string;
  hint?: string;
  error?: string;
  children: React.ReactNode;
  className?: string;
}) {
  const hintID = `${htmlFor}-hint`;
  const errorID = `${htmlFor}-error`;
  return (
    <div className={cn("space-y-2", className)}>
      <label htmlFor={htmlFor} className="block font-display text-sm font-bold">
        {label}
      </label>
      {children}
      {error ? (
        <p id={errorID} className="text-sm font-medium text-destructive">
          {error}
        </p>
      ) : hint ? (
        <p id={hintID} className="text-sm text-muted-foreground">
          {hint}
        </p>
      ) : null}
    </div>
  );
}
