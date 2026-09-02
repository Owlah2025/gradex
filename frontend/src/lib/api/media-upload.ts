import { authenticatedRequest } from "./http";
import { ProblemError } from "./problem";

/**
 * Browser side of the existing direct-to-storage media upload contract.
 *
 * The bytes never pass through the Go API. The API issues an expiry-bounded
 * upload intent, the browser PUTs the file straight to private object storage,
 * and the API then verifies the stored object itself — size, content
 * signature, and a hash it computes over the exact stored version — before the
 * asset enters quarantine and the worker picks it up.
 *
 * Nothing here holds or sees storage credentials: the presigned URL is the
 * only capability the browser receives, and it expires.
 */

/** Content types this control offers. The server re-validates independently. */
export const ACCEPTED_VIDEO_CONTENT_TYPES = ["video/mp4"] as const;

/**
 * The Lesson Resource types the launch profile accepts: PDF and DOCX.
 *
 * The server decides this, not the browser. It re-reads the stored object and
 * proves the real format — for DOCX it parses the whole OOXML package and
 * rejects a renamed archive or a macro-bearing document — so this list only
 * spares the Instructor an upload that would certainly be refused.
 */
export const ACCEPTED_RESOURCE_CONTENT_TYPES = [
  "application/pdf",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
] as const;

/** File extensions offered alongside the media types, for browsers that report
 *  an empty or generic type for a .docx picked from disk. */
export const ACCEPTED_RESOURCE_EXTENSIONS = [".pdf", ".docx"] as const;

/** Per-file ceiling for a Lesson Resource (D-011: 50 MB). The API enforces it. */
export const MAX_RESOURCE_UPLOAD_BYTES = 50 * 1024 * 1024;

/**
 * Client-side ceiling for a single browser upload.
 *
 * This is a usability bound, not the security bound: the API enforces
 * MAX_UPLOAD_SIZE_BYTES on the intent and re-checks the stored object's actual
 * size at completion. It exists because the browser hashes the file in memory
 * to produce completion evidence.
 */
export const MAX_BROWSER_UPLOAD_BYTES = 2 * 1024 * 1024 * 1024;

export type UploadTicket = {
  asset_version_id: string;
  upload_url: string;
  storage_object_key: string;
  expires_at: string;
};

export type CompletionResult = {
  asset_version_id: string;
  state: string;
  duplicate?: boolean;
};

export type MediaAssetStatus = {
  asset_version_id: string;
  logical_asset_id: string;
  kind: string;
  state: string;
  size_bytes: number;
  trusted_duration_ms?: number | null;
  created_at: string;
  deliverable: boolean;
};

export type LocalisedInput = { locale: "ar" | "en"; csrf: string };

/**
 * States the pipeline will not leave on its own; polling stops at these.
 *
 * These are the API's own failure states, spelled exactly as it reports them —
 * `PROCESS_FAILED`, not `PROCESSING_FAILED`. A mismatch here would leave a
 * failed upload spinning until the poll timed out instead of showing the
 * Instructor what actually went wrong.
 *
 * `VALIDATED` is deliberately absent: it is a real state a D-088 video passes
 * through on its way to PROCESSING, so polling must continue past it.
 */
const TERMINAL_STATES = new Set(["READY", "SCAN_FAILED", "SCAN_ERROR", "PROCESS_FAILED"]);

/** Locale-aware explanation for a state the pipeline stopped at. */
export function describeAssetState(state: string, locale: "ar" | "en"): string {
  const isAr = locale === "ar";
  switch (state) {
    case "SCAN_FAILED":
      return isAr
        ? "رفض الفحص هذا الملف. اختر ملفًا آخر."
        : "The file was rejected by content checks. Choose a different file.";
    case "SCAN_ERROR":
      return isAr
        ? "تعذر إكمال فحص الملف. حاول مرة أخرى لاحقًا."
        : "The file could not be checked. Try again later.";
    case "PROCESS_FAILED":
      return isAr
        ? "تعذر تجهيز الملف للعرض. حاول رفعه مرة أخرى."
        : "The file could not be prepared for playback. Try uploading it again.";
    default:
      return isAr ? `حالة الملف: ${state}` : `File state: ${state}`;
  }
}

export function isReadyState(state: string): boolean {
  return state === "READY";
}

export function isTerminalState(state: string): boolean {
  return TERMINAL_STATES.has(state);
}

/**
 * Rejects a file the server would certainly refuse, before any request is
 * made. It deliberately duplicates only the cheap, obvious checks; it is not a
 * substitute for the server's content-signature and hash verification.
 */
export function validateSelectedVideo(file: File, locale: "ar" | "en"): string | null {
  const isAr = locale === "ar";
  if (!(ACCEPTED_VIDEO_CONTENT_TYPES as readonly string[]).includes(file.type)) {
    return isAr ? "يجب اختيار ملف MP4." : "Select an MP4 video file.";
  }
  if (file.size <= 0) {
    return isAr ? "الملف فارغ." : "The selected file is empty.";
  }
  if (file.size > MAX_BROWSER_UPLOAD_BYTES) {
    return isAr
      ? "حجم الملف يتجاوز الحد المسموح به للرفع من المتصفح."
      : "The file exceeds the maximum size this browser upload supports.";
  }
  return null;
}

export type AssetKind = "VIDEO" | "RESOURCE" | "PREVIEW";

/**
 * Resolves the content type to declare for a picked file.
 *
 * Browsers are inconsistent about .docx: some report the OOXML type, some
 * report a generic ZIP type, and some report nothing at all. The extension is
 * used only to pick which *allowed* type to declare — it is never treated as
 * proof of format, because the server independently parses the stored bytes.
 */
export function resourceContentType(file: File): string | null {
  const declared = (file.type || "").toLowerCase();
  if ((ACCEPTED_RESOURCE_CONTENT_TYPES as readonly string[]).includes(declared)) {
    return declared;
  }
  const name = file.name.toLowerCase();
  if (name.endsWith(".pdf")) return "application/pdf";
  if (name.endsWith(".docx")) {
    return "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
  }
  return null;
}

/**
 * Rejects a Lesson Resource the server would certainly refuse. As with video,
 * this is convenience only: the API re-checks size, real format, and checksum
 * against the exact stored object.
 */
export function validateSelectedResource(
  file: File,
  locale: "ar" | "en",
): { contentType: string } | { error: string } {
  const isAr = locale === "ar";
  const contentType = resourceContentType(file);
  if (!contentType) {
    return { error: isAr ? "يجب اختيار ملف PDF أو DOCX." : "Select a PDF or DOCX file." };
  }
  if (file.size <= 0) {
    return { error: isAr ? "الملف فارغ." : "The selected file is empty." };
  }
  if (file.size > MAX_RESOURCE_UPLOAD_BYTES) {
    return {
      error: isAr
        ? "حجم الملف يتجاوز الحد المسموح به للمرفقات (50 ميغابايت)."
        : "The file exceeds the 50 MB limit for Lesson Resources.",
    };
  }
  return { contentType };
}

export async function beginUpload(
  input: LocalisedInput & {
    courseID: string;
    lessonID?: string;
    revisionID?: string;
    kind: AssetKind;
    contentType: string;
    sizeBytes: number;
  },
): Promise<UploadTicket> {
  const ticket = await authenticatedRequest<UploadTicket>(
    "/media/uploads",
    "POST",
    input.locale,
    input.csrf,
    {
      course_id: input.courseID,
      lesson_id: input.lessonID,
	  revision_id: input.revisionID,
      kind: input.kind,
      content_type: input.contentType,
      size_bytes: input.sizeBytes,
    },
  );
  if (ticket === null) {
    throw new Error(
      input.locale === "ar" ? "لم يصدر الخادم تصريح رفع" : "The server issued no upload ticket",
    );
  }
  return ticket;
}

export async function beginVideoUpload(
  input: LocalisedInput & {
    courseID: string;
    lessonID: string;
    contentType: string;
    sizeBytes: number;
  },
): Promise<UploadTicket> {
  return beginUpload({ ...input, kind: "VIDEO" });
}

export async function beginResourceUpload(
  input: LocalisedInput & {
    courseID: string;
    lessonID: string;
    contentType: string;
    sizeBytes: number;
  },
): Promise<UploadTicket> {
  return beginUpload({ ...input, kind: "RESOURCE" });
}

/** Begins a separate scanner-gated public-preview upload for one editable revision. */
export async function beginPublicPreviewUpload(
  input: LocalisedInput & {
    courseID: string;
    revisionID: string;
    contentType: string;
    sizeBytes: number;
  },
): Promise<UploadTicket> {
  return beginUpload({ ...input, kind: "PREVIEW" });
}

export type DirectUploadResult = { storageObjectVersion: string };

function isStrongQuotedETag(etag: string): boolean {
  if (etag.length < 3 || etag[0] !== '"' || etag[etag.length - 1] !== '"') {
    return false;
  }
  for (const character of etag.slice(1, -1)) {
    const codePoint = character.charCodeAt(0);
    if (codePoint <= 0x20 || codePoint === 0x7f || character === '"') {
      return false;
    }
  }
  return true;
}

function isCloudflareR2UploadURL(uploadURL: string): boolean {
  try {
    return new URL(uploadURL).hostname.endsWith(".r2.cloudflarestorage.com");
  } catch {
    return false;
  }
}

/** Chooses the immutable provider identity returned by a direct browser PUT. */
export function storageObjectVersionFromUploadHeaders(
  versionIDHeader: string | null,
  etagHeader: string | null,
  preferETag = false,
): string {
  const versionID = (versionIDHeader || "").trim();
  const etag = (etagHeader || "").trim();

  if (preferETag) {
    if (!isStrongQuotedETag(etag)) {
      throw new Error(
        "The storage provider did not return the strong ETag required for immutable R2 object identity.",
      );
    }
    return `etag:${etag}`;
  }

  if (versionID) {
    return versionID;
  }

  if (!isStrongQuotedETag(etag)) {
    throw new Error(
      "The storage provider did not return a usable object identity (x-amz-version-id or a strong ETag).",
    );
  }
  return `etag:${etag}`;
}

/**
 * PUTs the file to the presigned storage URL.
 *
 * XMLHttpRequest rather than fetch, because the upload needs byte progress and
 * the response's provider identity headers. The request carries no cookies and
 * no CSRF token: it is a cross-origin call to storage authorized solely by the
 * signature already in the URL.
 */
export function uploadFileToStorage(
  uploadURL: string,
  file: File,
  contentType: string,
  onProgress?: (fraction: number) => void,
): Promise<DirectUploadResult> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("PUT", uploadURL, true);
    request.withCredentials = false;
    request.setRequestHeader("Content-Type", contentType);

    request.upload.onprogress = (event) => {
      if (onProgress && event.lengthComputable && event.total > 0) {
        onProgress(event.loaded / event.total);
      }
    };
    request.onerror = () => reject(new Error("The storage upload could not be completed."));
    request.onabort = () => reject(new Error("The storage upload was cancelled."));
    request.onload = () => {
      if (request.status < 200 || request.status >= 300) {
        reject(new Error(`The storage upload was rejected (HTTP ${request.status}).`));
        return;
      }
      try {
        const storageObjectVersion = storageObjectVersionFromUploadHeaders(
          request.getResponseHeader("x-amz-version-id"),
          request.getResponseHeader("etag"),
          isCloudflareR2UploadURL(uploadURL),
        );
        resolve({ storageObjectVersion });
      } catch (error) {
        reject(error);
        return;
      }
    };
    request.send(file);
  });
}

/** SHA-256 over the file's bytes, as lowercase hex. */
export async function sha256Hex(file: File): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
  return Array.from(new Uint8Array(digest))
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

export async function completeUpload(
  input: LocalisedInput & {
    assetVersionID: string;
    providerEventID: string;
    storageObjectKey: string;
    storageObjectVersion: string;
    contentType: string;
    sizeBytes: number;
    sha256: string;
  },
): Promise<CompletionResult> {
  const result = await authenticatedRequest<CompletionResult>(
    `/media/uploads/${encodeURIComponent(input.assetVersionID)}/completions`,
    "POST",
    input.locale,
    input.csrf,
    {
      provider_event_id: input.providerEventID,
      storage_object_key: input.storageObjectKey,
      storage_object_version: input.storageObjectVersion,
      content_type: input.contentType,
      size_bytes: input.sizeBytes,
      sha256_hex: input.sha256,
    },
  );
  if (result === null) {
    throw new Error(
      input.locale === "ar"
        ? "لم يؤكد الخادم اكتمال الرفع"
        : "The server did not confirm the upload completion",
    );
  }
  return result;
}

export type LessonVideoCompletionResult = CompletionResult & { selected: boolean };

/**
 * Completes an exact Lesson-video upload and durably selects it in one
 * idempotent browser operation. Retrying transport or server failures reuses
 * identical completion evidence, including the provider event identifier.
 */
export async function completeAndSelectLessonVideo(
  input: LocalisedInput & {
    courseID: string;
    revisionID: string;
    lessonID: string;
    assetVersionID: string;
    providerEventID: string;
    storageObjectKey: string;
    storageObjectVersion: string;
    contentType: string;
    sizeBytes: number;
    sha256: string;
  },
): Promise<LessonVideoCompletionResult> {
  const route = `/courses/${encodeURIComponent(input.courseID)}/revisions/${encodeURIComponent(input.revisionID)}/lessons/${encodeURIComponent(input.lessonID)}/video/upload-completions`;
  const body = {
    asset_version_id: input.assetVersionID,
    provider_event_id: input.providerEventID,
    storage_object_key: input.storageObjectKey,
    storage_object_version: input.storageObjectVersion,
    content_type: input.contentType,
    size_bytes: input.sizeBytes,
    sha256_hex: input.sha256,
  };
  const send = async () => {
    const result = await authenticatedRequest<LessonVideoCompletionResult>(
      route,
      "POST",
      input.locale,
      input.csrf,
      body,
    );
    if (result === null) {
      throw new Error(
        input.locale === "ar"
          ? "لم يؤكد الخادم اكتمال رفع الفيديو واختياره"
          : "The server did not confirm the video upload and selection",
      );
    }
    return result;
  };
  try {
    return await send();
  } catch (error) {
    if (error instanceof ProblemError && error.problem.status < 500) throw error;
  }
  return send();
}

export async function getMediaAssetStatus(
  assetVersionID: string,
  locale: "ar" | "en",
): Promise<MediaAssetStatus> {
  const status = await authenticatedRequest<MediaAssetStatus>(
    `/media/assets/${encodeURIComponent(assetVersionID)}`,
    "GET",
    locale,
  );
  if (status === null) {
    throw new Error(locale === "ar" ? "تعذر قراءة حالة الوسائط" : "Unable to read the media status");
  }
  return status;
}

export type ProcessingPollOptions = {
  intervalMs?: number;
  timeoutMs?: number;
  onState?: (state: string) => void;
  signal?: AbortSignal;
};

export class ProcessingObservationTimeoutError extends Error {
  readonly status: MediaAssetStatus;

  constructor(status: MediaAssetStatus, locale: "ar" | "en") {
    super(
      locale === "ar"
        ? "انتهت مهلة معالجة الفيديو. حالة الأصل لا تزال قيد المعالجة."
        : "Timed out waiting for video processing. The asset is still being processed.",
    );
    this.name = "ProcessingObservationTimeoutError";
    this.status = status;
  }
}

/**
 * Polls the media status route until the Asset Version reaches a terminal
 * state or the bound elapses. Bounded on purpose: launch does not add a
 * WebSocket, and an unbounded poll would outlive the page that started it.
 */
export async function waitForProcessing(
  assetVersionID: string,
  locale: "ar" | "en",
  options: ProcessingPollOptions = {},
): Promise<MediaAssetStatus> {
  const intervalMs = options.intervalMs ?? 3000;
  const timeoutMs = options.timeoutMs ?? 10 * 60 * 1000;
  const deadline = Date.now() + timeoutMs;

  for (;;) {
    if (options.signal?.aborted) {
      throw new Error(locale === "ar" ? "تم إيقاف المتابعة" : "Processing was no longer being watched");
    }
    const status = await getMediaAssetStatus(assetVersionID, locale);
    options.onState?.(status.state);
    if (isTerminalState(status.state)) return status;
    if (Date.now() + intervalMs > deadline) {
      throw new ProcessingObservationTimeoutError(status, locale);
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
}

/** One provider event identifier per completion attempt. */
export function newProviderEventID(): string {
  return crypto.randomUUID();
}
