"use client";

import { useEffect, type RefObject } from "react";
import { currentCSRFToken } from "@/lib/identity/session";
import { ProgressReporter } from "./progress-reporter-controller";
import { attachProgressReporter } from "./progress-reporter-lifecycle";

export function useProgressReporter(
  videoRef: RefObject<HTMLVideoElement>,
  lessonID: string,
  assetVersionID: string | null,
): void {
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !assetVersionID) return;
    const reporter = new ProgressReporter({ lessonID, assetVersionID, csrfToken: currentCSRFToken });
    // The disposer removes every subscription this mount made and disposes the reporter with it,
    // so a StrictMode remount never leaves the previous mount's reporter, interval, or listeners
    // alive alongside the new one.
    return attachProgressReporter(
      { media: video, documentTarget: document, windowTarget: window },
      reporter,
    );
  }, [assetVersionID, lessonID, videoRef]);
}
