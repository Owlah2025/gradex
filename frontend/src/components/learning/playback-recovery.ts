export type ResumePlayback = { lessonID: string; position: number; playing: boolean };

type HLSError = { fatal: boolean; type: string; response?: { code?: number } };

// hls.js owns non-fatal retries. Only terminal authorization failures need a
// new application authorization; other terminal errors reach the retry UI.
export function isExpiredHLSError(error: HLSError, expiresAt: string, now = Date.now()): boolean {
  const code = error.response?.code;
  // The API deliberately hides expired capabilities behind its uniform 404.
  // A 404 before known expiry remains a terminal media failure.
  return error.fatal && error.type === "networkError" &&
    (code === 401 || code === 403 || (code === 404 && Date.parse(expiresAt) <= now));
}

// A source can trigger only one refresh. The budget survives source replacement
// and is reset only for another Lesson or an explicit Student retry.
export function createPlaybackRecovery(maxAttempts = 2) {
  let source: string | null = null;
  let attempts = 0;
  let pending = false;
  return {
    bind(key: string) { source = key; pending = false; },
    unbind(key: string) { if (source === key) source = null; },
    begin(key: string): "refresh" | "ignore" | "exhausted" {
      if (source !== key || pending) return "ignore";
      if (attempts >= maxAttempts) return "exhausted";
      attempts += 1;
      pending = true;
      return "refresh";
    },
    reset() { attempts = 0; pending = false; },
  };
}
