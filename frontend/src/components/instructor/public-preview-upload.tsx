"use client";

import { useEffect, useRef, useState } from "react";
import { clearPublicPreview } from "@/lib/api/authoring";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import {
  ACCEPTED_VIDEO_CONTENT_TYPES,
  beginPublicPreviewUpload,
  completeAndSelectPublicPreview,
  describeAssetState,
  isReadyState,
  newProviderEventID,
  ProcessingObservationTimeoutError,
  sha256Hex,
  uploadFileToStorage,
  validateSelectedVideo,
  waitForProcessing,
} from "@/lib/api/media-upload";
import { recoverMediaPhase } from "./media-upload-phase";

type Phase =
  | "IDLE"
  | "PREPARING"
  | "UPLOADING"
  | "ATTACHING"
  | "PROCESSING"
  | "PROCESSING_BACKGROUND"
  | "READY"
  | "FAILED";

type PublicPreviewUploadProps = {
  courseID: string;
  revisionID: string;
  hasPreview: boolean;
  previewAssetVersionID?: string;
  previewAssetState?: string;
  locale: "ar" | "en";
  onChanged: () => void | Promise<void>;
};

/**
 * The only Instructor control for a public preview. It intentionally has no
 * Lesson picker and never renders asset IDs: the API creates a PREVIEW asset
 * already bound to this editable revision, and one idempotent completion
 * request both closes the upload and durably selects it for the revision.
 *
 * Selection happens before processing finishes, on purpose. D-096 sends a
 * trusted preview through the same FFmpeg path a Lesson video takes, which can
 * outlast this tab; if the browser were the thing that attached the preview
 * afterwards, a closed tab would orphan a perfectly good upload. The bounded
 * poll below is therefore an observation, not a step — timing out means
 * "still processing", never "failed".
 */
export function PublicPreviewUpload({
  courseID,
  revisionID,
  hasPreview,
  previewAssetVersionID,
  previewAssetState,
  locale,
  onChanged,
}: PublicPreviewUploadProps) {
  const { t: dictionary } = useLocale();
  const media = dictionary.instructor.media;
  const t = media.preview;
  const input = useRef<HTMLInputElement>(null);

  // First render and every server refresh derive the phase from what the
  // server projected, so a reload mid-processing recovers instead of showing
  // an idle control over a preview that is really still being prepared.
  const describeRecovered = (state?: string): string | null => {
    const recovered = recoverMediaPhase(previewAssetVersionID, state);
    if (recovered === "READY") return t.ready;
    if (recovered === "PROCESSING_BACKGROUND") return t.processingBackground;
    if (recovered === "FAILED") {
      return state === "UPLOADED"
        ? t.uploadInterrupted
        : describeAssetState(state || "PROCESS_FAILED", locale);
    }
    return null;
  };

  const initialPhase = recoverMediaPhase(previewAssetVersionID, previewAssetState);
  const [phase, setPhase] = useState<Phase>(initialPhase);
  const [progress, setProgress] = useState(0);
  const [message, setMessage] = useState<string | null>(() => describeRecovered(previewAssetState));
  const activeAssetVersionID = useRef<string | null>(null);
  const busy = ["PREPARING", "UPLOADING", "ATTACHING", "PROCESSING"].includes(phase);

  useEffect(() => {
    if (activeAssetVersionID.current === previewAssetVersionID) return;
    setPhase(recoverMediaPhase(previewAssetVersionID, previewAssetState));
    setMessage(describeRecovered(previewAssetState));
    // describeRecovered closes over the same inputs this effect already tracks.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [previewAssetVersionID, previewAssetState, locale, media]);

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
      activeAssetVersionID.current = ticket.asset_version_id;

      // Hashed before the PUT, so the completion evidence describes the bytes
      // this browser actually sent.
      const sha256 = await sha256Hex(file);

      setPhase("UPLOADING");
      const stored = await uploadFileToStorage(ticket.upload_url, file, file.type, setProgress);

      setPhase("ATTACHING");
      const completion = await completeAndSelectPublicPreview({
        courseID,
        revisionID,
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
      await onChanged();
      if (!completion.selected) {
        // A newer completed upload already holds the revision. This upload is
        // safely stored and simply not the winner.
        activeAssetVersionID.current = null;
        setPhase("IDLE");
        setMessage(t.superseded);
        return;
      }

      setPhase("PROCESSING");
      const status = await waitForProcessing(ticket.asset_version_id, locale);
      if (!isReadyState(status.state)) {
        activeAssetVersionID.current = null;
        setPhase("FAILED");
        setMessage(describeAssetState(status.state, locale));
        await onChanged();
        return;
      }

      activeAssetVersionID.current = null;
      setPhase("READY");
      setMessage(t.ready);
      await onChanged();
    } catch (cause) {
      activeAssetVersionID.current = null;
      if (cause instanceof ProcessingObservationTimeoutError) {
        // The upload succeeded and the revision already points at it. Only the
        // watching stopped.
        setPhase("PROCESSING_BACKGROUND");
        setMessage(t.processingBackground);
        return;
      }
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
      activeAssetVersionID.current = null;
      await onChanged();
      setPhase("IDLE");
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
        data-preview-media-state={previewAssetState ?? ""}
        className="mt-3 text-sm font-semibold text-foreground"
      >
        {hasPreview ? t.selected : t.absent}
      </p>
      <input
        ref={input}
        type="file"
        accept={ACCEPTED_VIDEO_CONTENT_TYPES.join(",")}
        // Visually hidden and driven by the button below it, so it carries its own name.
        aria-label={hasPreview ? t.replace : t.choose}
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
