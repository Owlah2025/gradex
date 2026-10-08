import {
  ResumeFileMismatchError,
  type MultipartCompletionResult,
  type ResumableInput,
  type SavedUploadSummary,
  type UploadProgressListener,
} from "../../lib/api/media-multipart";
import { ThroughputMeter, type UploadProgress } from "../../lib/api/upload-progress";

export type TransferReadout = {
  progress: UploadProgress;
  bytesPerSecond: number | null;
  secondsRemaining: number | null;
};

export type ResumableState = {
  pending: boolean;
  saved: SavedUploadSummary | null;
  running: boolean;
  /** Pause was requested: no new parts start; parts in flight are finishing so they are saved. */
  pausing: boolean;
  verifying: boolean;
  cancelling: boolean;
  cancelled: boolean;
  resuming: SavedUploadSummary | null;
  transfer: TransferReadout | null;
  fileAvailable: boolean;
  error: string | null;
};

export const initialResumableState: ResumableState = {
  pending: false,
  saved: null,
  running: false,
  pausing: false,
  verifying: false,
  cancelling: false,
  cancelled: false,
  resuming: null,
  transfer: null,
  fileAvailable: false,
  error: null,
};

export type SessionInput = Omit<ResumableInput, "csrf">;

/** Everything the session talks to, injected so its lifecycle can be exercised without a browser. */
export type SessionDependencies = {
  upload: (file: File, input: ResumableInput, onProgress?: UploadProgressListener) => Promise<MultipartCompletionResult>;
  hasSaved: (input: SessionInput) => boolean;
  readSaved: (input: SessionInput) => SavedUploadSummary | null;
  refreshSaved: (input: SessionInput) => Promise<SavedUploadSummary | null>;
  cancelSaved: (input: ResumableInput) => Promise<void>;
  acknowledgeSaved: (input: ResumableInput, digest: string) => void;
  csrf: () => string | null;
  describeError: (error: unknown, locale: "ar" | "en") => string;
  now: () => number;
  /** How long a graceful pause may wait for in-flight parts before stopping them outright. */
  pauseDrainTimeoutMs?: number;
  setTimer?: (callback: () => void, ms: number) => unknown;
  clearTimer?: (handle: unknown) => void;
};

// Long enough for one 8 MiB part per worker on a slow (~150 KB/s) link; a pause never hangs longer.
export const DEFAULT_PAUSE_DRAIN_TIMEOUT_MS = 60_000;

/**
 * Whether leaving the page now would discard work: bytes of parts not yet accepted are actually on
 * the wire. Nothing is at risk while parts are only being signed, between parts, once every part is
 * saved, or while the server verifies and processes (which needs no tab).
 */
export function transferInProgress(state: Pick<ResumableState, "running" | "transfer" | "verifying">): boolean {
  return (
    state.running &&
    state.transfer !== null &&
    !state.verifying &&
    state.transfer.progress.transferredBytes > state.transfer.progress.completedBytes
  );
}

// Progress events arrive per XHR many times a second; the readout is refreshed at most this often,
// except when a part finishes, a retry starts or ends, or the upload reaches 100%.
const READOUT_INTERVAL_MS = 150;

const identityOf = (input: SessionInput) => `${input.courseID}\u0000${input.revisionID || ""}\u0000${input.storageKeyId}`;

/**
 * One upload control's resumable lifecycle, independent of React.
 *
 * Every asynchronous result is scoped to the control identity and the run that produced it. When the
 * control switches to another Lesson or Course, the old run is aborted and nothing it later reports
 * — progress, verification, its final saved summary, or the File it held — can reach the new
 * identity's state. A late server answer to a saved-upload read is likewise ignored once anything
 * newer has happened.
 */
export class ResumableUploadSession {
  private state: ResumableState = initialResumableState;
  private input: SessionInput | null = null;
  private identity: string | null = null;
  private controller: AbortController | null = null;
  private drain: AbortController | null = null;
  private drainTimer: unknown = null;
  private operation: Promise<unknown> | null = null;
  private retainedFile: File | null = null;
  private generation = 0;
  // Completed runs awaiting acknowledgement remember the control they belong to, keyed by the
  // result object that run returned — unique per run even when two Lessons upload identical bytes.
  // The caller acknowledges only after a separate attach request, by which time the mounted control
  // may show another Lesson; the acknowledgement must still clear the originating checkpoint.
  private readonly completedRuns = new WeakMap<MultipartCompletionResult, { identity: string; input: SessionInput }>();

  constructor(
    private readonly deps: SessionDependencies,
    private readonly publish: (state: ResumableState) => void,
  ) {}

  snapshot(): ResumableState {
    return this.state;
  }

  /** The File picked in this tab for the current control, if any; resuming with it needs no picker. */
  lastFile(): File | null {
    return this.retainedFile;
  }

  /** Called on every render-affecting change. Only a different control identity resets the state. */
  setInput(input: SessionInput): void {
    const identity = identityOf(input);
    this.input = input;
    if (identity === this.identity) return;
    this.abandon();
    this.identity = identity;
    this.retainedFile = null;
    this.update({ ...initialResumableState });
    this.reloadSaved();
  }

  /**
   * The control unmounted: stop the transfer (it stays resumable) and ignore anything it reports.
   * The identity is forgotten, so a remount (React StrictMode mounts twice) starts cleanly.
   */
  dispose(): void {
    this.abandon();
    this.identity = null;
    this.retainedFile = null;
  }

  async run(file: File, progress: (fraction: number) => void, contentType?: string): Promise<MultipartCompletionResult> {
    const input = this.requireInput();
    if (this.controller) throw new Error("This upload is already running");
    const csrf = this.deps.csrf();
    if (!csrf) throw new Error("Your session must be refreshed before uploading.");
    const active = new AbortController();
    const drain = new AbortController();
    this.controller = active;
    this.drain = drain;
    this.generation++;
    const identity = this.identity;
    const current = () => this.controller === active && this.identity === identity;
    this.retainedFile = file;
    this.update({ fileAvailable: true, running: true, cancelled: false, resuming: null, transfer: null, error: null });

    const meter = new ThroughputMeter();
    let lastReadout = 0;
    let lastRetrying = false;
    let lastCompleted = -1;
    let lastAtRisk = false;
    const task = this.deps.upload(
      file,
      {
        ...input,
        csrf,
        contentType,
        signal: active.signal,
        drain: drain.signal,
        onVerifying: () => {
          if (current()) this.update({ verifying: true });
        },
        onResuming: (summary) => {
          if (current()) this.update({ resuming: summary });
        },
      },
      (fraction, detail) => {
        if (!current()) return;
        const now = this.deps.now();
        // A change in whether bytes are at risk is published at once (below), so Pause and the
        // leave-page prompt never act on a readout that is one throttle interval old.
        const atRisk = detail.transferredBytes > detail.completedBytes;
        // Speed is measured from the first byte this run actually sends, not from the moment the
        // run started (fingerprinting, session recovery and signing are not upload throughput).
        if (detail.transferredBytes > detail.resumedFromBytes) meter.record(detail.reportedBytes, now);
        const settled = detail.reportedBytes >= detail.totalBytes;
        if (
          now - lastReadout < READOUT_INTERVAL_MS &&
          detail.retrying === lastRetrying &&
          detail.completedBytes === lastCompleted &&
          atRisk === lastAtRisk &&
          !settled
        )
          return;
        lastReadout = now;
        lastRetrying = detail.retrying;
        lastCompleted = detail.completedBytes;
        lastAtRisk = atRisk;
        this.update({
          transfer: {
            progress: detail,
            bytesPerSecond: meter.bytesPerSecond(),
            secondsRemaining: meter.secondsRemaining(detail.totalBytes - detail.reportedBytes),
          },
        });
        progress(fraction);
      },
    );
    this.operation = task;
    try {
      const result = await task;
      active.signal.throwIfAborted();
      if (!current()) throw new DOMException("Upload paused", "AbortError");
      // Pause was requested after the last part: the run ends paused and its checkpoint stays for
      // Resume. It is never handed to the caller to attach and acknowledge.
      if (drain.signal.aborted) throw new DOMException("Upload paused", "AbortError");
      this.completedRuns.set(result, { identity: identity!, input });
      return result;
    } catch (cause) {
      // A refused file must never become the in-tab file that "Resume upload" would send.
      if (current() && cause instanceof ResumeFileMismatchError) {
        this.retainedFile = null;
        this.update({ fileAvailable: false });
      }
      throw cause;
    } finally {
      if (this.drain === drain) this.clearDrain();
      if (current()) {
        this.controller = null;
        this.operation = null;
        this.update({ running: false, pausing: false, verifying: false, resuming: null, transfer: null });
        this.reloadSaved();
      }
    }
  }

  /**
   * Pause without throwing away bytes already on the wire. While parts are being sent, no new part
   * starts and the ones in flight finish, so their bytes are saved; the run then ends as paused.
   * The wait is bounded, and stopNow() ends it at once. Before any part is moving (checking the
   * file, recovering the session) or while verifying, there is nothing to save and the run stops.
   */
  pause(): void {
    if (!this.controller) return;
    if (!transferInProgress(this.state) || !this.drain) {
      this.stopNow();
      return;
    }
    if (this.drain.signal.aborted) return;
    this.drain.abort();
    this.update({ pausing: true });
    const setTimer = this.deps.setTimer ?? ((callback: () => void, ms: number) => setTimeout(callback, ms));
    this.drainTimer = setTimer(() => this.stopNow(), this.deps.pauseDrainTimeoutMs ?? DEFAULT_PAUSE_DRAIN_TIMEOUT_MS);
  }

  /** Stop immediately. Parts still in flight are discarded; completed parts stay saved. */
  stopNow(): void {
    this.clearDrain();
    this.controller?.abort(new DOMException("Upload paused", "AbortError"));
  }

  private clearDrain(): void {
    if (this.drainTimer !== null) {
      const clearTimer = this.deps.clearTimer ?? ((handle: unknown) => clearTimeout(handle as ReturnType<typeof setTimeout>));
      clearTimer(this.drainTimer);
      this.drainTimer = null;
    }
    this.drain = null;
  }

  async cancel(): Promise<void> {
    const input = this.requireInput();
    const identity = this.identity;
    this.generation++;
    this.update({ cancelling: true, error: null });
    // Cancel is a hard stop, never a graceful drain.
    this.clearDrain();
    this.controller?.abort(new DOMException("Upload paused", "AbortError"));
    try {
      if (this.operation) await this.operation.catch(() => undefined);
      const csrf = this.deps.csrf();
      if (!csrf) throw new Error("Your session must be refreshed before cancelling.");
      await this.deps.cancelSaved({ ...input, csrf });
      if (this.identity !== identity) return;
      // A saved-upload read started while the run was settling must not repaint what was cancelled.
      this.generation++;
      this.retainedFile = null;
      this.update({ fileAvailable: false, pending: false, saved: null, cancelled: true });
    } catch (cause) {
      if (this.identity === identity) this.update({ error: this.deps.describeError(cause, input.locale) });
    } finally {
      if (this.identity === identity) this.update({ cancelling: false });
    }
  }

  /**
   * The completed upload was attached; its checkpoint can go. The checkpoint cleared is the one
   * the completing run belonged to, never whichever control happens to be mounted now, and the
   * visible state changes only if that control is still the current one and nothing newer runs.
   */
  acknowledge(result: MultipartCompletionResult): void {
    const origin = this.completedRuns.get(result);
    if (!origin) return;
    this.completedRuns.delete(result);
    this.deps.acknowledgeSaved({ ...origin.input, csrf: this.deps.csrf() || "" }, result.sha256_hex);
    if (origin.identity !== this.identity || this.controller) return;
    this.generation++;
    this.retainedFile = null;
    this.update({ fileAvailable: false, pending: this.deps.hasSaved(origin.input), saved: this.deps.readSaved(origin.input) });
  }

  private reloadSaved(): void {
    const input = this.requireInput();
    const token = ++this.generation;
    const local = this.deps.readSaved(input);
    this.update({ pending: this.deps.hasSaved(input), saved: local });
    if (!local) return;
    // The server's part list replaces the local hint as soon as it answers — unless anything newer
    // (another control, a run, a cancel, an acknowledgement, an unmount) has happened since.
    void this.deps.refreshSaved(input).then((summary) => {
      if (this.generation === token && !this.controller) this.update({ saved: summary });
    });
  }

  private abandon(): void {
    this.generation++;
    this.clearDrain();
    this.controller?.abort(new DOMException("Upload paused", "AbortError"));
    this.controller = null;
    this.operation = null;
  }

  private requireInput(): SessionInput {
    if (!this.input) throw new Error("Upload control has no identity yet");
    return this.input;
  }

  private update(patch: Partial<ResumableState>): void {
    this.state = { ...this.state, ...patch };
    this.publish(this.state);
  }
}
