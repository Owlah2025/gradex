import type { VerificationChallenge } from "@/lib/api/identity";

/**
 * Carrying a verification challenge from one screen to the next.
 *
 * The journey crosses a navigation — register, then verify — and the second
 * screen has to know which challenge it is asking about. Re-prompting for the
 * email address would be the alternative, and it is the exact defect this flow
 * exists to remove: a Student who has just typed their address is asked for it
 * again before they have done anything.
 *
 * Nothing stored here is a secret. The challenge identifier authenticates
 * nobody — the code is the proof, and the code never leaves the message it was
 * sent in. The masked address is derived from an address the visitor typed. The
 * two timestamps describe metering the server applies whether the browser knows
 * about it or not.
 *
 * `sessionStorage` rather than `localStorage`: a verification challenge is
 * meaningful for one tab for a few minutes, and it should not outlive the
 * browsing session or leak into another one.
 */
const storagePrefix = "gradex.verification.";

/** The query parameter that names the challenge across the navigation. */
export const challengeParameter = "challenge";

function storageKey(challengeId: string): string {
  return storagePrefix + challengeId;
}

function usableStorage(): Storage | null {
  // Private modes and hardened configurations can throw on access rather than
  // return null, and a verification screen must not break because of it.
  try {
    return typeof window === "undefined" ? null : window.sessionStorage;
  } catch {
    return null;
  }
}

export function rememberChallenge(challenge: VerificationChallenge): void {
  const storage = usableStorage();
  if (!storage || !challenge.challenge_id) return;
  try {
    storage.setItem(storageKey(challenge.challenge_id), JSON.stringify(challenge));
  } catch {
    // A full or refused storage is not a failure of the journey. The screen
    // falls back to the identifier in the address bar and asks the server for
    // the rest by offering a resend.
  }
}

export function recallChallenge(challengeId: string | null): VerificationChallenge | null {
  const storage = usableStorage();
  if (!storage || !challengeId) return null;
  let raw: string | null = null;
  try {
    raw = storage.getItem(storageKey(challengeId));
  } catch {
    return null;
  }
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);
    return isChallenge(parsed) && parsed.challenge_id === challengeId ? parsed : null;
  } catch {
    return null;
  }
}

export function forgetChallenge(challengeId: string | null): void {
  const storage = usableStorage();
  if (!storage || !challengeId) return;
  try {
    storage.removeItem(storageKey(challengeId));
  } catch {
    // Nothing to recover: the entry carries no secret and expires on its own.
  }
}

/**
 * Validates a value read back out of storage.
 *
 * Storage is shared with anything else running on this origin, so what comes
 * out of it is input, not state. A malformed entry is treated as absent.
 */
function isChallenge(value: unknown): value is VerificationChallenge {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.challenge_id === "string" &&
    typeof candidate.masked_email === "string" &&
    typeof candidate.expires_at === "string" &&
    typeof candidate.resend_available_at === "string" &&
    typeof candidate.code_length === "number" &&
    typeof candidate.maximum_attempts === "number"
  );
}

/**
 * Seconds remaining until a moment, floored at zero.
 *
 * Returns 0 for an unparseable timestamp so a bad value shows the control as
 * available rather than disabling it forever — the server still enforces the
 * cooldown, so the worst outcome is one refused request.
 */
export function secondsUntil(iso: string, now: number): number {
  const target = Date.parse(iso);
  if (!Number.isFinite(target)) return 0;
  return Math.max(0, Math.ceil((target - now) / 1000));
}
