"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";
import {
  acknowledgeResumableUpload,
  cancelResumableUpload,
  hasResumableUpload,
  uploadResumable,
  type ResumableInput,
} from "@/lib/api/media-multipart";

export function isPausedUpload(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export function useResumableUpload(input: Omit<ResumableInput, "csrf">) {
  const [pending, setPending] = useState(false);
  const [running, setRunning] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const controller = useRef<AbortController | null>(null);
  const operation = useRef<Promise<unknown> | null>(null);
  useEffect(() => {
    setPending(hasResumableUpload(input));
    return () =>
      controller.current?.abort(
        new DOMException("Upload paused", "AbortError"),
      );
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
    setRunning(true);
    setError(null);
    const csrf = currentCSRFToken();
    if (!csrf) {
      controller.current = null;
      setRunning(false);
      throw new Error("Your session must be refreshed before uploading.");
    }
    const task = uploadResumable(
      file,
      {
        ...input,
        csrf,
        contentType,
        signal: active.signal,
        onVerifying: () => setVerifying(true),
      },
      progress,
    );
    operation.current = task;
    try {
      const result = await task;
      active.signal.throwIfAborted();
      return result;
    } finally {
      controller.current = null;
      operation.current = null;
      setRunning(false);
      setVerifying(false);
      setPending(hasResumableUpload(input));
    }
  };
  const cancel = async () => {
    setCancelling(true);
    setError(null);
    controller.current?.abort(new DOMException("Upload paused", "AbortError"));
    try {
      if (operation.current) await operation.current.catch(() => undefined);
      const csrf = currentCSRFToken();
      if (!csrf)
        throw new Error("Your session must be refreshed before cancelling.");
      await cancelResumableUpload({ ...input, csrf });
      setPending(false);
    } catch (cause) {
      setError(describeApiError(cause, input.locale));
    } finally {
      setCancelling(false);
    }
  };
  return {
    run,
    pending,
    running,
    verifying,
    cancelling,
    error,
    pause: () =>
      controller.current?.abort(
        new DOMException("Upload paused", "AbortError"),
      ),
    cancel,
    acknowledge: (digest: string) => {
      acknowledgeResumableUpload(
        { ...input, csrf: currentCSRFToken() || "" },
        digest,
      );
      setPending(false);
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
  onReselect: () => void;
  locked?: boolean;
}) {
  const ar = locale === "ar";
  return (
    <div className="space-y-2">
      {upload.verifying ? (
        <p role="status" className="text-xs text-muted-foreground">
          {ar
            ? "جارٍ التحقق من الملف المرفوع. يمكنك مغادرة الصفحة والعودة لاحقاً."
            : "Verifying the uploaded file. You can leave this page and return later."}
        </p>
      ) : null}
      {upload.pending && !upload.running && !locked ? (
        <p role="status" className="text-xs text-muted-foreground">
          {ar
            ? "يوجد رفع محفوظ. اختر الملف نفسه لاستكمال الرفع."
            : "A saved upload is available. Reselect the same file to resume."}
        </p>
      ) : null}
      {upload.error ? (
        <p role="alert" className="text-xs text-destructive">
          {upload.error}
        </p>
      ) : null}
      <div className="flex flex-wrap gap-2">
        {upload.running ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={upload.pause}
            disabled={upload.cancelling}
          >
            {ar ? "إيقاف مؤقت" : "Pause upload"}
          </Button>
        ) : upload.pending ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onReselect}
            disabled={upload.cancelling || locked}
          >
            {ar ? "استكمال الرفع" : "Resume upload"}
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
