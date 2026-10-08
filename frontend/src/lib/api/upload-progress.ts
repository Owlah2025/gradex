/**
 * Byte-level accounting for a multipart upload.
 *
 * Progress used to advance only when a whole part finished, so with three parts in flight the
 * percentage jumped 0 → 10 → 30. This counts what the browser has actually sent: the bytes of
 * parts the server already holds, plus the bytes each in-flight XMLHttpRequest reports through
 * `upload.onprogress`. Nothing here is estimated.
 *
 * Invariants:
 * - a part is counted once: in-flight until it completes, then completed, never both;
 * - an in-flight part is clamped to its own size, and the total to the file size;
 * - a failed attempt's in-flight bytes are dropped before its retry starts, so a retry can never
 *   inflate the total;
 * - `reportedBytes` never moves backwards within one run. When a retry drops bytes, the reported
 *   figure holds at its high-water mark until real bytes catch up, rather than showing a misleading
 *   regression. `transferredBytes` always carries the exact figure.
 */
export type UploadProgress = {
  totalBytes: number;
  /** Bytes in parts the server has confirmed. This is what survives a pause or a reload. */
  completedBytes: number;
  /** Exact bytes sent: completed parts plus in-flight bytes of active parts. */
  transferredBytes: number;
  /** Monotonic within one run; what the progress bar shows. */
  reportedBytes: number;
  /** Bytes already on the server when this run began (non-zero when resuming). */
  resumedFromBytes: number;
  /** True while at least one part is waiting to retry after a transient failure. */
  retrying: boolean;
};

export class UploadProgressTracker {
  private readonly completed = new Map<number, number>();
  private readonly active = new Map<number, number>();
  private readonly retrying = new Set<number>();
  private highWater = 0;
  private readonly resumedFrom: number;

  constructor(
    private readonly totalBytes: number,
    private readonly partSizeBytes: number,
    completedParts: number[] = [],
  ) {
    for (const part of completedParts) this.completed.set(part, this.partSize(part));
    this.resumedFrom = this.sum(this.completed);
    this.highWater = Math.min(this.resumedFrom, this.totalBytes);
  }

  /** The exact size of a part number for this file; the last part may be short. */
  partSize(partNumber: number): number {
    const start = (partNumber - 1) * this.partSizeBytes;
    return Math.max(0, Math.min(this.partSizeBytes, this.totalBytes - start));
  }

  /** An in-flight XHR reported `loaded` bytes for this part's current attempt. */
  partProgress(partNumber: number, loaded: number): void {
    if (this.completed.has(partNumber)) return;
    const clamped = Math.max(0, Math.min(Number.isFinite(loaded) ? loaded : 0, this.partSize(partNumber)));
    this.active.set(partNumber, clamped);
    if (clamped > 0) this.retrying.delete(partNumber);
  }

  /** The server accepted the part. In-flight bytes become completed bytes exactly once. */
  partCompleted(partNumber: number): void {
    this.active.delete(partNumber);
    this.retrying.delete(partNumber);
    this.completed.set(partNumber, this.partSize(partNumber));
  }

  /**
   * The current attempt for this part failed or was aborted. Its in-flight bytes did not reach
   * the server and stop counting. `willRetry` marks the visible "retrying" state.
   */
  partReset(partNumber: number, willRetry = false): void {
    this.active.delete(partNumber);
    if (willRetry && !this.completed.has(partNumber)) this.retrying.add(partNumber);
    else this.retrying.delete(partNumber);
  }

  /** Every in-flight attempt stopped (pause, cancel, fatal error). Completed parts stay. */
  stopAll(): void {
    this.active.clear();
    this.retrying.clear();
  }

  snapshot(): UploadProgress {
    const completedBytes = Math.min(this.sum(this.completed), this.totalBytes);
    const transferredBytes = Math.min(completedBytes + this.sum(this.active), this.totalBytes);
    this.highWater = Math.max(this.highWater, transferredBytes);
    return {
      totalBytes: this.totalBytes,
      completedBytes,
      transferredBytes,
      reportedBytes: this.highWater,
      resumedFromBytes: Math.min(this.resumedFrom, this.totalBytes),
      retrying: this.retrying.size > 0,
    };
  }

  private sum(values: Map<number, number>): number {
    let total = 0;
    for (const value of values.values()) total += value;
    return total;
  }
}

/** 0–1 for a progress bar. A zero-byte file is complete by definition. */
export function progressFraction(progress: Pick<UploadProgress, "reportedBytes" | "totalBytes">): number {
  if (progress.totalBytes <= 0) return 1;
  return Math.max(0, Math.min(1, progress.reportedBytes / progress.totalBytes));
}

/**
 * Recent throughput from a rolling window of byte samples, so a single slow or bursty progress
 * event does not swing the figure. Returns null until the window holds enough time to mean
 * anything; callers show nothing rather than a guess.
 */
export class ThroughputMeter {
  private samples: Array<{ at: number; bytes: number }> = [];

  constructor(
    private readonly windowMs = 5000,
    private readonly minimumSpanMs = 1000,
  ) {}

  record(bytes: number, at: number): void {
    const last = this.samples.at(-1);
    // A run that restarted (pause, resume) starts a new measurement.
    if (last && (bytes < last.bytes || at < last.at)) this.samples = [];
    this.samples.push({ at, bytes });
    while (this.samples.length > 2 && at - this.samples[1].at >= this.windowMs) this.samples.shift();
  }

  reset(): void {
    this.samples = [];
  }

  bytesPerSecond(): number | null {
    const first = this.samples[0];
    const last = this.samples.at(-1);
    if (!first || !last) return null;
    const span = last.at - first.at;
    if (span < this.minimumSpanMs) return null;
    return ((last.bytes - first.bytes) * 1000) / span;
  }

  secondsRemaining(remainingBytes: number): number | null {
    const rate = this.bytesPerSecond();
    if (rate === null || rate <= 0) return null;
    return Math.max(0, Math.ceil(remainingBytes / rate));
  }
}

const units = ["B", "KB", "MB", "GB", "TB"] as const;

/** Decimal units (1 MB = 1,000,000 bytes), one decimal below 10 and whole numbers above. */
export function formatBytes(bytes: number): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit++;
  }
  const digits = unit === 0 || value >= 10 ? 0 : 1;
  return `${value.toFixed(digits)} ${units[unit]}`;
}
