"use client";

import React, { useRef, useState } from "react";
import { addLessonFile, deleteLessonFile, type LessonFileWire } from "@/lib/api/authoring";
import {
  ACCEPTED_RESOURCE_CONTENT_TYPES,
  ACCEPTED_RESOURCE_EXTENSIONS,
  beginResourceUpload,
  completeUpload,
  describeAssetState,
  isReadyState,
  newProviderEventID,
  sha256Hex,
  uploadFileToStorage,
  validateSelectedResource,
  waitForProcessing,
} from "@/lib/api/media-upload";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";

type Phase = "IDLE" | "PREPARING" | "UPLOADING" | "CHECKING" | "ATTACHING" | "READY" | "FAILED";

const PHASE_LABELS: Record<Phase, { en: string; ar: string }> = {
  IDLE: { en: "No upload in progress", ar: "لا يوجد رفع جارٍ" },
  PREPARING: { en: "Preparing", ar: "جارٍ التحضير" },
  UPLOADING: { en: "Uploading", ar: "جارٍ الرفع" },
  CHECKING: { en: "Checking the file", ar: "جارٍ فحص الملف" },
  ATTACHING: { en: "Attaching", ar: "جارٍ الإرفاق" },
  READY: { en: "Attached", ar: "تم الإرفاق" },
  FAILED: { en: "Failed", ar: "فشل" },
};

export type LessonResourceUploadProps = {
  courseID: string;
  revisionID: string;
  lessonID: string;
  locale: "ar" | "en";
  files: LessonFileWire[];
  onChanged: () => void | Promise<void>;
};

/**
 * Instructor-facing PDF/DOCX Lesson Resource upload.
 *
 * It drives the same media contract the video control uses — intent, direct
 * upload to private storage, completion evidence, then the Lesson attachment —
 * and reports only what the server confirmed. The Instructor picks a file and
 * names it; no Asset Version identifier is ever shown to be copied by hand.
 */
export function LessonResourceUpload({
  courseID,
  revisionID,
  lessonID,
  locale,
  files,
  onChanged,
}: LessonResourceUploadProps) {
  const isAr = locale === "ar";
  const [phase, setPhase] = useState<Phase>("IDLE");
  const [progress, setProgress] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);

  const busy =
    phase === "PREPARING" || phase === "UPLOADING" || phase === "CHECKING" || phase === "ATTACHING";
  const resources = files.filter((file) => file.kind === "RESOURCE");

  const fail = (text: string) => {
    setPhase("FAILED");
    setMessage(text);
  };

  const run = async (file: File) => {
    const checked = validateSelectedResource(file, locale);
    if ("error" in checked) {
      fail(checked.error);
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      fail(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }

    setMessage(null);
    setProgress(0);
    try {
      setPhase("PREPARING");
      const ticket = await beginResourceUpload({
        courseID,
        lessonID,
        contentType: checked.contentType,
        sizeBytes: file.size,
        locale,
        csrf,
      });

      // The digest is computed before the PUT so the completion evidence
      // describes the bytes this browser actually sent.
      const digest = await sha256Hex(file);

      setPhase("UPLOADING");
      const uploaded = await uploadFileToStorage(
        ticket.upload_url,
        file,
        checked.contentType,
        setProgress,
      );

      setPhase("CHECKING");
      const completion = await completeUpload({
        assetVersionID: ticket.asset_version_id,
        providerEventID: newProviderEventID(),
        storageObjectKey: ticket.storage_object_key,
        storageObjectVersion: uploaded.storageObjectVersion,
        contentType: checked.contentType,
        sizeBytes: file.size,
        sha256: digest,
        locale,
        csrf,
      });

      // A validated Lesson Resource is READY the moment completion returns; a
      // scanner-gated deployment needs the poll. Both are handled without the
      // Instructor needing to know which one this deployment is.
      const state = isReadyState(completion.state)
        ? completion
        : await waitForProcessing(ticket.asset_version_id, locale);
      if (!isReadyState(state.state)) {
        fail(describeAssetState(state.state, locale));
        return;
      }

      setPhase("ATTACHING");
      // The file's own name is the display name in both locales: the Instructor
      // uploaded one artefact, and inventing a second translated title here
      // would be fabricating content they never wrote.
      const displayName = file.name.slice(0, 200);
      await addLessonFile({
        courseID,
        revisionID,
        lessonID,
        kind: "RESOURCE",
        assetVersionID: ticket.asset_version_id,
        displayNameAr: displayName,
        displayNameEn: displayName,
        locale,
        csrf,
      });

      setPhase("READY");
      setMessage(isAr ? "تمت إضافة الملف إلى الدرس." : "File attached to this Lesson.");
      await onChanged();
    } catch (error) {
      fail(describeApiError(error, locale));
    }
  };

  const remove = async (fileID: string) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      fail(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }
    setRemoving(fileID);
    setMessage(null);
    try {
      await deleteLessonFile({ courseID, revisionID, lessonID, fileID, locale, csrf });
      setPhase("IDLE");
      setMessage(isAr ? "تمت إزالة الملف من الدرس." : "File removed from this Lesson.");
      await onChanged();
    } catch (error) {
      fail(describeApiError(error, locale));
    } finally {
      setRemoving(null);
    }
  };

  const label = PHASE_LABELS[phase];

  return (
    <div className="mt-2 flex flex-col gap-1" data-testid={`lesson-resource-upload-${lessonID}`}>
      {resources.length > 0 && (
        <ul className="flex flex-col gap-1" data-testid={`lesson-resource-list-${lessonID}`}>
          {resources.map((file) => (
            <li
              key={file.id}
              data-testid={`lesson-resource-${file.id}`}
              className="flex flex-wrap items-center gap-2 text-[11px]"
            >
              <span className="rounded bg-slate-100 px-1.5 py-0.5 dark:bg-slate-800">
                {isAr ? file.display_name_ar : file.display_name_en}
              </span>
              <button
                type="button"
                disabled={busy || removing === file.id}
                onClick={() => void remove(file.id)}
                data-testid={`remove-lesson-resource-${file.id}`}
                className="text-red-700 underline disabled:opacity-50 dark:text-red-400"
              >
                {removing === file.id
                  ? isAr
                    ? "جارٍ الإزالة…"
                    : "Removing…"
                  : isAr
                    ? "إزالة"
                    : "Remove"}
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <label className="text-[11px] font-medium text-slate-700 dark:text-slate-300">
          {isAr ? "مرفق الدرس (PDF أو DOCX)" : "Lesson resource (PDF or DOCX)"}
          <input
            ref={fileInput}
            type="file"
            accept={[...ACCEPTED_RESOURCE_CONTENT_TYPES, ...ACCEPTED_RESOURCE_EXTENSIONS].join(",")}
            disabled={busy}
            aria-label={isAr ? "اختر ملف PDF أو DOCX" : "Select a PDF or DOCX file"}
            data-testid={`lesson-resource-file-${lessonID}`}
            className="ms-2 text-[11px]"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) void run(file);
              // Clearing the input lets the same file be retried after a failure.
              event.target.value = "";
            }}
          />
        </label>
        <span
          data-testid={`lesson-resource-phase-${lessonID}`}
          className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-[10px] text-slate-700 dark:bg-slate-800 dark:text-slate-300"
        >
          {isAr ? label.ar : label.en}
          {phase === "UPLOADING" ? ` ${Math.round(progress * 100)}%` : ""}
        </span>
      </div>

      {message && (
        <p
          role={phase === "FAILED" ? "alert" : "status"}
          data-testid={`lesson-resource-message-${lessonID}`}
          className={
            phase === "FAILED"
              ? "text-[11px] text-red-700 dark:text-red-400"
              : "text-[11px] text-slate-700 dark:text-slate-300"
          }
        >
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
