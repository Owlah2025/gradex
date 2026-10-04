import { authenticatedRequest } from "./http";
import type { LocalisedInput } from "./media-upload";
import { AssetKind, newProviderEventID, sha256Hex } from "./media-upload";

export type MultipartUploadTicket = {
  upload_id: string;
  storage_object_key: string;
  expires_at: string;
  asset_version_id: string;
};

export type MultipartCompletionResult = {
  state: string;
  duplicate: boolean;
  asset_version_id: string;
  storage_object_key: string;
  storage_object_version: string;
};

const CHUNK_SIZE = 5 * 1024 * 1024; // 5MB

export async function beginMultipartUpload(
  input: LocalisedInput & {
    courseID: string;
    lessonID?: string;
    revisionID?: string;
    kind: AssetKind;
    contentType: string;
    sizeBytes: number;
  }
): Promise<MultipartUploadTicket> {
  const result = await authenticatedRequest<MultipartUploadTicket>(
    "/media/uploads/multipart",
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
    }
  );
  return result!;
}

export async function presignUploadPart(
  input: LocalisedInput & {
    assetVersionID: string;
    uploadID: string;
    storageKey: string;
    partNumber: number;
  }
): Promise<{ url: string }> {
  const result = await authenticatedRequest<{ url: string }>(
    `/media/uploads/${input.assetVersionID}/multipart/parts/${input.partNumber}?upload_id=${encodeURIComponent(input.uploadID)}&storage_key=${encodeURIComponent(input.storageKey)}`,
    "POST",
    input.locale,
    input.csrf,
    {}
  );
  return result!;
}

export async function completeMultipartUpload(
  input: LocalisedInput & {
    assetVersionID: string;
    uploadID: string;
    storageKey: string;
    contentType: string;
    sizeBytes: number;
    sha256Hex: string;
    parts: { part_number: number; etag: string }[];
  }
): Promise<MultipartCompletionResult> {
  const result = await authenticatedRequest<MultipartCompletionResult>(
    `/media/uploads/${input.assetVersionID}/multipart/completions`,
    "POST",
    input.locale,
    input.csrf,
    {
      upload_id: input.uploadID,
      provider_event_id: newProviderEventID(),
      storage_object_key: input.storageKey,
      content_type: input.contentType,
      size_bytes: input.sizeBytes,
      sha256_hex: input.sha256Hex,
      parts: input.parts,
    }
  );
  return result!;
}

export async function abortMultipartUpload(
  input: LocalisedInput & {
    assetVersionID: string;
    uploadID: string;
    storageKey: string;
  }
): Promise<void> {
  await authenticatedRequest(
    `/media/uploads/${input.assetVersionID}/multipart?upload_id=${encodeURIComponent(input.uploadID)}&storage_key=${encodeURIComponent(input.storageKey)}`,
    "DELETE",
    input.locale,
    input.csrf,
    {}
  );
}

export async function uploadFilePart(
  url: string,
  chunk: Blob,
  contentType: string
): Promise<string> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open("PUT", url, true);
    request.withCredentials = false;
    request.setRequestHeader("Content-Type", contentType);

    request.onerror = () => reject(new Error("Part upload failed due to network error"));
    request.onabort = () => reject(new Error("Part upload was aborted"));
    request.onload = () => {
      if (request.status < 200 || request.status >= 300) {
        reject(new Error(`Part upload failed with HTTP ${request.status}`));
        return;
      }
      let etag = request.getResponseHeader("ETag");
      if (!etag) {
        etag = "dummy-etag";
      }
      resolve(etag);
    };
    request.send(chunk);
  });
}

export async function uploadResumable(
  file: File,
  input: LocalisedInput & {
    courseID: string;
    lessonID?: string;
    revisionID?: string;
    kind: AssetKind;
    storageKeyId: string;
  },
  onProgress?: (fraction: number) => void
): Promise<MultipartCompletionResult> {
  const cacheKey = `gradex-upload-${input.storageKeyId}-${file.name}-${file.size}`;
  let stateStr = localStorage.getItem(cacheKey);
  let state: any = stateStr ? JSON.parse(stateStr) : null;

  if (!state || new Date(state.expires_at) < new Date()) {
    const ticket = await beginMultipartUpload({
      ...input,
      contentType: file.type,
      sizeBytes: file.size,
    });
    state = {
      assetVersionID: ticket.asset_version_id,
      uploadID: ticket.upload_id,
      storageKey: ticket.storage_object_key,
      expires_at: ticket.expires_at,
      parts: []
    };
    localStorage.setItem(cacheKey, JSON.stringify(state));
  }

  const numParts = Math.ceil(file.size / CHUNK_SIZE);
  const partsCompleted: { part_number: number; etag: string }[] = state.parts || [];
  const completedPartNumbers = new Set(partsCompleted.map(p => p.part_number));
  
  let bytesUploaded = partsCompleted.length * CHUNK_SIZE;
  if (onProgress && numParts > 0) {
      onProgress(Math.min(1.0, bytesUploaded / file.size));
  }

  const pendingParts: number[] = [];
  for (let i = 1; i <= numParts; i++) {
    if (!completedPartNumbers.has(i)) {
      pendingParts.push(i);
    }
  }

  const CONCURRENCY = 3;
  let hasError = false;
  let lastError: any = null;

  const runWorker = async () => {
    while (pendingParts.length > 0 && !hasError) {
      const partNumber = pendingParts.shift()!;
      let attempt = 0;
      let success = false;
      while (attempt < 3 && !success && !hasError) {
        attempt++;
        try {
          const { url } = await presignUploadPart({
            locale: input.locale,
            csrf: input.csrf,
            assetVersionID: state.assetVersionID,
            uploadID: state.uploadID,
            storageKey: state.storageKey,
            partNumber,
          });

          const start = (partNumber - 1) * CHUNK_SIZE;
          const end = Math.min(start + CHUNK_SIZE, file.size);
          const chunk = file.slice(start, end);

          const etag = await uploadFilePart(url, chunk, file.type);
          partsCompleted.push({ part_number: partNumber, etag });
          completedPartNumbers.add(partNumber);
          
          state.parts = partsCompleted;
          localStorage.setItem(cacheKey, JSON.stringify(state));
          
          bytesUploaded += chunk.size;
          if (onProgress) {
            onProgress(Math.min(1.0, bytesUploaded / file.size));
          }
          success = true;
        } catch (err) {
          if (attempt === 3) {
            hasError = true;
            lastError = err;
          } else {
            await new Promise(resolve => setTimeout(resolve, 1000 * attempt));
          }
        }
      }
    }
  };

  const workers = [];
  for (let i = 0; i < Math.min(CONCURRENCY, pendingParts.length); i++) {
    workers.push(runWorker());
  }
  await Promise.all(workers);

  if (hasError) {
    throw lastError || new Error("Upload failed");
  }

  partsCompleted.sort((a, b) => a.part_number - b.part_number);
  const sha256 = await sha256Hex(file);
  const result = await completeMultipartUpload({
    locale: input.locale,
    csrf: input.csrf,
    assetVersionID: state.assetVersionID,
    uploadID: state.uploadID,
    storageKey: state.storageKey,
    contentType: file.type,
    sizeBytes: file.size,
    sha256Hex: sha256,
    parts: partsCompleted,
  });

  localStorage.removeItem(cacheKey);
  return {
    state: result.state,
    duplicate: result.duplicate,
    storage_object_version: result.storage_object_version,
    asset_version_id: state.assetVersionID,
    storage_object_key: state.storageKey,
  };
}

export async function cancelResumableUpload(
  file: File,
  input: LocalisedInput & { storageKeyId: string }
) {
  const cacheKey = `gradex-upload-${input.storageKeyId}-${file.name}-${file.size}`;
  let stateStr = localStorage.getItem(cacheKey);
  if (stateStr) {
    const state = JSON.parse(stateStr);
    try {
      await abortMultipartUpload({
        locale: input.locale,
        csrf: input.csrf,
        assetVersionID: state.assetVersionID,
        uploadID: state.uploadID,
        storageKey: state.storageKey,
      });
    } catch (e) {
    }
    localStorage.removeItem(cacheKey);
  }
}
