"use client";

import { useId, useRef, useState } from "react";
import { ImagePlus, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/lib/i18n/locale-provider";
import { currentCSRFToken } from "@/lib/identity/session";
import { revisionThumbnailURL, setCourseThumbnail, THUMBNAIL_TYPES, uploadCourseThumbnail, validateThumbnailFile } from "@/lib/api/course-thumbnail";
import { ThumbnailImage } from "@/components/catalog/thumbnail-image";

export function CourseThumbnailUpload({ courseID, revisionID, assetID, disabled = false, unresolved = false, onChanged, onBlockedChange }: {
  courseID: string; revisionID: string; assetID?: string | null; disabled?: boolean; unresolved?: boolean;
  onChanged: () => Promise<void>; onBlockedChange: (blocked: boolean) => void;
}) {
  const { locale, t: dictionary } = useLocale();
  const t = dictionary.courseThumbnail;
  const input = useRef<HTMLInputElement>(null);
  const lock = useRef(false);
  const id = useId();
  const [phase, setPhase] = useState<"idle" | "uploading" | "processing" | "ready" | "failed">(unresolved ? "failed" : "idle");
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState(unresolved ? t.failed : "");
  const [previewAttempt, setPreviewAttempt] = useState(0);
  const [dragging, setDragging] = useState(false);
  const busy = phase === "uploading" || phase === "processing";

  async function run(action: (csrf: string) => Promise<void>) {
    if (lock.current || disabled) return;
    lock.current = true;
    onBlockedChange(true);
    setError("");
    setPhase("processing");
    try {
      const csrf = currentCSRFToken();
      if (!csrf) throw new Error("session unavailable");
      await action(csrf);
      await onChanged();
      setPreviewAttempt((attempt) => attempt + 1);
      setPhase("ready");
      onBlockedChange(false);
    } catch {
      setError(t.failed);
      setPhase("failed");
    } finally { lock.current = false; }
  }

  function upload(file: File) {
    if (lock.current || disabled) return;
    const rejected = validateThumbnailFile(file);
    if (rejected) { setError(rejected === "type" ? t.invalidType : t.invalidSize); return; }
    void run(async (csrf) => {
      setPhase("uploading"); setProgress(0);
      await uploadCourseThumbnail({ courseID, revisionID, expectedAssetID: assetID ?? null, file, locale, csrf,
        onProgress: setProgress, onProcessing: () => setPhase("processing") });
    });
  }

  return <section data-testid="course-thumbnail-authoring" className="space-y-3 rounded-lg border border-border bg-card p-4" aria-labelledby={`${id}-title`}>
    <h3 id={`${id}-title`} className="font-display text-base font-bold">{t.title}</h3>
    <p id={`${id}-guidance`} className="text-sm leading-6 text-muted-foreground">{t.guidance}</p>
    <div className={`w-full max-w-[340px] overflow-hidden rounded-lg border ${dragging ? "border-primary bg-primary/5" : "border-border bg-muted/30"}`}
      onDragOver={(event) => { event.preventDefault(); if (!busy && !disabled) setDragging(true); }}
      onDragLeave={() => setDragging(false)}
      onDrop={(event) => { event.preventDefault(); setDragging(false); if (event.dataTransfer.files.length === 1) upload(event.dataTransfer.files[0]); else setError(t.oneFile); }}>
      <div className="relative flex h-[168px] items-center justify-center overflow-hidden sm:h-[180px]" data-testid="thumbnail-card-crop">
        <div className="flex flex-col items-center gap-2 p-4 text-center text-sm text-muted-foreground"><ImagePlus aria-hidden className="size-7" />{assetID ? t.preview : t.empty}</div>
        {assetID ? <ThumbnailImage key={`${assetID}-${previewAttempt}`} src={revisionThumbnailURL(courseID, revisionID, assetID)} className="absolute inset-0 size-full object-cover" onError={() => setError(t.imageUnavailable)} /> : null}
      </div>
    </div>
    <input id={`${id}-input`} ref={input} type="file" accept={THUMBNAIL_TYPES.join(",")} className="hidden" tabIndex={-1}
      aria-label={t.upload} aria-describedby={`${id}-guidance`} disabled={busy || disabled}
      onChange={(event) => { const file = event.target.files?.[0]; event.currentTarget.value = ""; if (file) upload(file); }} />
    <div className="flex flex-wrap gap-2">
      <Button type="button" size="sm" disabled={busy || disabled} onClick={() => input.current?.click()}><Upload aria-hidden className="size-4" />{assetID ? t.replace : t.upload}</Button>
      {assetID ? <Button type="button" size="sm" variant="ghost" disabled={busy || disabled} onClick={() => void run(async (csrf) => { await setCourseThumbnail({ courseID, revisionID, assetID: null, expectedAssetID: assetID, locale, csrf }); })}>{t.remove}</Button> : null}
      {error && !busy ? <Button type="button" size="sm" variant="outline" onClick={() => void run(async () => {})}>{t.reload}</Button> : null}
    </div>
    {busy ? <p role="status" className="text-sm text-muted-foreground">{phase === "uploading" ? `${t.uploading} ${Math.round(progress * 100)}%` : t.processing}</p> : null}
    {phase === "ready" && !error ? <p role="status" className="text-sm text-muted-foreground">{t.saved}</p> : null}
    {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
    <p className="text-xs leading-5 text-muted-foreground">{t.approvalNote}</p>
  </section>;
}
