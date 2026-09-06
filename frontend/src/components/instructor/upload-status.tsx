"use client";

import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import type { ProcessingStage } from "@/lib/api/media-upload";
import { cn } from "@/lib/utils";

type MediaLabels = Dictionary["instructor"]["media"];

/** The phases the media client genuinely passes through. Nothing here is invented. */
export type UploadPhase =
  | "IDLE"
  | "PREPARING"
  | "UPLOADING"
  | "PROCESSING"
  | "PROCESSING_BACKGROUND"
  | "CHECKING"
  | "ATTACHING"
  | "READY"
  | "FAILED";

export function isUploadBusy(phase: UploadPhase): boolean {
  return (
    phase === "PREPARING" ||
    phase === "UPLOADING" ||
    phase === "PROCESSING" ||
    phase === "CHECKING" ||
    phase === "ATTACHING"
  );
}

/**
 * Where an upload has got to, and — when it ends badly — that it ended badly.
 *
 * The lesson video control reported failure in a `role="status"` paragraph coloured
 * `text-slate-700`: the same tag, the same weight, and the same ink as "Video attached to this
 * Lesson". An upload that failed after four minutes of a real file looked exactly like one that
 * succeeded, and was announced to a screen reader as a polite status rather than an alert. The
 * resource control had already got this right; this is the pair of them agreeing.
 *
 * The phase itself was set in `font-mono` at 10px, which is a debugging readout, not a status.
 *
 * Two different measures reach this component and are never mixed. `progress` is the browser's own
 * byte count for the PUT to storage, and is meaningful during UPLOADING alone. `processing` is the
 * server's measured account of the transcode — FFmpeg's structured progress against the probed
 * duration — and is meaningful only while the worker is actually running. Whichever one is
 * genuinely being measured drives a determinate bar with a real `aria-valuenow`; when neither is,
 * the bar is indeterminate and carries no value at all, because a number nobody measured is worse
 * than no number.
 */
export function UploadStatus({
  phase,
  progress,
  processing,
  message,
  labels,
  phaseTestID,
  messageTestID,
  onRetry,
}: {
  phase: UploadPhase;
  /** 0–1, meaningful during UPLOADING only. */
  progress: number;
  /**
   * The server's own processing observation, when it has one. Null means the
   * worker has not measured anything yet — which is shown as an indeterminate
   * stage, never as 0%.
   */
  processing?: { stage: ProcessingStage; percent: number } | null;
  message: string | null;
  labels: MediaLabels;
  phaseTestID?: string;
  messageTestID?: string;
  onRetry?: () => void;
}) {
  const failed = phase === "FAILED";
  const busy = isUploadBusy(phase);
  const uploading = phase === "UPLOADING";
  const processingPhase = phase === "PROCESSING" || phase === "PROCESSING_BACKGROUND";
  const observation = processingPhase ? (processing ?? null) : null;

  const percent = uploading
    ? Math.round(progress * 100)
    : observation
      ? observation.percent
      : null;
  const determinate = percent !== null;

  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <span
          data-testid={phaseTestID}
          data-upload-phase={phase}
          data-processing-stage={observation?.stage ?? undefined}
          data-processing-percent={observation ? String(observation.percent) : undefined}
          className={cn(
            "rounded-pill px-2 py-0.5 text-xs font-semibold",
            failed && "bg-destructive/10 text-destructive",
            // Text, so the AA-safe success green rather than the icon one.
            phase === "READY" && "bg-gx-success-soft text-gx-success-strong",
            !failed && phase !== "READY" && "bg-muted text-muted-foreground",
          )}
        >
          {labels.phase[phase]}
          {determinate ? ` ${percent}%` : ""}
        </span>
        {busy ? (
          <span
            role="progressbar"
            aria-label={labels.phase[phase]}
            aria-valuemin={determinate ? 0 : undefined}
            aria-valuemax={determinate ? 100 : undefined}
            /* Omitted entirely when nothing is being measured: an indeterminate
               progressbar must not claim a position it does not have. */
            aria-valuenow={determinate ? percent : undefined}
            aria-valuetext={determinate ? `${percent}%` : undefined}
            className="h-1.5 w-24 overflow-hidden rounded-pill bg-muted"
          >
            <span
              aria-hidden
              className="block h-full rounded-pill bg-primary transition-[width] duration-base"
              style={{ width: determinate ? `${percent}%` : "100%" }}
            />
          </span>
        ) : null}
      </div>

      {/* The stage is named beside the number so "42%" says what is at 42%. */}
      {observation ? (
        <p className="text-xs leading-5 text-muted-foreground" data-testid={phaseTestID ? `${phaseTestID}-stage` : undefined}>
          {labels.processingStage[observation.stage]}
        </p>
      ) : null}

      {message ? (
        <p
          role={failed ? "alert" : "status"}
          data-testid={messageTestID}
          className={cn(
            "text-xs leading-5",
            failed ? "font-medium text-destructive" : "text-muted-foreground",
          )}
        >
          {message}
        </p>
      ) : null}

      {failed && onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="rounded-md border border-input px-2.5 py-1 text-xs font-semibold text-foreground hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          {labels.retry}
        </button>
      ) : null}
    </div>
  );
}
