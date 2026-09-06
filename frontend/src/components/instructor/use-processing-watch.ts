"use client";

import { useEffect, useRef, useState } from "react";
import {
  getMediaAssetStatus,
  isTerminalState,
  processingProgressOf,
  type MediaAssetStatus,
  type ProcessingStage,
} from "@/lib/api/media-upload";

export type ProcessingObservation = { stage: ProcessingStage; percent: number } | null;

/**
 * Watches one Asset Version's server-side processing and reports its measured
 * progress.
 *
 * This exists because processing outlives the tab that started it. The upload
 * completion is durable and the worker runs on its own, so a reload lands on a
 * lesson whose video is halfway through a transcode with nothing in the browser
 * that remembers it. Recovery therefore cannot come from local state: the hook
 * re-reads the asset from the API and picks the run back up.
 *
 * Bounded and scoped on purpose:
 *
 *   - It polls only while `active`, and stops the moment the asset reaches a
 *     terminal state. A READY or failed asset is never polled again.
 *   - It is keyed on the asset version identifier, so two lessons processing at
 *     once each watch their own asset. There is no shared percentage anywhere.
 *   - Unmounting, or changing the watched asset, aborts the loop in flight; a
 *     late response from a previous asset is discarded rather than rendered.
 *   - `timeoutMs` bounds the whole watch, so a stuck worker cannot leave a
 *     request loop running for the life of the page.
 *
 * No WebSocket or SSE is introduced. The API already serves this state on the
 * route the studio polls, and one more transport for one more field would be a
 * poor trade.
 */
export function useProcessingWatch({
  assetVersionID,
  active,
  locale,
  intervalMs = 1500,
  timeoutMs = 30 * 60 * 1000,
  onSettled,
}: {
  assetVersionID: string | null | undefined;
  active: boolean;
  locale: "ar" | "en";
  intervalMs?: number;
  timeoutMs?: number;
  /** Called once, with the terminal status, when the asset stops moving. */
  onSettled?: (status: MediaAssetStatus) => void;
}): ProcessingObservation {
  const [observation, setObservation] = useState<ProcessingObservation>(null);
  // Held in a ref so a caller may pass an inline closure without restarting the
  // poll on every render.
  const settled = useRef(onSettled);
  settled.current = onSettled;

  useEffect(() => {
    if (!active || !assetVersionID) {
      setObservation(null);
      return;
    }
    let cancelled = false;
    const deadline = Date.now() + timeoutMs;

    const watch = async () => {
      for (;;) {
        if (cancelled) return;
        let status: MediaAssetStatus;
        try {
          status = await getMediaAssetStatus(assetVersionID, locale);
        } catch {
          // A transient read failure is not a processing failure. The next tick
          // asks again; the bound below still ends the watch eventually.
          if (Date.now() + intervalMs > deadline) return;
          await delay(intervalMs);
          continue;
        }
        if (cancelled) return;
        setObservation(processingProgressOf(status));
        if (isTerminalState(status.state)) {
          settled.current?.(status);
          return;
        }
        if (Date.now() + intervalMs > deadline) return;
        await delay(intervalMs);
      }
    };
    void watch();
    return () => {
      cancelled = true;
    };
  }, [active, assetVersionID, locale, intervalMs, timeoutMs]);

  return observation;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
