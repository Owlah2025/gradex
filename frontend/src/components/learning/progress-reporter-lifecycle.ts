import { progressReportIntervalMilliseconds } from "./progress-contract";

/**
 * The reporter's DOM wiring, extracted from the React hook so the mount/unmount contract can be
 * proved deterministically.
 *
 * React development StrictMode mounts an effect, tears it down, and mounts it again. If the
 * teardown were incomplete, the first mount's interval, listeners, and reporter would survive
 * alongside the second's — two live reporters, each with its own single-flight budget, writing
 * Progress independently. Every subscription made here is therefore removed by the returned
 * disposer, and the reporter it owns is disposed with it.
 */
export type ReporterMediaTarget = {
  currentTime: number;
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
};

export type ReporterDocumentTarget = {
  visibilityState: string;
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
};

export type ReporterWindowTarget = {
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
  setInterval(handler: () => void, milliseconds: number): number;
  clearInterval(handle: number): void;
};

export type LifecycleReporter = {
  reportPosition(positionSeconds: number): void;
  reportPageExit(positionSeconds: number): void;
  dispose(): void;
};

export type ReporterTargets = {
  media: ReporterMediaTarget;
  documentTarget: ReporterDocumentTarget;
  windowTarget: ReporterWindowTarget;
};

export function attachProgressReporter(targets: ReporterTargets, reporter: LifecycleReporter): () => void {
  const { media, documentTarget, windowTarget } = targets;

  const report = () => reporter.reportPosition(media.currentTime);
  const reportWhenHidden = () => {
    if (documentTarget.visibilityState === "hidden") report();
  };
  const reportOnPageHide = () => reporter.reportPageExit(media.currentTime);

  const interval = windowTarget.setInterval(report, progressReportIntervalMilliseconds);
  media.addEventListener("pause", report);
  media.addEventListener("seeked", report);
  // Reaching the end is the moment a Lesson becomes complete, and it is the one
  // moment where waiting for the next interval tick is visible to the Student:
  // the video stops, and the Lesson still says "in progress" for up to fifteen
  // seconds. `ended` also fires on the last frame, so this is the report that
  // actually carries the completing position.
  media.addEventListener("ended", report);
  documentTarget.addEventListener("visibilitychange", reportWhenHidden);
  windowTarget.addEventListener("pagehide", reportOnPageHide);

  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    windowTarget.clearInterval(interval);
    media.removeEventListener("pause", report);
    media.removeEventListener("seeked", report);
    media.removeEventListener("ended", report);
    documentTarget.removeEventListener("visibilitychange", reportWhenHidden);
    windowTarget.removeEventListener("pagehide", reportOnPageHide);
    reporter.dispose();
  };
}
