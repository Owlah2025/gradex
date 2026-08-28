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
import { useLocale } from "@/lib/i18n/locale-provider";
import { UploadStatus, isUploadBusy, type UploadPhase } from "./upload-status";

type Phase = Extract<
  UploadPhase,
  "IDLE" | "PREPARING" | "UPLOADING" | "PROCESSING" | "ATTACHING" | "READY" | "FAILED"
>;

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
  const { t } = useLocale();
  const media = t.instructor.media;
  const [phase, setPhase] = useState<Phase>("IDLE");
  const [progress, setProgress] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);

  const busy = isUploadBusy(phase);

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
      setMessage(media.csrfMissing);
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
      setMessage(media.videoAttached);
      await onAttached();
    } catch (error) {
      setPhase("FAILED");
      setMessage(describeApiError(error, locale));
    }
  };

  return (
    <div className="space-y-2" data-testid={`lesson-video-upload-${lessonID}`}>
      <label
        htmlFor={`lesson-video-file-${lessonID}`}
        className="block font-display text-xs font-bold text-foreground"
      >
        {media.videoLabel}
      </label>
      <p className="text-xs text-muted-foreground">{media.videoHint}</p>
      <input
        id={`lesson-video-file-${lessonID}`}
        ref={fileInput}
        type="file"
        accept={ACCEPTED_VIDEO_CONTENT_TYPES.join(",")}
        disabled={busy}
        data-testid={`lesson-video-file-${lessonID}`}
        className="block w-full max-w-full text-xs text-muted-foreground file:me-3 file:rounded-pill file:border-0 file:bg-secondary file:px-3 file:py-1.5 file:font-display file:text-xs file:font-bold file:text-secondary-foreground hover:file:bg-secondary/80"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) void run(file);
          // Clearing the input lets the same file be retried after a failure.
          event.target.value = "";
        }}
      />
      <UploadStatus
        phase={phase}
        progress={progress}
        message={message}
        labels={media}
        phaseTestID={`lesson-video-phase-${lessonID}`}
        messageTestID={`lesson-video-message-${lessonID}`}
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
