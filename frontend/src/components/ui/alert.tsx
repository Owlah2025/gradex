import * as React from "react";
import { AlertCircle, CheckCircle2, Info } from "lucide-react";
import { cn } from "@/lib/utils";

const icons = {
  error: AlertCircle,
  success: CheckCircle2,
  info: Info,
};

export function Alert({
  tone = "info",
  title,
  children,
}: {
  tone?: keyof typeof icons;
  title: string;
  children?: React.ReactNode;
}) {
  const Icon = icons[tone];
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={cn(
        "flex gap-3 rounded-lg border p-4 text-sm",
        tone === "error" && "border-destructive/25 bg-destructive/5",
        tone === "success" && "border-gx-success/25 bg-gx-success-soft text-gx-navy",
        tone === "info" && "border-gx-blue-200 bg-gx-blue-50 text-gx-navy",
      )}
    >
      <Icon className="mt-0.5 size-5 shrink-0" aria-hidden />
      <div>
        <p className="font-display font-bold">{title}</p>
        {children ? <div className="mt-1 leading-6">{children}</div> : null}
      </div>
    </div>
  );
}
