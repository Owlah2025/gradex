"use client";

import { Check, CircleDashed, Loader2, TriangleAlert } from "lucide-react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Alert } from "@/components/ui/alert";
import { mediaGuidance, pendingReadiness } from "./course-builder-guidance-state";
import type { AuthoringPlan } from "./authoring-plan";

type GuidanceLabels = Dictionary["instructor"]["builderGuidance"];
type SubmissionLabels = Dictionary["instructor"]["submission"];

export function CourseBuilderGuidance({
  plan,
  labels,
  submissionLabels,
}: {
  plan: AuthoringPlan;
  labels: GuidanceLabels;
  submissionLabels: SubmissionLabels;
}) {
  const outstanding = pendingReadiness(plan);
  const media = mediaGuidance(plan);
  const readyForGuidance = plan.ready && media !== "ATTENTION";

  return (
    <section
      aria-labelledby="builder-guidance-title"
      data-testid="course-builder-guidance"
      className="rounded-lg border border-border bg-card p-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-2.5">
          {media === "ATTENTION" ? (
            <TriangleAlert className="mt-0.5 size-5 shrink-0 text-destructive" aria-hidden />
          ) : media === "PROCESSING" ? (
            <Loader2 className="mt-0.5 size-5 shrink-0 animate-spin text-muted-foreground" aria-hidden />
          ) : readyForGuidance ? (
            <Check className="mt-0.5 size-5 shrink-0 text-gx-success" aria-hidden />
          ) : (
            <CircleDashed className="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden />
          )}
          <div className="min-w-0">
            <h3 id="builder-guidance-title" className="font-display text-base font-bold text-foreground">
              {labels.title}
            </h3>
            <p className="mt-1 text-sm leading-6 text-muted-foreground">
              {readyForGuidance ? labels.ready : labels.needsAction}
            </p>
          </div>
        </div>
        <span className="shrink-0 text-xs font-semibold text-muted-foreground">
          {readyForGuidance
            ? labels.readyShort
            : outstanding.length > 0
              ? labels.actionCount.replace("{count}", String(outstanding.length))
              : labels.mediaShort}
        </span>
      </div>

      {outstanding.length > 0 ? (
        <ul className="mt-4 space-y-2 border-t border-border pt-3">
          {outstanding.map((requirement) => (
            <li key={requirement.key} className="flex gap-2 text-sm leading-6 text-foreground">
              <CircleDashed className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
              <span>
                {submissionLabels.requirement[requirement.key]}
                {requirement.offenders.length > 0 ? (
                  <span className="text-muted-foreground">
                    {` — ${submissionLabels.offenders} `}
                    <bdi>{requirement.offenders.slice(0, 3).join("، ")}</bdi>
                    {requirement.offenders.length > 3 ? ` (+${requirement.offenders.length - 3} ${submissionLabels.offenderMore})` : ""}
                  </span>
                ) : null}
              </span>
            </li>
          ))}
        </ul>
      ) : null}

      {media === "PROCESSING" ? (
        <div className="mt-4"><Alert tone="info" title={labels.mediaProcessing} /></div>
      ) : null}
      {media === "ATTENTION" ? (
        <div className="mt-4"><Alert tone="error" title={labels.mediaAttention} /></div>
      ) : null}
    </section>
  );
}
