"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";
import {
  acknowledgeResumableUpload,
  cancelResumableUpload,
  hasResumableUpload,
  readSavedUpload,
  refreshSavedUpload,
  uploadResumable,
  type MultipartCompletionResult,
  type ResumableInput,
} from "@/lib/api/media-multipart";
import {
  ResumableUploadSession,
  initialResumableState,
  transferInProgress,
  type ResumableState,
  type SessionDependencies,
} from "./resumable-upload-session";
import {
  cancelledLine,
  pausingLine,
  resumingLine,
  savedForResumeLine,
  retryingLine,
  savedUploadCopy,
  transferLine,
} from "./resumable-upload-copy";

export function isPausedUpload(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export type { TransferReadout } from "./resumable-upload-session";

const browserDependencies: SessionDependencies = {
  upload: uploadResumable,
  hasSaved: hasResumableUpload,
  readSaved: readSavedUpload,
  refreshSaved: refreshSavedUpload,
  cancelSaved: cancelResumableUpload,
  acknowledgeSaved: acknowledgeResumableUpload,
  csrf: currentCSRFToken,
  describeError: describeApiError,
  now: () => Date.now(),
};

/**
 * React binding for one upload control. The lifecycle lives in ResumableUploadSession so that a
 * control switching Lessons mid-upload, late server answers and the in-tab File are handled in one
 * place that tests can drive directly.
 */
export function useResumableUpload(input: Omit<ResumableInput, "csrf">) {
  const [state, setState] = useState<ResumableState>(initialResumableState);
  const session = useRef<ResumableUploadSession | null>(null);
  if (!session.current) session.current = new ResumableUploadSession(browserDependencies, setState);
  const current = session.current;

  useEffect(() => {
    current.setInput(input);
    // Identity is courseID/revisionID/storageKeyId; the session ignores anything else changing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current, input.courseID, input.revisionID, input.storageKeyId, input.lessonID, input.kind, input.locale]);
  useEffect(() => () => current.dispose(), [current]);

  // While bytes are on the wire, leaving the page discards the parts still being sent (completed
  // parts are saved and resumable). Browsers show their own wording for this prompt; the control
  // itself says which part is safe. Nothing is asked once the server is verifying or processing.
  const warnBeforeLeaving = transferInProgress(state);
  useEffect(() => {
    if (!warnBeforeLeaving) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [warnBeforeLeaving]);

  return {
    ...state,
    run: (file: File, progress: (fraction: number) => void, contentType?: string) =>
      current.run(file, progress, contentType),
    /** The File picked in this tab for this control, if any; resuming with it needs no picker. */
    lastFile: () => current.lastFile(),
    pause: () => current.pause(),
    /** End a graceful pause at once; parts still in flight are discarded. */
    stopNow: () => current.stopNow(),
    cancel: () => current.cancel(),
    /** Pass the result object `run` returned; it identifies the run and the control it belonged to. */
    acknowledge: (result: MultipartCompletionResult) => current.acknowledge(result),
  };
}

export function ResumableUploadControls({
  upload,
  locale,
  onReselect,
  locked = false,
}: {
  upload: ReturnType<typeof useResumableUpload>;
  locale: "ar" | "en";
  /** Resume: with the in-tab File when there is one, otherwise by opening the picker. */
  onReselect: () => void;
  locked?: boolean;
}) {
  const ar = locale === "ar";
  const transfer = upload.transfer;
  const showSaved = upload.pending && !upload.running && !locked && upload.saved;
  const savedCopy = upload.saved
    ? savedUploadCopy(upload.saved, locale, upload.fileAvailable)
    : null;
  // "Resuming from 68%…" until the first byte beyond what the server already held moves.
  const resumingNow =
    upload.running &&
    upload.resuming &&
    (!transfer || transfer.progress.transferredBytes <= transfer.progress.resumedFromBytes);
  return (
    <div className="space-y-2" data-testid="resumable-upload-controls">
      {resumingNow && upload.resuming ? (
        <p role="status" className="text-xs text-muted-foreground" data-testid="resumable-resuming">
          {resumingLine(upload.resuming, locale)}
        </p>
      ) : null}
      {upload.running && transfer && !upload.verifying ? (
        <p
          className="text-xs tabular-nums text-muted-foreground"
          data-testid="resumable-transfer"
          data-reported-bytes={transfer.progress.reportedBytes}
          data-completed-bytes={transfer.progress.completedBytes}
          data-total-bytes={transfer.progress.totalBytes}
        >
          {transferLine(transfer.progress, transfer.bytesPerSecond, transfer.secondsRemaining, locale)}
        </p>
      ) : null}
      {upload.running && transfer && !upload.verifying && savedForResumeLine(transfer.progress, locale) ? (
        <p
          className="text-xs tabular-nums text-muted-foreground"
          data-testid="resumable-saved-for-resume"
          data-completed-bytes={transfer.progress.completedBytes}
        >
          {savedForResumeLine(transfer.progress, locale)}
        </p>
      ) : null}
      {upload.pausing ? (
        <p role="status" className="text-xs font-medium text-foreground" data-testid="resumable-pausing">
          {pausingLine(locale)}
        </p>
      ) : null}
      {upload.running && transfer?.progress.retrying && !upload.pausing ? (
        <p role="status" className="text-xs font-medium text-foreground" data-testid="resumable-retrying">
          {retryingLine(locale)}
        </p>
      ) : null}
      {upload.verifying ? (
        <p role="status" className="text-xs text-muted-foreground">
          {ar
            ? "جارٍ التحقق من الملف المرفوع. يمكنك مغادرة الصفحة والعودة لاحقاً."
            : "Verifying the uploaded file. You can leave this page and return later."}
        </p>
      ) : null}
      {showSaved && savedCopy ? (
        <div
          role="status"
          className="space-y-0.5 rounded-md border border-border bg-muted/40 px-3 py-2 text-xs"
          data-testid="resumable-saved"
          data-saved-status={upload.saved?.status}
          data-saved-percent={upload.saved?.percent ?? undefined}
          data-saved-source={upload.saved?.source}
        >
          <p className="font-semibold text-foreground">{savedCopy.title}</p>
          {savedCopy.detail ? (
            <p className="tabular-nums text-muted-foreground">{savedCopy.detail}</p>
          ) : null}
          <p className="text-muted-foreground">{savedCopy.instruction}</p>
        </div>
      ) : null}
      {upload.cancelled && !upload.pending && !upload.running ? (
        <p role="status" className="text-xs text-muted-foreground" data-testid="resumable-cancelled">
          {cancelledLine(locale)}
        </p>
      ) : null}
      {upload.error ? (
        <p role="alert" className="text-xs text-destructive">
          {upload.error}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2 [&>button]:h-auto [&>button]:min-h-9 [&>button]:max-w-full [&>button]:whitespace-normal [&>button]:py-1.5">
        {upload.running && !upload.verifying && upload.pausing ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={upload.stopNow}
            disabled={upload.cancelling}
          >
            {ar ? "إيقاف الآن" : "Stop now"}
          </Button>
        ) : upload.running && !upload.verifying ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={upload.pause}
            disabled={upload.cancelling}
          >
            {ar ? "إيقاف مؤقت" : "Pause upload"}
          </Button>
        ) : !upload.running && upload.pending && upload.saved?.status !== "CANCELLED" ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onReselect}
            disabled={upload.cancelling || locked}
          >
            {savedCopy?.action ?? (ar ? "استكمال الرفع" : "Resume upload")}
          </Button>
        ) : null}
        {upload.running || upload.pending ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => void upload.cancel()}
            disabled={upload.cancelling || locked}
          >
            {upload.cancelling
              ? ar
                ? "جارٍ الإلغاء…"
                : "Cancelling…"
              : ar
                ? "إلغاء الرفع"
                : "Cancel upload"}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
