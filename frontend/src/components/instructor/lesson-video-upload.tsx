"use client";

import React, { useRef, useState } from "react";
import { setLessonVideo } from "@/lib/api/authoring";
import {
  ACCEPTED_VIDEO_CONTENT_TYPES,
  beginVideoUpload,
  completeVideoUpload,
  describeAssetState,
  isReadyState,
  newProviderEventID,
  sha256Hex,
  uploadFileToStorage,
  validateSelectedVideo,
  waitForProcessing,
} from "@/lib/api/media-upload";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";

type Phase = "IDLE" | "PREPARING" | "UPLOADING" | "PROCESSING" | "ATTACHING" | "READY" | "FAILED";

const PHASE_LABELS: Record<Phase, { en: string; ar: string }> = {
  IDLE: { en: "No upload in progress", ar: "لا يوجد رفع جارٍ" },
  PREPARING: { en: "Preparing", ar: "جارٍ التحضير" },
  UPLOADING: { en: "Uploading", ar: "جارٍ الرفع" },
  PROCESSING: { en: "Processing", ar: "جارٍ المعالجة" },
  ATTACHING: { en: "Attaching", ar: "جارٍ الإرفاق" },
  READY: { en: "Ready", ar: "جاهز" },
  FAILED: { en: "Failed", ar: "فشل" },
};

export type LessonVideoUploadProps = {
  courseID: string;
  revisionID: string;
  lessonID: string;
  locale: "ar" | "en";
  onAttached: () => void | Promise<void>;
};

/**
 * Instructor-facing MP4 upload for one Lesson.
 *
 * It drives the existing media contract end to end — intent, direct upload to
 * private storage, completion evidence, bounded processing poll, then the
 * Course revision video attachment — and reports only what the server
 * confirmed. Nothing is shown as attached on the strength of local state.
 */
export function LessonVideoUpload({
  courseID,
  revisionID,
  lessonID,
  locale,
  onAttached,
}: LessonVideoUploadProps) {
  const isAr = locale === "ar";
  const [phase, setPhase] = useState<Phase>("IDLE");
  const [progress, setProgress] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);

  const busy = phase === "PREPARING" || phase === "UPLOADING" || phase === "PROCESSING" || phase === "ATTACHING";

  const run = async (file: File) => {
    const rejection = validateSelectedVideo(file, locale);
    if (rejection) {
      setPhase("FAILED");
      setMessage(rejection);
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      setPhase("FAILED");
      setMessage(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }

    setMessage(null);
    setProgress(0);
    try {
      setPhase("PREPARING");
      const ticket = await beginVideoUpload({
        courseID,
        lessonID,
        contentType: file.type,
        sizeBytes: file.size,
        locale,
        csrf,
      });

      // The digest is computed before the PUT so the completion evidence
      // describes the bytes this browser actually sent.
      const digest = await sha256Hex(file);

      setPhase("UPLOADING");
      const uploaded = await uploadFileToStorage(ticket.upload_url, file, file.type, setProgress);

      setPhase("PROCESSING");
      await completeVideoUpload({
        assetVersionID: ticket.asset_version_id,
        providerEventID: newProviderEventID(),
        storageObjectKey: ticket.storage_object_key,
        storageObjectVersion: uploaded.storageObjectVersion,
        contentType: file.type,
        sizeBytes: file.size,
        sha256: digest,
        locale,
        csrf,
      });

      const status = await waitForProcessing(ticket.asset_version_id, locale);
      if (!isReadyState(status.state)) {
        setPhase("FAILED");
        setMessage(describeAssetState(status.state, locale));
        return;
      }

      setPhase("ATTACHING");
      await setLessonVideo({
        courseID,
        revisionID,
        lessonID,
        assetVersionID: ticket.asset_version_id,
        locale,
        csrf,
      });

      setPhase("READY");
      setMessage(isAr ? "تم إرفاق الفيديو بالدرس." : "Video attached to this Lesson.");
      await onAttached();
    } catch (error) {
      setPhase("FAILED");
      setMessage(describeApiError(error, locale));
    }
  };

  const label = PHASE_LABELS[phase];

  return (
    <div className="mt-2 flex flex-col gap-1" data-testid={`lesson-video-upload-${lessonID}`}>
      <div className="flex flex-wrap items-center gap-2">
        <input
          ref={fileInput}
          type="file"
          accept={ACCEPTED_VIDEO_CONTENT_TYPES.join(",")}
          disabled={busy}
          aria-label={isAr ? "اختر ملف MP4" : "Select an MP4 file"}
          data-testid={`lesson-video-file-${lessonID}`}
          className="text-[11px]"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) void run(file);
            // Clearing the input lets the same file be retried after a failure.
            event.target.value = "";
          }}
        />
        <span
          data-testid={`lesson-video-phase-${lessonID}`}
          className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] text-slate-700 dark:bg-slate-800 dark:text-slate-300"
        >
          {isAr ? label.ar : label.en}
          {phase === "UPLOADING" ? ` ${Math.round(progress * 100)}%` : ""}
        </span>
      </div>
      {message && (
        <p role="status" className="text-[11px] text-slate-700 dark:text-slate-300">
          {message}
        </p>
      )}
      {phase === "FAILED" && (
        <button
          type="button"
          className="self-start rounded border border-slate-300 px-2 py-1 text-[11px] dark:border-slate-700"
          onClick={() => {
            setPhase("IDLE");
            setMessage(null);
            setProgress(0);
            fileInput.current?.click();
          }}
        >
          {isAr ? "إعادة المحاولة" : "Retry upload"}
        </button>
      )}
    </div>
  );
}
