import { authenticatedRequest } from "./http";
import { ProblemError } from "./problem";
import type { AssetKind, LocalisedInput } from "./media-upload";
import { sha256Hex } from "./media-upload";
import {
  UploadProgressTracker,
  progressFraction,
  type UploadProgress,
} from "./upload-progress";

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
  /**
   * A graceful pause. When aborted, no new part is started and a failed part is not retried, but
   * parts already being sent are allowed to finish so their bytes are saved on the server. `signal`
   * remains the hard stop.
   */
  drain?: AbortSignal;
  onVerifying?: () => void;
  /** Called once the server confirmed which parts it already holds for a resumed upload. */
  onResuming?: (summary: SavedUploadSummary) => void;
};
/** Non-sensitive description of the selected file. Never its bytes; never a signed URL. */
type CheckpointFile = { name: string; size: number; type: string };
type Checkpoint = {
  requestID: string;
  ticket: MultipartUploadTicket | null;
  parts: Part[];
  /** Absent on checkpoints written before this field existed. */
  file?: CheckpointFile;
  updatedAt?: number;
};
export type UploadProgressListener = (fraction: number, progress: UploadProgress) => void;

/**
 * What the browser can safely say about a saved upload before the file is reselected.
 *
 * The local checkpoint is a hint; when a server session exists its part list replaces the local
 * one (`source: "server"`). `percent` and `sizeBytes` are null for checkpoints written before the
 * file description was stored.
 */
export type SavedUploadSummary = {
  fileName: string | null;
  sizeBytes: number | null;
  completedBytes: number;
  percent: number | null;
  assetVersionID: string | null;
  /** STARTING: no server session yet. PAUSED: parts can be resumed. FINISHING: all bytes are
   * stored and only completion/verification remains. CANCELLED: the server session ended. */
  status: "STARTING" | "PAUSED" | "FINISHING" | "CANCELLED";
  source: "local" | "server";
};

/** Another upload for the same control is already running here or in another tab. */
export class UploadAlreadyRunningError extends Error {
  constructor() {
    super("This upload is already running in another tab or window.");
    this.name = "UploadAlreadyRunningError";
  }
}

/** The selected file is not the file the saved upload belongs to. Nothing was uploaded. */
export class ResumeFileMismatchError extends Error {
  constructor(readonly savedFileName: string | null) {
    super(
      savedFileName
        ? `This is not the same file as the paused upload. Select ${savedFileName} to continue, or cancel the saved upload and start a new one.`
        : "This is not the same file as the paused upload. Select the original file to continue, or cancel the saved upload and start a new one.",
    );
    this.name = "ResumeFileMismatchError";
  }
}
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

function savedCheckpoints(
  input: Pick<ResumableInput, "courseID" | "revisionID" | "storageKeyId">,
): Array<{ key: string; checkpoint: Checkpoint }> {
  return Object.keys(localStorage)
    .filter((key) => key.startsWith(prefix(input)))
    .map((key) => ({ key, checkpoint: readCheckpoint(key) }))
    .filter((entry): entry is { key: string; checkpoint: Checkpoint } => entry.checkpoint !== null)
    .sort((a, b) => (b.checkpoint.updatedAt || 0) - (a.checkpoint.updatedAt || 0));
}

function persist(key: string, checkpoint: Checkpoint): void {
  checkpoint.updatedAt = Date.now();
  localStorage.setItem(key, JSON.stringify(checkpoint));
}

/**
 * Summarises one checkpoint. When `server` is given it is the authority on which parts exist and
 * whether the session is still open; the local part list is used only when there is no server
 * answer. Part sizes are derived from the stored part size and file size, not trusted from either.
 */
export function summarizeCheckpoint(
  checkpoint: Pick<Checkpoint, "ticket" | "parts" | "file">,
  server?: MultipartUploadTicket,
): SavedUploadSummary {
  const ticket = server || checkpoint.ticket;
  const sizeBytes = checkpoint.file?.size ?? null;
  const status: SavedUploadSummary["status"] = !ticket
    ? "STARTING"
    : ticket.status === "ABORTING" || ticket.status === "ABORTED"
      ? "CANCELLED"
      : ticket.status === "ACTIVE"
        ? "PAUSED"
        : "FINISHING";
  const parts = server ? server.parts : checkpoint.parts;
  let completedBytes = 0;
  if (status === "FINISHING" && sizeBytes !== null) completedBytes = sizeBytes;
  else if (status === "PAUSED" && ticket) {
    for (const part of new Set(parts.map((p) => p.part_number))) {
      const declared = parts.find((p) => p.part_number === part)?.size_bytes ?? 0;
      const expected =
        sizeBytes === null
          ? declared
          : Math.max(0, Math.min(ticket.part_size_bytes, sizeBytes - (part - 1) * ticket.part_size_bytes));
      completedBytes += expected;
    }
    if (sizeBytes !== null) completedBytes = Math.min(completedBytes, sizeBytes);
  }
  return {
    fileName: checkpoint.file?.name ?? null,
    sizeBytes,
    completedBytes,
    percent:
      sizeBytes === null
        ? null
        : sizeBytes === 0
          ? 100
          : Math.min(100, Math.floor((completedBytes / sizeBytes) * 100)),
    assetVersionID: ticket?.asset_version_id ?? null,
    status,
    source: server ? "server" : "local",
  };
}

/** The saved upload for this control from the browser checkpoint alone; no network. */
export function readSavedUpload(
  input: Pick<ResumableInput, "courseID" | "revisionID" | "storageKeyId">,
): SavedUploadSummary | null {
  const latest = savedCheckpoints(input)[0];
  return latest ? summarizeCheckpoint(latest.checkpoint) : null;
}

/**
 * The saved upload reconciled with the server session it names. Falls back to the local summary
 * when the server cannot be reached, so a reload never shows a false 0%.
 */
export async function refreshSavedUpload(
  input: Pick<ResumableInput, "courseID" | "revisionID" | "storageKeyId" | "locale">,
): Promise<SavedUploadSummary | null> {
  const latest = savedCheckpoints(input)[0];
  if (!latest) return null;
  if (!latest.checkpoint.ticket) return summarizeCheckpoint(latest.checkpoint);
  try {
    const server = await getSession(input, latest.checkpoint.ticket.asset_version_id);
    return summarizeCheckpoint(latest.checkpoint, server);
  } catch {
    return summarizeCheckpoint(latest.checkpoint);
  }
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
    input.signal,
  );
  if (!ticket) throw new Error("Upload creation returned no session");
  return ticket;
}

export async function presignUploadPart(
  input: LocalisedInput & { assetVersionID: string; partNumber: number; signal?: AbortSignal },
): Promise<{ url: string }> {
  const result = await authenticatedRequest<{ url: string }>(
    `/media/uploads/${encodeURIComponent(input.assetVersionID)}/multipart/parts/${input.partNumber}`,
    "POST",
    input.locale,
    input.csrf,
    {},
    input.signal,
  );
  if (!result) throw new Error("Part signing returned no URL");
  return result;
}

async function getSession(
  input: Pick<LocalisedInput, "locale">,
  id: string,
  signal?: AbortSignal,
): Promise<MultipartUploadTicket> {
  const ticket = await authenticatedRequest<MultipartUploadTicket>(
    `/media/uploads/${encodeURIComponent(id)}/multipart`,
    "GET",
    input.locale,
    undefined,
    undefined,
    signal,
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
    signal?: AbortSignal;
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
    input.signal,
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
  onProgress?: (loaded: number) => void,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest();
    let settled = false;
    const abort = () => request.abort();
    const fail = (error: Error) => {
      settled = true;
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
    // Byte-level progress for this part's body. Events after settlement or abort are ignored.
    if (onProgress && request.upload)
      request.upload.onprogress = (event: ProgressEvent) => {
        if (settled || signal?.aborted) return;
        onProgress(Math.min(event.loaded, chunk.size));
      };
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
      settled = true;
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

const PART_ATTEMPTS = 3;

async function uploadPartWithRetry(
  file: File,
  input: ResumableInput,
  ticket: MultipartUploadTicket,
  number: number,
  tracker: UploadProgressTracker,
  emit: () => void,
): Promise<Part> {
  const start = (number - 1) * ticket.part_size_bytes;
  const chunk = file.slice(
    start,
    Math.min(start + ticket.part_size_bytes, file.size),
  );
  for (let attempt = 0; ; attempt++) {
    input.signal?.throwIfAborted();
    // A retry that was waiting out its backoff when Pause was pressed does not start.
    if (attempt > 0 && input.drain?.aborted) throw new DOMException("Upload paused", "AbortError");
    try {
      const { url } = await presignUploadPart({
        ...input,
        assetVersionID: ticket.asset_version_id,
        partNumber: number,
      });
      // Pause arrived while this part was being signed: no byte of it is on the wire yet, so it
      // is left for Resume rather than started.
      if (input.drain?.aborted) throw new DOMException("Upload paused", "AbortError");
      const etag = await uploadFilePart(
        url,
        chunk,
        input.contentType || file.type,
        input.signal,
        (loaded) => {
          tracker.partProgress(number, loaded);
          emit();
        },
      );
      return { part_number: number, etag, size_bytes: chunk.size };
    } catch (error) {
      const willRetry =
        attempt < PART_ATTEMPTS - 1 && retryable(error) && !input.drain?.aborted;
      // The failed attempt's bytes never reached the server; they stop counting before any retry.
      tracker.partReset(number, willRetry);
      if (!willRetry) throw error;
      emit();
      await new Promise((resolve) => setTimeout(resolve, 500 * (attempt + 1)));
    }
  }
}

async function transferParts(
  file: File,
  input: ResumableInput,
  checkpoint: Checkpoint,
  key: string,
  onProgress?: UploadProgressListener,
) {
  const ticket = checkpoint.ticket;
  if (!ticket) throw new Error("Upload session has not been created");
  const count = Math.ceil(file.size / ticket.part_size_bytes);
  const done = new Map(checkpoint.parts.map((p) => [p.part_number, p]));
  const pending = Array.from({ length: count }, (_, i) => i + 1).filter(
    (n) => !done.has(n),
  );
  const tracker = new UploadProgressTracker(
    file.size,
    ticket.part_size_bytes,
    Array.from(done.keys()),
  );
  let failure: unknown;
  // Nothing is reported once the run is stopping: a paused or failed upload must not keep moving.
  const report = () => {
    if (failure || input.signal?.aborted) return;
    const progress = tracker.snapshot();
    onProgress?.(progressFraction(progress), progress);
  };
  report();
  const worker = async () => {
    // A graceful pause stops new parts here; parts already in flight run to completion.
    while (pending.length && !failure && !input.drain?.aborted) {
      const number = pending.shift()!;
      try {
        const part = await uploadPartWithRetry(file, input, ticket, number, tracker, report);
        done.set(number, part);
        tracker.partCompleted(number);
        checkpoint.parts = Array.from(done.values()).sort(
          (a, b) => a.part_number - b.part_number,
        );
        persist(key, checkpoint);
        report();
      } catch (error) {
        failure = error;
        tracker.stopAll();
      }
    }
  };
  // Build workers before any worker mutates the pending queue.
  await Promise.all(
    Array.from({ length: Math.min(3, pending.length) }, () => worker()),
  );
  // A transient failure while finishing a requested pause is not an upload failure: that part
  // was not saved and is simply sent again on Resume.
  if (failure && input.drain?.aborted && retryable(failure))
    throw new DOMException("Upload paused", "AbortError");
  if (failure) throw failure;
  input.signal?.throwIfAborted();
  // A requested pause ends here, after the parts in flight were saved — even when none are left to
  // send. Resume then has nothing to upload and goes straight to completion.
  if (input.drain?.aborted) throw new DOMException("Upload paused", "AbortError");
}

type LockManagerLike = {
  request: <T>(
    name: string,
    options: { ifAvailable: boolean },
    callback: (lock: unknown) => Promise<T>,
  ) => Promise<T>;
};

/*
  One upload per control, across tabs. The checkpoint check below reads localStorage, which another
  tab can write while this tab is still fingerprinting a large file; without a shared lock two tabs
  could each start a session for different files under the same lesson. The Web Locks API is held
  for the whole run where the browser provides it; elsewhere this tab's own set still serialises.
*/
async function withControlLock<T>(name: string, run: () => Promise<T>): Promise<T> {
  const locks = (globalThis.navigator as { locks?: LockManagerLike } | undefined)?.locks;
  if (!locks?.request) return run();
  return locks.request(name, { ifAvailable: true }, async (lock) => {
    if (!lock) throw new UploadAlreadyRunningError();
    return run();
  });
}

export async function uploadResumable(
  file: File,
  input: ResumableInput,
  onProgress?: UploadProgressListener,
): Promise<MultipartCompletionResult> {
  const control = prefix(input);
  if (activeUploads.has(control)) throw new UploadAlreadyRunningError();
  activeUploads.add(control);
  try {
    return await withControlLock(control, () => uploadResumableExclusive(file, input, onProgress));
  } finally {
    activeUploads.delete(control);
  }
}

async function uploadResumableExclusive(
  file: File,
  input: ResumableInput,
  onProgress?: UploadProgressListener,
): Promise<MultipartCompletionResult> {
  // One saved upload per control. A file of a different size cannot be the saved one, so it is
  // refused before spending time on a full fingerprint.
  const saved = savedCheckpoints(input);
  if (
    saved.length &&
    saved.every(({ checkpoint }) => checkpoint.file && checkpoint.file.size !== file.size)
  )
    throw new ResumeFileMismatchError(saved[0].checkpoint.file?.name ?? null);
  // Full streaming fingerprint prevents splicing two same-name/same-size files after refresh.
  const digest = await sha256Hex(file, input.signal);
  const key = prefix(input) + digest;
  // Fingerprinting takes a while; decide on the checkpoints as they are now, not as they were.
  // The file's own checkpoint always resumes, even beside an older one an earlier build left.
  const current = savedCheckpoints(input);
  if (current.length && !current.some((entry) => entry.key === key))
    throw new ResumeFileMismatchError(current[0].checkpoint.file?.name ?? null);
  let checkpoint = readCheckpoint(key);
  const description: CheckpointFile = {
    name: file.name,
    size: file.size,
    type: input.contentType || file.type,
  };
  if (checkpoint?.ticket) {
    const ticket = await getSession(
      input,
      checkpoint.ticket.asset_version_id,
      input.signal,
    );
    checkpoint.ticket = ticket;
    checkpoint.file = checkpoint.file || description;
    // The server's part list wins over whatever this browser remembered.
    if (ticket.status === "ACTIVE")
      checkpoint.parts = ticket.parts.sort(
        (a, b) => a.part_number - b.part_number,
      );
    if (ticket.status === "ABORTING" || ticket.status === "ABORTED")
      throw new Error(
        "This upload was cancelled. Clear it before starting again.",
      );
    if (ticket.status === "ACTIVE")
      input.onResuming?.(summarizeCheckpoint(checkpoint, ticket));
  } else {
    checkpoint = checkpoint || {
      requestID: crypto.randomUUID(),
      ticket: null,
      parts: [],
    };
    checkpoint.file = description;
    persist(key, checkpoint);
    checkpoint.ticket = await beginMultipartUpload({
      ...input,
      requestID: checkpoint.requestID,
      contentType: input.contentType || file.type,
      sizeBytes: file.size,
    });
    checkpoint.parts = checkpoint.ticket.parts;
  }
  persist(key, checkpoint);
  const ticket = checkpoint.ticket;
  if (!ticket) throw new Error("Upload session has not been created");
  if (ticket.status === "ACTIVE")
    await transferParts(file, input, checkpoint, key, onProgress);
  input.signal?.throwIfAborted();
  // A pause requested after the last part was saved still pauses: every part stays saved and
  // Resume only has to complete.
  if (input.drain?.aborted) throw new DOMException("Upload paused", "AbortError");
  let result = await completeMultipartUpload({
    ...input,
    ticket,
    contentType: input.contentType || file.type,
    sizeBytes: file.size,
    sha256Hex: digest,
    parts: checkpoint.parts,
    signal: input.signal,
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
      undefined,
      undefined,
      input.signal,
    );
    if (!status) throw new Error("Upload verification returned no status");
    result = { ...result, ...status };
  }
  // Keep the checkpoint until the separate course-selection operation is acknowledged.
  return result;
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
