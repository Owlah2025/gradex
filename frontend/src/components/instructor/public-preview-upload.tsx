"use client";

import { useRef, useState } from "react";
import { clearPublicPreview, setPublicPreview } from "@/lib/api/authoring";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import {
  ACCEPTED_VIDEO_CONTENT_TYPES,
  beginPublicPreviewUpload,
  completeUpload,
  describeAssetState,
  isReadyState,
  newProviderEventID,
  sha256Hex,
  uploadFileToStorage,
  validateSelectedVideo,
  waitForProcessing,
} from "@/lib/api/media-upload";

type Phase = "IDLE" | "PREPARING" | "UPLOADING" | "PROCESSING" | "ATTACHING" | "READY" | "FAILED";

type PublicPreviewUploadProps = {
  courseID: string;
  revisionID: string;
  hasPreview: boolean;
  locale: "ar" | "en";
  onChanged: () => void | Promise<void>;
};

/**
 * The only Instructor control for a public preview. It intentionally has no
 * Lesson picker and never renders asset IDs: the API creates a PREVIEW asset
 * already bound to this editable revision, then the semantic attach command
 * independently proves that relationship after the asset is READY.
 */
export function PublicPreviewUpload({
  courseID,
  revisionID,
  hasPreview,
  locale,
  onChanged,
}: PublicPreviewUploadProps) {
  const { t: dictionary } = useLocale();
  const media = dictionary.instructor.media;
  const t = media.preview;
  const input = useRef<HTMLInputElement>(null);
  const [phase, setPhase] = useState<Phase>("IDLE");
  const [progress, setProgress] = useState(0);
  const [message, setMessage] = useState<string | null>(null);
  const busy = !["IDLE", "READY", "FAILED"].includes(phase);

  async function upload(file: File) {
    const rejected = validateSelectedVideo(file, locale);
    if (rejected) {
      setPhase("FAILED");
      setMessage(rejected);
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
      const ticket = await beginPublicPreviewUpload({
        courseID,
        revisionID,
        contentType: file.type,
        sizeBytes: file.size,
        locale,
        csrf,
      });
      const sha256 = await sha256Hex(file);

      setPhase("UPLOADING");
      const stored = await uploadFileToStorage(ticket.upload_url, file, file.type, setProgress);

      setPhase("PROCESSING");
      await completeUpload({
        assetVersionID: ticket.asset_version_id,
        providerEventID: newProviderEventID(),
        storageObjectKey: ticket.storage_object_key,
        storageObjectVersion: stored.storageObjectVersion,
        contentType: file.type,
        sizeBytes: file.size,
        sha256,
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
      await setPublicPreview({ courseID, revisionID, assetVersionID: ticket.asset_version_id, locale, csrf });
      await onChanged();
      setPhase("READY");
      setMessage(t.ready);
    } catch (cause) {
      setPhase("FAILED");
      setMessage(describeApiError(cause, locale) || t.failed);
    }
  }

  async function remove() {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setPhase("FAILED");
      setMessage(media.csrfMissing);
      return;
    }
    setMessage(null);
    try {
      setPhase("ATTACHING");
      await clearPublicPreview({ courseID, revisionID, locale, csrf });
      await onChanged();
      setPhase("READY");
      setMessage(t.removed);
    } catch (cause) {
      setPhase("FAILED");
      setMessage(describeApiError(cause, locale) || t.failed);
    }
  }

  const status =
    phase === "PREPARING" || phase === "ATTACHING"
      ? t.processing
      : phase === "UPLOADING"
        ? `${t.upload} ${Math.round(progress * 100)}%`
        : phase === "PROCESSING"
          ? t.processing
          : message;

  return (
    <section
      data-testid="public-preview-authoring"
      className="rounded-lg border border-border bg-card p-4"
      aria-labelledby="public-preview-title"
    >
      <h3 id="public-preview-title" className="font-display text-base font-bold text-foreground">
        {t.title}
      </h3>
      <p className="mt-1 text-sm leading-6 text-muted-foreground">{t.description}</p>
      <p
        data-testid="public-preview-state"
        data-preview-attached={hasPreview ? "true" : "false"}
        className="mt-3 text-sm font-semibold text-foreground"
      >
        {hasPreview ? t.selected : t.absent}
      </p>
      <input
        ref={input}
        type="file"
        accept={ACCEPTED_VIDEO_CONTENT_TYPES.join(",")}
        className="sr-only"
        disabled={busy}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.currentTarget.value = "";
          if (file) void upload(file);
        }}
      />
      <div className="mt-4 flex flex-wrap gap-2">
        <Button
          type="button"
          size="sm"
          disabled={busy}
          onClick={() => input.current?.click()}
          data-testid="upload-public-preview"
        >
          {hasPreview ? t.replace : t.choose}
        </Button>
        {hasPreview ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => void remove()}
            data-testid="remove-public-preview"
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
          >
            {t.remove}
          </Button>
        ) : null}
      </div>
      {status ? (
        /* A failure must not read like a success: different role, different ink. */
        <p
          role={phase === "FAILED" ? "alert" : "status"}
          data-testid="public-preview-message"
          data-upload-phase={phase}
          className={
            phase === "FAILED"
              ? "mt-3 text-sm font-medium text-destructive"
              : "mt-3 text-sm text-muted-foreground"
          }
        >
          {status}
        </p>
      ) : null}
    </section>
  );
}
