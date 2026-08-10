import { authenticatedRequest } from "./http";

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

/** States the pipeline will not leave on its own; polling stops at these. */
const TERMINAL_STATES = new Set(["READY", "SCAN_FAILED", "PROCESSING_FAILED", "REJECTED", "FAILED"]);

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

export async function beginVideoUpload(
  input: LocalisedInput & {
    courseID: string;
    lessonID: string;
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
      kind: "VIDEO",
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

export type DirectUploadResult = { storageObjectVersion: string };

/**
 * PUTs the file to the presigned storage URL.
 *
 * XMLHttpRequest rather than fetch, because the upload needs byte progress and
 * the response's `x-amz-version-id` header. The request carries no cookies and
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
      const version = (request.getResponseHeader("x-amz-version-id") || "").trim();
      if (!version) {
        // Provenance is version-exact by design: without the stored object's
        // version there is nothing to prove the API later scanned and
        // processed these exact bytes.
        reject(
          new Error(
            "The storage provider did not return an object version. Object versioning must be enabled and x-amz-version-id exposed to the browser.",
          ),
        );
        return;
      }
      resolve({ storageObjectVersion: version });
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

export async function completeVideoUpload(
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
      throw new Error(
        locale === "ar"
          ? "انتهت مهلة معالجة الفيديو. حالة الأصل لا تزال قيد المعالجة."
          : "Timed out waiting for video processing. The asset is still being processed.",
      );
    }
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }
}

/** One provider event identifier per completion attempt. */
export function newProviderEventID(): string {
  return crypto.randomUUID();
}
