import type { PlaybackAuthorization } from "@/lib/api/learning";

// An authorization creates a lease even if the component has already gone
// away. Shared subscriptions survive React effect replay; late results release
// their own lease instead of leaking it or releasing a replacement player's.
export function createPlaybackRequest(
  authorize: () => Promise<PlaybackAuthorization>,
  release: (authorization: PlaybackAuthorization) => void,
) {
  let subscribers = 0;
  let released = false;
  let expired = false;
  const releaseOnce = (authorization: PlaybackAuthorization) => {
    if (!released) { released = true; release(authorization); }
  };
  const promise = new Promise<PlaybackAuthorization>((resolve, reject) => {
    const timer = setTimeout(() => {
      expired = true;
      reject(new Error("Playback authorization timed out."));
    }, 15_000);
    void authorize().then((authorization) => {
      clearTimeout(timer);
      if (expired) releaseOnce(authorization);
      else resolve(authorization);
    }, (error: unknown) => { clearTimeout(timer); reject(error); });
  });
  return {
    promise,
    retain() {
      subscribers += 1;
      return () => {
        subscribers -= 1;
        queueMicrotask(() => {
          void promise.then((authorization) => {
            if (subscribers === 0) releaseOnce(authorization);
          }, () => { /* Failed authorization has no lease to release. */ });
        });
      };
    },
  };
}
