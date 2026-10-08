"use client";

import { ResumableUploadControls, useResumableUpload, isPausedUpload } from "./resumable-upload-controls";
import React, { useEffect, useRef, useState } from "react";
import { addLessonFile, deleteLessonFile, type LessonFileWire } from "@/lib/api/authoring";
import {
  ACCEPTED_RESOURCE_CONTENT_TYPES,
  ACCEPTED_RESOURCE_EXTENSIONS,
  describeAssetState,
  isReadyState,
  validateSelectedResource,
  waitForProcessing,
} from "@/lib/api/media-upload";
import { currentCSRFToken } from "@/lib/identity/session";
import { describeApiError } from "@/lib/api/api-error";
import { describeUploadError } from "./resumable-upload-copy";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import { UploadStatus, isUploadBusy, type UploadPhase } from "./upload-status";

type Phase = Extract<
  UploadPhase,
  "IDLE" | "PREPARING" | "UPLOADING" | "VERIFYING" | "CHECKING" | "ATTACHING" | "READY" | "FAILED"
>;

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
  const { t } = useLocale();
  const media = t.instructor.media;
  const isAr = locale === "ar";
  const [phase, setPhase] = useState<Phase>("IDLE");
  const [progress, setProgress] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);

  const busy = isUploadBusy(phase);
  const resumable = useResumableUpload({ courseID, revisionID, lessonID, kind: "RESOURCE", storageKeyId: "resource-" + lessonID, locale });
  // Every byte is stored; the server is verifying, so no upload percentage is shown any more.
  useEffect(() => {
    if (resumable.verifying) setPhase("VERIFYING");
  }, [resumable.verifying]);
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
      fail(media.csrfMissing);
      return;
    }

    setMessage(null);
    setProgress(0);
    try {
      setPhase("PREPARING");
      const completion = await resumable.run(file, (fraction) => { setPhase("UPLOADING"); setProgress(fraction); }, checked.contentType);

      // A validated Lesson Resource is READY the moment completion returns; a
      // scanner-gated deployment needs the poll. Both are handled without the
      // Instructor needing to know which one this deployment is.
      if (!isReadyState(completion.state)) setPhase("CHECKING");
      const state = isReadyState(completion.state)
        ? completion
        : await waitForProcessing(completion.asset_version_id, locale);
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
        assetVersionID: completion.asset_version_id,
        displayNameAr: displayName,
        displayNameEn: displayName,
        locale,
        csrf,
      });

      setPhase("READY");
      resumable.acknowledge(completion.sha256_hex);
      setMessage(media.resourceAttached);
      await onChanged();
    } catch (error) {
      if (isPausedUpload(error)) { setPhase("IDLE"); setMessage(null); return; }
      fail(describeUploadError(error, locale));
    }
  };

  const remove = async (fileID: string) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      fail(media.csrfMissing);
      return;
    }
    setRemoving(fileID);
    setMessage(null);
    try {
      await deleteLessonFile({ courseID, revisionID, lessonID, fileID, locale, csrf });
      setPhase("IDLE");
      setMessage(media.resourceRemoved);
      await onChanged();
    } catch (error) {
      fail(describeApiError(error, locale));
    } finally {
      setRemoving(null);
    }
  };

  return (
    <div className="space-y-2" data-testid={`lesson-resource-upload-${lessonID}`}>
      {resources.length > 0 && (
        <div>
          <p className="font-display text-[11px] font-bold uppercase tracking-wide text-muted-foreground">
            {media.attachedFiles}
          </p>
          <ul className="mt-1 space-y-1" data-testid={`lesson-resource-list-${lessonID}`}>
            {resources.map((file) => (
              <li
                key={file.id}
                data-testid={`lesson-resource-${file.id}`}
                className="flex items-center justify-between gap-2 rounded-md bg-muted px-2 py-1"
              >
                {/*
                  A file name is arbitrary, user-supplied, and frequently long in both scripts.
                  It was rendered in a bare span that pushed the row — and the lesson card
                  around it — past the edge of the viewport. It truncates now, and keeps its
                  full value in `title` so nothing is actually lost.
                */}
                <span
                  className="min-w-0 flex-1 truncate text-xs text-foreground"
                  title={isAr ? file.display_name_ar : file.display_name_en}
                >
                  <bdi>{isAr ? file.display_name_ar : file.display_name_en}</bdi>
                </span>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={busy || removing === file.id}
                  onClick={() => void remove(file.id)}
                  data-testid={`remove-lesson-resource-${file.id}`}
                  className="h-7 shrink-0 px-2 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive"
                >
                  {removing === file.id ? media.removing : media.remove}
                </Button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <label
        htmlFor={`lesson-resource-file-${lessonID}`}
        className="block font-display text-xs font-bold text-foreground"
      >
        {media.resourceLabel}
      </label>
      <p className="text-xs text-muted-foreground">{media.resourceHint}</p>
      <input
        id={`lesson-resource-file-${lessonID}`}
        ref={fileInput}
        type="file"
        accept={[...ACCEPTED_RESOURCE_CONTENT_TYPES, ...ACCEPTED_RESOURCE_EXTENSIONS].join(",")}
        disabled={busy}
        data-testid={`lesson-resource-file-${lessonID}`}
        className="block w-full max-w-full text-xs text-muted-foreground file:me-3 file:rounded-pill file:border-0 file:bg-secondary file:px-3 file:py-1.5 file:font-display file:text-xs file:font-bold file:text-secondary-foreground hover:file:bg-secondary/80"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) void run(file);
          // Clearing the input lets the same file be retried after a failure.
          event.target.value = "";
        }}
      />
      <ResumableUploadControls upload={resumable} locale={locale} locked={busy && !resumable.running} onReselect={() => {
          const file = resumable.lastFile();
          if (file) void run(file);
          else fileInput.current?.click();
        }} />
      <UploadStatus
        phase={phase}
        progress={progress}
        message={message}
        labels={media}
        phaseTestID={`lesson-resource-phase-${lessonID}`}
        messageTestID={`lesson-resource-message-${lessonID}`}
        onRetry={() => {
          setPhase("IDLE");
          setMessage(null);
          setProgress(0);
          fileInput.current?.click();
        }}
      />
    </div>
  );
}
