"use client";

import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { cn } from "@/lib/utils";

type MediaLabels = Dictionary["instructor"]["media"];

/** The phases the media client genuinely passes through. Nothing here is invented. */
export type UploadPhase =
  | "IDLE"
  | "PREPARING"
  | "UPLOADING"
  | "PROCESSING"
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
 * The percentage appears on `UPLOADING` alone, because the browser can only measure the bytes it
 * is sending. Server-side processing reports no fraction, so none is shown — a bar creeping
 * through a phase nobody is measuring is a lie with a progress indicator on it.
 */
export function UploadStatus({
  phase,
  progress,
  message,
  labels,
  phaseTestID,
  messageTestID,
  onRetry,
}: {
  phase: UploadPhase;
  /** 0–1, meaningful during UPLOADING only. */
  progress: number;
  message: string | null;
  labels: MediaLabels;
  phaseTestID?: string;
  messageTestID?: string;
  onRetry?: () => void;
}) {
  const failed = phase === "FAILED";
  const busy = isUploadBusy(phase);

  return (
    <div className="space-y-1.5">
      <div className="flex flex-wrap items-center gap-2">
        <span
          data-testid={phaseTestID}
          data-upload-phase={phase}
          className={cn(
            "rounded-pill px-2 py-0.5 text-xs font-semibold",
            failed && "bg-destructive/10 text-destructive",
            phase === "READY" && "bg-gx-success-soft text-gx-success",
            !failed && phase !== "READY" && "bg-muted text-muted-foreground",
          )}
        >
          {labels.phase[phase]}
          {phase === "UPLOADING" ? ` ${Math.round(progress * 100)}%` : ""}
        </span>
        {busy ? (
          <span
            aria-hidden
            className="h-1.5 w-24 overflow-hidden rounded-pill bg-muted"
          >
            {/* Width tracks real bytes during UPLOADING; every other busy phase shows the bar at
                rest rather than pretending to advance. */}
            <span
              className="block h-full rounded-pill bg-primary transition-[width] duration-base"
              style={{ width: phase === "UPLOADING" ? `${Math.round(progress * 100)}%` : "100%" }}
            />
          </span>
        ) : null}
      </div>

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
