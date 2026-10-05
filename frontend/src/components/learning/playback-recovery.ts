export type ResumePlayback = { lessonID: string; position: number; playing: boolean };

type HLSError = { fatal: boolean; type: string; response?: { code?: number } };

export function isAuthorizationHLSError(error: HLSError, expiresAt: string, now = Date.now()): boolean {
  const code = error.response?.code;
  // The API deliberately hides expired capabilities behind its uniform 404.
  // A 404 before known expiry remains a terminal media failure.
  return error.type === "networkError" &&
    (code === 401 || code === 403 || (code === 404 && Date.parse(expiresAt) <= now));
}

// Fatal authorization failures refresh immediately. Non-fatal retries get a
// bounded chance to recover instead of leaving permanent segment refusals spinning.
export function isExpiredHLSError(error: HLSError, expiresAt: string, now = Date.now()): boolean {
  return error.fatal && isAuthorizationHLSError(error, expiresAt, now);
}

export function createAuthorizationFailureDeadline(refresh: () => void) {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let active = true;
  const clear = () => {
    if (timer !== null) clearTimeout(timer);
    timer = null;
  };
  return {
    denied() {
      if (!active || timer !== null) return;
      timer = setTimeout(() => { timer = null; if (active) refresh(); }, 5_000);
    },
    recovered: clear,
    stop() { active = false; clear(); },
  };
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
