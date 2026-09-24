"use client";

import Hls from "hls.js";
import { useEffect, useRef, useState } from "react";

/**
 * The one HLS attachment and teardown used by every surface that plays an
 * application-issued protected manifest.
 *
 * It was extracted from the Admin review player rather than written afresh, and
 * every surface now shares it, because the fatal-error and teardown behaviour is
 * the part that is easy to get subtly wrong: a player that keeps a dead `Hls`
 * instance attached after a fatal error leaks a worker and a network loop, and one
 * that leaves `src` set on unmount keeps fetching segments for a video nobody is
 * watching. Duplicating that per surface means fixing it once and missing it
 * everywhere else.
 *
 * It never decides what may be watched. The `manifestURL` is an application route
 * the server already authorized; this only renders it.
 */
export function ProtectedHLSPlayer({
  manifestURL,
  label,
  unavailableLabel,
  testID,
  unavailableTestID,
  className,
  autoPlay = false,
}: {
  /** An application manifest route. Never a storage object URL. */
  manifestURL: string;
  /** The accessible name for the video element. */
  label: string;
  /** What to say when the stream cannot be played. */
  unavailableLabel: string;
  testID: string;
  unavailableTestID: string;
  className?: string;
  autoPlay?: boolean;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [unavailable, setUnavailable] = useState(false);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !manifestURL) return;
    let hls: Hls | null = null;
    setUnavailable(false);

    const mediaFailed = () => setUnavailable(true);
    video.addEventListener("error", mediaFailed);

    if (Hls.isSupported()) {
      hls = new Hls();
      hls.on(Hls.Events.ERROR, (_event, data) => {
        // Only fatal errors surface. hls.js reports recoverable network and media
        // errors routinely and recovers from them itself; treating those as
        // failures would replace a stream that is about to resume with an error
        // message.
        if (data.fatal) setUnavailable(true);
      });
      hls.loadSource(manifestURL);
      hls.attachMedia(video);
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      // Safari plays HLS natively and has no hls.js instance to tear down.
      video.src = manifestURL;
    } else {
      setUnavailable(true);
    }

    return () => {
      video.removeEventListener("error", mediaFailed);
      hls?.destroy();
      video.removeAttribute("src");
      video.load();
    };
  }, [manifestURL]);

  if (unavailable) {
    return (
      <p
        role="alert"
        data-testid={unavailableTestID}
        className="text-sm text-destructive"
      >
        {unavailableLabel}
      </p>
    );
  }

  return (
    <video
      ref={videoRef}
      controls
      autoPlay={autoPlay}
      data-testid={testID}
      aria-label={label}
      className={className ?? "w-full rounded-lg bg-card"}
    />
  );
}
