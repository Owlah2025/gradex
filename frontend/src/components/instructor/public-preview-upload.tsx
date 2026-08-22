"use client";

import { useRef, useState } from "react";
import { clearPublicPreview, setPublicPreview } from "@/lib/api/authoring";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
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

const copy = {
  en: {
    title: "Public preview",
    description:
      "Upload a separate short video for this revision. It is not a Lesson and stays private until an administrator approves this revision.",
    selected: "A public preview is selected for this revision.",
    absent: "No public preview is selected for this revision.",
    choose: "Upload public preview",
    replace: "Replace public preview",
    remove: "Remove public preview",
    processing: "Preparing your public preview…",
    upload: "Uploading public preview…",
    ready: "Public preview is ready for review.",
    removed: "The public preview was removed from this revision.",
    failed: "The public preview could not be updated. Try again.",
    csrf: "Session CSRF token is missing",
  },
  ar: {
    title: "المعاينة العامة",
    description:
      "ارفع فيديو قصيراً منفصلاً لهذه المراجعة. لا يُعد درساً ويبقى خاصاً حتى يعتمد المشرف هذه المراجعة.",
    selected: "تم اختيار معاينة عامة لهذه المراجعة.",
    absent: "لا توجد معاينة عامة مختارة لهذه المراجعة.",
    choose: "رفع معاينة عامة",
    replace: "استبدال المعاينة العامة",
    remove: "إزالة المعاينة العامة",
    processing: "جارٍ تجهيز المعاينة العامة…",
    upload: "جارٍ رفع المعاينة العامة…",
    ready: "أصبحت المعاينة العامة جاهزة للمراجعة.",
    removed: "أُزيلت المعاينة العامة من هذه المراجعة.",
    failed: "تعذّر تحديث المعاينة العامة. حاول مرة أخرى.",
    csrf: "رمز CSRF للجلسة مفقود",
  },
} as const;

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
  const t = copy[locale];
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
      setMessage(t.csrf);
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
      setMessage(t.csrf);
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
      className="rounded-md border border-teal-200 bg-teal-50/50 p-4 dark:border-teal-900/70 dark:bg-teal-950/20"
      aria-labelledby="public-preview-title"
    >
      <h3 id="public-preview-title" className="text-sm font-semibold">
        {t.title}
      </h3>
      <p className="mt-1 text-xs text-slate-600 dark:text-slate-300">{t.description}</p>
      <p data-testid="public-preview-state" className="mt-2 text-xs font-medium">
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
      <div className="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          disabled={busy}
          onClick={() => input.current?.click()}
          data-testid="upload-public-preview"
          className="rounded-md bg-teal-700 px-3 py-2 text-xs font-medium text-white hover:bg-teal-800 disabled:opacity-50"
        >
          {hasPreview ? t.replace : t.choose}
        </button>
        {hasPreview ? (
          <button
            type="button"
            disabled={busy}
            onClick={() => void remove()}
            data-testid="remove-public-preview"
            className="rounded-md border border-slate-300 px-3 py-2 text-xs font-medium hover:bg-white disabled:opacity-50 dark:border-slate-700 dark:hover:bg-slate-900"
          >
            {t.remove}
          </button>
        ) : null}
      </div>
      {status ? (
        <p role={phase === "FAILED" ? "alert" : "status"} className="mt-3 text-xs" data-testid="public-preview-message">
          {status}
        </p>
      ) : null}
    </section>
  );
}
