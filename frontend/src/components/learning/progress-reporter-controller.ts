import {
  progressConfirmation,
  progressPath,
  progressReport,
  type ProgressConfirmation,
  type ProgressReport,
} from "./progress-contract";

const maximumAttempts = 3;
const retryAfterFallbackMilliseconds = 15_000;
const ordinaryRetryDelays = [2_000, 4_000];

type ProgressTimer = {
  schedule(callback: () => void, delayMilliseconds: number): number;
  cancel(timerID: number): void;
};

export type ProgressReporterOptions = {
  lessonID: string;
  assetVersionID: string;
  fetchImplementation?: typeof fetch;
  now?: () => number;
  random?: () => number;
  timer?: ProgressTimer;
  csrfToken?: () => string | null;
  /**
   * Called with the canonical state the server returned for an accepted write.
   *
   * This is the whole point of the reporter having a response at all. Without
   * it the reporter is write-only and the rendered progress can only be
   * corrected by reloading the page.
   */
  onConfirmed?: (confirmation: ProgressConfirmation) => void;
};

const browserTimer: ProgressTimer = {
  schedule: (callback, delayMilliseconds) => window.setTimeout(callback, delayMilliseconds),
  cancel: (timerID) => window.clearTimeout(timerID),
};

function retryAfterDelay(response: Response, now: number): number {
  const retryAfter = response.headers.get("Retry-After")?.trim();
  if (!retryAfter) return retryAfterFallbackMilliseconds;
  if (/^\d+$/.test(retryAfter)) {
    const seconds = Number(retryAfter);
    const delay = seconds * 1_000;
    return Number.isFinite(delay) ? delay : retryAfterFallbackMilliseconds;
  }
  const retryAt = Date.parse(retryAfter);
  return Number.isFinite(retryAt) && retryAt > now ? retryAt - now : retryAfterFallbackMilliseconds;
}

function ordinaryRetryDelay(attempt: number, random: () => number): number {
  const nominalDelay = ordinaryRetryDelays[attempt - 1];
  return Math.round(nominalDelay * (0.8 + random() * 0.4));
}

function isRetryableResponse(response: Response): boolean {
  return response.status === 408 || response.status === 429 || [500, 502, 503, 504].includes(response.status);
}

function isDeliberateAbort(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}

function newerSample(current: ProgressReport | null, candidate: ProgressReport): ProgressReport {
  if (!current || candidate.position_seconds > current.position_seconds) return candidate;
  return current;
}

// A reporter is permanently scoped to one stable Lesson and exact Asset Version.
export class ProgressReporter {
  private readonly endpoint: string;
  private readonly assetVersion: string;
  private readonly fetchImplementation: typeof fetch;
  private readonly now: () => number;
  private readonly random: () => number;
  private readonly timer: ProgressTimer;
  private readonly csrfToken: () => string | null;
  private readonly lessonID: string;
  private readonly onConfirmed: (confirmation: ProgressConfirmation) => void;
  private pending: ProgressReport | null = null;
  private timerID: number | null = null;
  private abortController: AbortController | null = null;
  private attemptCount = 0;
  private inFlight = false;
  private disposed = false;
  private exiting = false;

  constructor(options: ProgressReporterOptions) {
    this.endpoint = progressPath(options.lessonID);
    this.assetVersion = options.assetVersionID;
    // `fetch` must keep its global receiver. Storing the bare reference and then calling it as
    // `this.fetchImplementation(...)` invokes it with the reporter as `this`, which throws
    // "Illegal invocation" in the browser before any request is issued — a failure the retry
    // path then swallows, so every Progress write disappears silently.
    this.fetchImplementation =
      options.fetchImplementation ?? ((input: RequestInfo | URL, init?: RequestInit) => fetch(input, init));
    this.now = options.now ?? Date.now;
    this.random = options.random ?? Math.random;
    this.timer = options.timer ?? browserTimer;
    this.csrfToken = options.csrfToken ?? (() => null);
    this.lessonID = options.lessonID;
    this.onConfirmed = options.onConfirmed ?? (() => {});
  }

  reportPosition(positionSeconds: number): void {
    const report = progressReport(positionSeconds, this.assetVersion);
    if (!report || this.disposed || this.exiting) return;
    this.pending = newerSample(this.pending, report);
    if (!this.inFlight && this.timerID === null) this.submit(false);
  }

  reportPageExit(positionSeconds: number): void {
    const report = progressReport(positionSeconds, this.assetVersion);
    if (!report || this.disposed) return;
    this.exiting = true;
    this.pending = newerSample(this.pending, report);
    this.cancelTimer();
    if (!this.inFlight && this.attemptCount < maximumAttempts) this.submit(true);
  }

  dispose(): void {
    this.disposed = true;
    this.pending = null;
    this.cancelTimer();
    this.abortController?.abort();
    this.abortController = null;
  }

  private submit(keepalive: boolean): void {
    const report = this.pending;
    if (!report || this.disposed || this.inFlight || this.attemptCount >= maximumAttempts) return;
    this.pending = null;
    this.inFlight = true;
    this.attemptCount += 1;
    this.abortController = new AbortController();
    this.sendRequest(report, keepalive);
  }

  private sendRequest(report: ProgressReport, keepalive: boolean): void {
    let requestOptions: RequestInit;
    try {
      requestOptions = this.requestOptions(report, keepalive);
    } catch (error) {
      this.requestFailed(report, false, error);
      return;
    }
    try {
      void this.fetchImplementation(this.endpoint, requestOptions)
        .then((response) => this.responseReceived(report, response, keepalive))
        .catch((error: unknown) => this.requestFailed(report, true, error));
    } catch (error) {
      this.requestFailed(report, true, error);
    }
  }

  private requestOptions(report: ProgressReport, keepalive: boolean): RequestInit {
    const csrf = this.csrfToken();
    return {
      method: "PUT",
      credentials: "same-origin",
      cache: "no-store",
      keepalive,
      headers: {
        Accept: "application/json, application/problem+json",
        "Content-Type": "application/json",
        ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      },
      body: JSON.stringify(report),
      signal: this.abortController?.signal,
    };
  }

  private responseReceived(report: ProgressReport, response: Response, keepalive: boolean): void {
    if (response.ok) {
      // The body is read before the success bookkeeping so a slow parse cannot
      // delay the next queued report, and a body that fails to parse is simply
      // no confirmation — the write still succeeded.
      void this.publishConfirmation(response);
      this.requestSucceeded();
      return;
    }
    this.requestFailed(report, !keepalive && isRetryableResponse(response), null, response);
  }

  private async publishConfirmation(response: Response): Promise<void> {
    try {
      const confirmation = progressConfirmation(this.lessonID, await response.json());
      if (confirmation && !this.disposed) this.onConfirmed(confirmation);
    } catch {
      // A 204 from an older server, an empty body, or unparseable JSON all mean
      // the same thing here: nothing new to render. The visible progress keeps
      // whatever it already had rather than being reset to a guess.
    }
  }

  private requestFailed(report: ProgressReport, retryable: boolean, error: unknown, response?: Response): void {
    this.inFlight = false;
    this.abortController = null;
    if (this.disposed || this.exiting || isDeliberateAbort(error)) return;
    if (!retryable || this.attemptCount === maximumAttempts) {
      this.finishChain();
      return;
    }
    this.pending = newerSample(this.pending, report);
    this.scheduleRetry(response);
  }

  private requestSucceeded(): void {
    this.inFlight = false;
    this.abortController = null;
    this.attemptCount = 0;
    if (!this.exiting && this.pending) this.submit(false);
  }

  private scheduleRetry(response?: Response): void {
    const delay = response?.status === 429
      ? retryAfterDelay(response, this.now())
      : ordinaryRetryDelay(this.attemptCount, this.random);
    this.timerID = this.timer.schedule(() => {
      this.timerID = null;
      this.submit(false);
    }, delay);
  }

  private cancelTimer(): void {
    if (this.timerID === null) return;
    this.timer.cancel(this.timerID);
    this.timerID = null;
  }

  private finishChain(): void {
    this.pending = null;
    this.attemptCount = 0;
    this.cancelTimer();
  }
}
