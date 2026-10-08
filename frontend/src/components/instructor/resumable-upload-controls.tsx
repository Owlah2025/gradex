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
  ResumeFileMismatchError,
  type ResumableInput,
  type SavedUploadSummary,
} from "@/lib/api/media-multipart";
import { ThroughputMeter, type UploadProgress } from "@/lib/api/upload-progress";
import {
  cancelledLine,
  resumingLine,
  retryingLine,
  savedUploadCopy,
  transferLine,
} from "./resumable-upload-copy";

export function isPausedUpload(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export type TransferReadout = {
  progress: UploadProgress;
  bytesPerSecond: number | null;
  secondsRemaining: number | null;
};

// Progress events arrive per XHR many times a second; the readout is refreshed at most this often,
// except when a part finishes, a retry starts or ends, or the upload reaches 100%.
const READOUT_INTERVAL_MS = 150;

export function useResumableUpload(input: Omit<ResumableInput, "csrf">) {
  const [pending, setPending] = useState(false);
  const [saved, setSaved] = useState<SavedUploadSummary | null>(null);
  const [running, setRunning] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [cancelled, setCancelled] = useState(false);
  const [resuming, setResuming] = useState<SavedUploadSummary | null>(null);
  const [transfer, setTransfer] = useState<TransferReadout | null>(null);
  const [fileAvailable, setFileAvailable] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const controller = useRef<AbortController | null>(null);
  const operation = useRef<Promise<unknown> | null>(null);
  // The File survives a pause in this tab, so Resume needs no picker. A reload loses it.
  const lastFile = useRef<File | null>(null);
  // Every recovery read belongs to one generation. A control switch, a new run, a cancel, an
  // acknowledgement or an unmount starts a new one, so a late server answer for an earlier state
  // (or another control) can never overwrite what is shown now.
  const generation = useRef(0);
  const invalidateRecovery = () => ++generation.current;

  const reloadSaved = () => {
    const ticket = invalidateRecovery();
    const local = readSavedUpload(input);
    setPending(hasResumableUpload(input));
    setSaved(local);
    if (!local) return;
    // The server's part list replaces the local hint as soon as it answers.
    void refreshSavedUpload(input).then((summary) => {
      if (generation.current === ticket && !controller.current) setSaved(summary);
    });
  };

  useEffect(() => {
    reloadSaved();
    return () => {
      invalidateRecovery();
      controller.current?.abort(
        new DOMException("Upload paused", "AbortError"),
      );
    };
    // Recovery belongs to one course/revision/control.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [input.courseID, input.revisionID, input.storageKeyId]);

  const run = async (
    file: File,
    progress: (fraction: number) => void,
    contentType?: string,
  ) => {
    if (controller.current) throw new Error("This upload is already running");
    const active = new AbortController();
    controller.current = active;
    invalidateRecovery();
    lastFile.current = file;
    setFileAvailable(true);
    setRunning(true);
    setCancelled(false);
    setResuming(null);
    setTransfer(null);
    setError(null);
    const csrf = currentCSRFToken();
    if (!csrf) {
      controller.current = null;
      setRunning(false);
      throw new Error("Your session must be refreshed before uploading.");
    }
    const meter = new ThroughputMeter();
    let lastReadout = 0;
    let lastRetrying = false;
    let lastCompleted = -1;
    const task = uploadResumable(
      file,
      {
        ...input,
        csrf,
        contentType,
        signal: active.signal,
        onVerifying: () => setVerifying(true),
        onResuming: (summary) => setResuming(summary),
      },
      (fraction, detail) => {
        const now = Date.now();
        // Speed is measured from the first byte this run actually sends, not from the moment the
        // run started (fingerprinting, session recovery and signing are not upload throughput).
        if (detail.transferredBytes > detail.resumedFromBytes) meter.record(detail.reportedBytes, now);
        const settled = detail.reportedBytes >= detail.totalBytes;
        if (
          now - lastReadout < READOUT_INTERVAL_MS &&
          detail.retrying === lastRetrying &&
          detail.completedBytes === lastCompleted &&
          !settled
        )
          return;
        lastReadout = now;
        lastRetrying = detail.retrying;
        lastCompleted = detail.completedBytes;
        setTransfer({
          progress: detail,
          bytesPerSecond: meter.bytesPerSecond(),
          secondsRemaining: meter.secondsRemaining(detail.totalBytes - detail.reportedBytes),
        });
        progress(fraction);
      },
    );
    operation.current = task;
    try {
      const result = await task;
      active.signal.throwIfAborted();
      return result;
    } catch (cause) {
      // A refused file must never become the in-tab file that "Resume upload" would send.
      if (cause instanceof ResumeFileMismatchError) {
        lastFile.current = null;
        setFileAvailable(false);
      }
      throw cause;
    } finally {
      controller.current = null;
      operation.current = null;
      setRunning(false);
      setVerifying(false);
      setResuming(null);
      setTransfer(null);
      reloadSaved();
    }
  };
  const cancel = async () => {
    invalidateRecovery();
    setCancelling(true);
    setError(null);
    controller.current?.abort(new DOMException("Upload paused", "AbortError"));
    try {
      if (operation.current) await operation.current.catch(() => undefined);
      const csrf = currentCSRFToken();
      if (!csrf)
        throw new Error("Your session must be refreshed before cancelling.");
      await cancelResumableUpload({ ...input, csrf });
      lastFile.current = null;
      setFileAvailable(false);
      setPending(false);
      setSaved(null);
      setCancelled(true);
    } catch (cause) {
      setError(describeApiError(cause, input.locale));
    } finally {
      setCancelling(false);
    }
  };
  return {
    run,
    pending,
    saved,
    running,
    verifying,
    cancelling,
    cancelled,
    resuming,
    transfer,
    fileAvailable,
    /** The File picked in this tab, if any; resuming with it needs no picker. */
    lastFile: () => lastFile.current,
    error,
    pause: () =>
      controller.current?.abort(
        new DOMException("Upload paused", "AbortError"),
      ),
    cancel,
    acknowledge: (digest: string) => {
      invalidateRecovery();
      acknowledgeResumableUpload(
        { ...input, csrf: currentCSRFToken() || "" },
        digest,
      );
      lastFile.current = null;
      setFileAvailable(false);
      setPending(false);
      setSaved(null);
    },
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
          data-total-bytes={transfer.progress.totalBytes}
        >
          {transferLine(transfer.progress, transfer.bytesPerSecond, transfer.secondsRemaining, locale)}
        </p>
      ) : null}
      {upload.running && transfer?.progress.retrying ? (
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
        {upload.running && !upload.verifying ? (
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
