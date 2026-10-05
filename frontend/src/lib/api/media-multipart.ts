import { authenticatedRequest } from "./http";
import { ProblemError } from "./problem";
import type { AssetKind, LocalisedInput } from "./media-upload";
import { sha256Hex } from "./media-upload";

type Part = { part_number: number; etag: string; size_bytes?: number };
export type MultipartUploadTicket = {
  upload_id: string;
  storage_object_key: string;
  expires_at: string;
  asset_version_id: string;
  part_size_bytes: number;
  status: string;
  parts: Part[];
};
export type MultipartCompletionResult = {
  state: string;
  duplicate: boolean;
  asset_version_id: string;
  storage_object_key: string;
  storage_object_version: string;
  sha256_hex: string;
  provider_event_id: string;
};
export type ResumableInput = LocalisedInput & {
  courseID: string;
  lessonID?: string;
  revisionID?: string;
  kind: AssetKind;
  storageKeyId: string;
  contentType?: string;
  signal?: AbortSignal;
  onVerifying?: () => void;
};
type Checkpoint = {
  requestID: string;
  ticket: MultipartUploadTicket | null;
  parts: Part[];
};
const activeUploads = new Set<string>();
const prefix = (
  input: Pick<ResumableInput, "courseID" | "revisionID" | "storageKeyId">,
) =>
  `gradex-multipart-v2:${input.courseID}:${input.revisionID || ""}:${input.storageKeyId}:`;

export function hasResumableUpload(
  input: Pick<ResumableInput, "courseID" | "revisionID" | "storageKeyId">,
): boolean {
  return Object.keys(localStorage).some((key) => key.startsWith(prefix(input)));
}

function readCheckpoint(key: string): Checkpoint | null {
  const raw = localStorage.getItem(key);
  if (!raw) return null;
  try {
    const state: Checkpoint = JSON.parse(raw);
    if (!state.requestID || !Array.isArray(state.parts))
      throw new Error("Invalid upload checkpoint");
    return state;
  } catch (error) {
    if (!(error instanceof SyntaxError) && !(error instanceof Error))
      throw error;
    localStorage.removeItem(key);
    return null;
  }
}

export async function beginMultipartUpload(
  input: ResumableInput & {
    contentType: string;
    sizeBytes: number;
    requestID: string;
  },
): Promise<MultipartUploadTicket> {
  const ticket = await authenticatedRequest<MultipartUploadTicket>(
    "/media/uploads/multipart",
    "POST",
    input.locale,
    input.csrf,
    {
      client_request_id: input.requestID,
      course_id: input.courseID,
      lesson_id: input.lessonID,
      revision_id: input.kind === "PREVIEW" ? input.revisionID : undefined,
      kind: input.kind,
      content_type: input.contentType,
      size_bytes: input.sizeBytes,
    },
  );
  if (!ticket) throw new Error("Upload creation returned no session");
  return ticket;
}

export async function presignUploadPart(
  input: LocalisedInput & { assetVersionID: string; partNumber: number },
): Promise<{ url: string }> {
  const result = await authenticatedRequest<{ url: string }>(
    `/media/uploads/${encodeURIComponent(input.assetVersionID)}/multipart/parts/${input.partNumber}`,
    "POST",
    input.locale,
    input.csrf,
    {},
  );
  if (!result) throw new Error("Part signing returned no URL");
  return result;
}

async function getSession(
  input: LocalisedInput,
  id: string,
): Promise<MultipartUploadTicket> {
  const ticket = await authenticatedRequest<MultipartUploadTicket>(
    `/media/uploads/${encodeURIComponent(id)}/multipart`,
    "GET",
    input.locale,
  );
  if (!ticket) throw new Error("Upload recovery returned no session");
  return ticket;
}

export async function completeMultipartUpload(
  input: LocalisedInput & {
    ticket: MultipartUploadTicket;
    contentType: string;
    sizeBytes: number;
    sha256Hex: string;
    parts: Part[];
  },
): Promise<MultipartCompletionResult> {
  const result = await authenticatedRequest<MultipartCompletionResult>(
    `/media/uploads/${encodeURIComponent(input.ticket.asset_version_id)}/multipart/completions`,
    "POST",
    input.locale,
    input.csrf,
    {
      upload_id: input.ticket.upload_id,
      provider_event_id: `multipart:${encodeURIComponent(input.ticket.asset_version_id)}`,
      storage_object_key: input.ticket.storage_object_key,
      content_type: input.contentType,
      size_bytes: input.sizeBytes,
      sha256_hex: input.sha256Hex,
      parts: input.parts.map(({ part_number, etag }) => ({
        part_number,
        etag,
      })),
    },
  );
  if (!result?.storage_object_version)
    throw new Error("Completion returned no immutable object identity");
  return {
    ...result,
    storage_object_key: input.ticket.storage_object_key,
    sha256_hex: input.sha256Hex,
    provider_event_id: `multipart:${encodeURIComponent(input.ticket.asset_version_id)}`,
  };
}

export async function abortMultipartUpload(
  input: LocalisedInput & { assetVersionID: string },
): Promise<void> {
  await authenticatedRequest(
    `/media/uploads/${encodeURIComponent(input.assetVersionID)}/multipart`,
    "DELETE",
    input.locale,
    input.csrf,
  );
}

export function uploadFilePart(
  url: string,
  chunk: Blob,
  contentType: string,
  signal?: AbortSignal,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    const abort = () => request.abort();
    const fail = (error: Error) => {
      signal?.removeEventListener("abort", abort);
      reject(error);
    };
    if (signal?.aborted) {
      reject(new DOMException("Upload paused", "AbortError"));
      return;
    }
    request.open("PUT", url, true);
    request.withCredentials = false;
    request.timeout = 120000;
    request.setRequestHeader("Content-Type", contentType);
    signal?.addEventListener("abort", abort, { once: true });
    request.onerror = () =>
      fail(new Error("Part upload failed due to network error"));
    request.ontimeout = () => fail(new Error("Part upload timed out"));
    request.onabort = () =>
      fail(new DOMException("Upload paused", "AbortError"));
    request.onload = () => {
      if (request.status < 200 || request.status >= 300) {
        fail(new Error(`Part upload failed with HTTP ${request.status}`));
        return;
      }
      const etag = request.getResponseHeader("ETag");
      if (!etag || !/^"[^"\s]+"$/.test(etag)) {
        fail(new Error("Storage returned no valid part ETag"));
        return;
      }
      signal?.removeEventListener("abort", abort);
      resolve(etag);
    };
    request.send(chunk);
  });
}

function retryable(error: unknown): boolean {
  return (
    !(error instanceof DOMException && error.name === "AbortError") &&
    !(
      error instanceof ProblemError &&
      error.problem.status < 500 &&
      error.problem.status !== 429
    )
  );
}

async function uploadPartWithRetry(
  file: File,
  input: ResumableInput,
  ticket: MultipartUploadTicket,
  number: number,
): Promise<Part> {
  const start = (number - 1) * ticket.part_size_bytes;
  const chunk = file.slice(
    start,
    Math.min(start + ticket.part_size_bytes, file.size),
  );
  for (let attempt = 0; ; attempt++) {
    input.signal?.throwIfAborted();
    try {
      const { url } = await presignUploadPart({
        ...input,
        assetVersionID: ticket.asset_version_id,
        partNumber: number,
      });
      const etag = await uploadFilePart(
        url,
        chunk,
        input.contentType || file.type,
        input.signal,
      );
      return { part_number: number, etag, size_bytes: chunk.size };
    } catch (error) {
      if (attempt === 2 || !retryable(error)) throw error;
      await new Promise((resolve) => setTimeout(resolve, 500 * (attempt + 1)));
    }
  }
}

async function transferParts(
  file: File,
  input: ResumableInput,
  checkpoint: Checkpoint,
  key: string,
  onProgress?: (fraction: number) => void,
) {
  const ticket = checkpoint.ticket;
  if (!ticket) throw new Error("Upload session has not been created");
  const count = Math.ceil(file.size / ticket.part_size_bytes);
  const done = new Map(checkpoint.parts.map((p) => [p.part_number, p]));
  const pending = Array.from({ length: count }, (_, i) => i + 1).filter(
    (n) => !done.has(n),
  );
  const report = () =>
    onProgress?.(
      Array.from(done.keys()).reduce(
        (sum, n) =>
          sum +
          Math.min(
            ticket.part_size_bytes,
            file.size - (n - 1) * ticket.part_size_bytes,
          ),
        0,
      ) / file.size,
    );
  report();
  let failure: unknown;
  const worker = async () => {
    while (pending.length && !failure) {
      const number = pending.shift()!;
      try {
        const part = await uploadPartWithRetry(file, input, ticket, number);
        done.set(number, part);
        checkpoint.parts = Array.from(done.values()).sort(
          (a, b) => a.part_number - b.part_number,
        );
        localStorage.setItem(key, JSON.stringify(checkpoint));
        report();
      } catch (error) {
        failure = error;
      }
    }
  };
  // Build workers before any worker mutates the pending queue.
  await Promise.all(
    Array.from({ length: Math.min(3, pending.length) }, () => worker()),
  );
  if (failure) throw failure;
  input.signal?.throwIfAborted();
}

export async function uploadResumable(
  file: File,
  input: ResumableInput,
  onProgress?: (fraction: number) => void,
): Promise<MultipartCompletionResult> {
  // Full streaming fingerprint prevents splicing two same-name/same-size files after refresh.
  const digest = await sha256Hex(file, input.signal);
  const key = prefix(input) + digest;
  if (activeUploads.has(key)) throw new Error("This upload is already running");
  activeUploads.add(key);
  try {
    let checkpoint = readCheckpoint(key);
    if (checkpoint?.ticket) {
      const ticket = await getSession(
        input,
        checkpoint.ticket.asset_version_id,
      );
      checkpoint.ticket = ticket;
      if (ticket.status === "ACTIVE")
        checkpoint.parts = ticket.parts.sort(
          (a, b) => a.part_number - b.part_number,
        );
      if (ticket.status === "ABORTING" || ticket.status === "ABORTED")
        throw new Error(
          "This upload was cancelled. Clear it before starting again.",
        );
    } else {
      checkpoint = checkpoint || {
        requestID: crypto.randomUUID(),
        ticket: null,
        parts: [],
      };
      localStorage.setItem(key, JSON.stringify(checkpoint));
      checkpoint.ticket = await beginMultipartUpload({
        ...input,
        requestID: checkpoint.requestID,
        contentType: input.contentType || file.type,
        sizeBytes: file.size,
      });
      checkpoint.parts = checkpoint.ticket.parts;
    }
    localStorage.setItem(key, JSON.stringify(checkpoint));
    const ticket = checkpoint.ticket;
    if (!ticket) throw new Error("Upload session has not been created");
    if (ticket.status === "ACTIVE")
      await transferParts(file, input, checkpoint, key, onProgress);
    input.signal?.throwIfAborted();
    let result = await completeMultipartUpload({
      ...input,
      ticket,
      contentType: input.contentType || file.type,
      sizeBytes: file.size,
      sha256Hex: digest,
      parts: checkpoint.parts,
    });
    const deadline = Date.now() + 16 * 60 * 1000;
    if (result.state === "UPLOADED") input.onVerifying?.();
    while (result.state === "UPLOADED") {
      input.signal?.throwIfAborted();
      if (Date.now() > deadline)
        throw new Error(
          "Verification is still pending. Reselect the same file later to check again.",
        );
      await new Promise((resolve) => setTimeout(resolve, 1000));
      const status = await authenticatedRequest<MultipartCompletionResult>(
        `/media/uploads/${encodeURIComponent(ticket.asset_version_id)}/multipart/verification`,
        "GET",
        input.locale,
      );
      if (!status) throw new Error("Upload verification returned no status");
      result = { ...result, ...status };
    }
    // Keep the checkpoint until the separate course-selection operation is acknowledged.
    return result;
  } finally {
    activeUploads.delete(key);
  }
}

export function acknowledgeResumableUpload(
  input: ResumableInput,
  digest: string,
): void {
  localStorage.removeItem(prefix(input) + digest);
}

export async function cancelResumableUpload(
  input: ResumableInput,
): Promise<void> {
  for (const key of Object.keys(localStorage).filter((key) =>
    key.startsWith(prefix(input)),
  )) {
    const checkpoint = readCheckpoint(key);
    if (!checkpoint) continue;
    try {
      const identity =
        checkpoint.ticket ||
        (await authenticatedRequest<{ asset_version_id: string }>(
          `/media/uploads/multipart/requests/${encodeURIComponent(checkpoint.requestID)}`,
          "GET",
          input.locale,
        ));
      if (identity)
        await abortMultipartUpload({
          ...input,
          assetVersionID: identity.asset_version_id,
        });
    } catch (error) {
      // An already finalized upload is retained server-side for processing; only discard its browser checkpoint.
      if (
        !(
          error instanceof ProblemError &&
          (error.problem.status === 409 || error.problem.status === 404)
        )
      )
        throw error;
    }
    localStorage.removeItem(key);
  }
}
