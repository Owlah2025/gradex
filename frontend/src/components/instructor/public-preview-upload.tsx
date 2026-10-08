"use client";

import { useEffect, useRef, useState } from "react";
import { ResumableUploadControls, useResumableUpload, isPausedUpload } from "./resumable-upload-controls";
import { clearPublicPreview } from "@/lib/api/authoring";
import { describeApiError } from "@/lib/api/api-error";
import { describeUploadError } from "./resumable-upload-copy";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import {
  ACCEPTED_VIDEO_CONTENT_TYPES,
  completeAndSelectPublicPreview,
  describeAssetState,
  isReadyState,
  validateSelectedVideo,
} from "@/lib/api/media-upload";
import { recoverMediaPhase } from "./media-upload-phase";
import { useProcessingWatch } from "./use-processing-watch";

type Phase =
  | "IDLE"
  | "PREPARING"
  | "UPLOADING"
  | "VERIFYING"
  | "ATTACHING"
  | "PROCESSING"
  | "PROCESSING_BACKGROUND"
  | "READY"
  | "FAILED";

type PublicPreviewUploadProps = {
  courseID: string;
  revisionID: string;
  hasPreview: boolean;
  /**
   * Render as the retained compatibility surface rather than as the way to
   * create a preview.
   *
   * Public preview is a Lesson permission now. This control is kept so a course
   * that already has a separate course-level preview can still manage or remove
   * it, and it is only mounted for such a course — so `legacy` is not a styling
   * variant, it is what this surface has become.
   */
  legacy?: boolean;
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
  legacy = false,
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
  const busy = ["PREPARING", "UPLOADING", "VERIFYING", "ATTACHING", "PROCESSING"].includes(phase);
  const resumable = useResumableUpload({ courseID, revisionID, kind: "PREVIEW", storageKeyId: "preview-" + courseID, locale });
  // Every byte is stored; the server is verifying, so no upload percentage is shown any more.
  useEffect(() => {
    if (resumable.verifying) setPhase("VERIFYING");
  }, [resumable.verifying]);

  /*
    The same single watch the Lesson video uses, for the same reason: a trusted
    preview goes through FFmpeg, which outlasts the tab. Keyed on the asset the
    server says is selected, so a reload resumes the real run and its measured
    progress instead of starting from nothing.
  */
  const processing = useProcessingWatch({
    assetVersionID: previewAssetVersionID,
    active: phase === "PROCESSING" || phase === "PROCESSING_BACKGROUND",
    locale,
    onSettled: (settledStatus) => {
      activeAssetVersionID.current = null;
      if (isReadyState(settledStatus.state)) {
        setPhase("READY");
        setMessage(t.ready);
      } else {
        setPhase("FAILED");
        setMessage(describeAssetState(settledStatus.state, locale));
      }
      void onChanged();
    },
  });

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
      const completionResult = await resumable.run(file, (fraction) => { setPhase("UPLOADING"); setProgress(fraction); });
      activeAssetVersionID.current = completionResult.asset_version_id;


      setPhase("ATTACHING");
      const completion = await completeAndSelectPublicPreview({
        courseID,
        revisionID,
        assetVersionID: completionResult.asset_version_id,
        providerEventID: completionResult.provider_event_id,
        storageObjectKey: completionResult.storage_object_key,
        storageObjectVersion: completionResult.storage_object_version,
        contentType: file.type,
        sizeBytes: file.size,
        sha256: completionResult.sha256_hex,
        locale,
        csrf,
      });
      resumable.acknowledge(completionResult.sha256_hex);
      await onChanged();
      if (!completion.selected) {
        // A newer completed upload already holds the revision. This upload is
        // safely stored and simply not the winner.
        activeAssetVersionID.current = null;
        setPhase("IDLE");
        setMessage(t.superseded);
        return;
      }

      // The upload is stored and the revision already points at it; the worker
      // owns the rest. The watch above reports it, here and after a reload.
      setPhase("PROCESSING");
      setMessage(t.processingBackground);
    } catch (cause) {
      if (isPausedUpload(cause)) { setPhase("IDLE"); setMessage(null); return; }
      activeAssetVersionID.current = null;
      setPhase("FAILED");
      setMessage(describeUploadError(cause, locale) || t.failed);
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

  // The percentage shown is always one somebody measured: the browser's byte
  // count while uploading, the worker's own account while processing, and no
  // number at all in between.
  const status =
    phase === "VERIFYING"
      ? media.phase.VERIFYING
      : phase === "PREPARING" || phase === "ATTACHING"
      ? t.processing
      : phase === "UPLOADING"
        ? `${t.upload} ${Math.round(progress * 100)}%`
        : phase === "PROCESSING" || phase === "PROCESSING_BACKGROUND"
          ? processing
            ? `${t.processing} ${processing.percent}%`
            : t.processing
          : message;

  return (
    <section
      data-testid="public-preview-authoring"
      data-legacy={legacy ? "true" : "false"}
      className="rounded-lg border border-border bg-card p-4"
      aria-labelledby="public-preview-title"
    >
      <h3 id="public-preview-title" className="font-display text-base font-bold text-foreground">
        {legacy ? t.legacyTitle : t.title}
      </h3>
      {/* The legacy wording is explicit rather than tactful. An Instructor who
          sees "Public preview" beside a lesson-preview panel has to guess which
          one is current; "Legacy course preview", plus a line saying new previews
          belong on lessons, does not require guessing. */}
      <p className="mt-1 text-sm leading-6 text-muted-foreground">
        {legacy ? t.legacyDescription : t.description}
      </p>
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
      <ResumableUploadControls upload={resumable} locale={locale} locked={busy && !resumable.running} onReselect={() => {
          const file = resumable.lastFile();
          if (file) void upload(file);
          else input.current?.click();
        }} />
      {status ? (
        /* A failure must not read like a success: different role, different ink. */
        <p
          role={phase === "FAILED" ? "alert" : "status"}
          data-testid="public-preview-message"
          data-upload-phase={phase}
          data-processing-stage={processing?.stage ?? undefined}
          data-processing-percent={processing ? String(processing.percent) : undefined}
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
