/**
 * Why protected playback stopped, or would not start.
 *
 * These are the only refusals the player treats as *states* rather than as
 * failures. Everything else keeps the existing "this Lesson has no playable
 * media" dead end, which is deliberately uniform and says nothing about
 * entitlement or Course inventory.
 *
 * None of them carries any fact about the other device. The Student is told
 * their account is busy and what to do about it, which is the whole useful
 * content of the answer.
 */
export type PlaybackBlock =
  | "ANOTHER_DEVICE"
  | "LEASE_LOST"
  | "COORDINATION_UNAVAILABLE"
  | "DEVICE_TRUST_REQUIRED";

/**
 * Classifies a playback refusal.
 *
 * Returns null when the error is not a playback-concurrency or device-trust
 * outcome, so the caller falls through to its existing handling rather than
 * inventing a message for an error it does not understand.
 */
export function playbackBlockOf(error: unknown): PlaybackBlock | null {
  // Read structurally rather than through `instanceof ProblemError`. This
  // module is pure classification with no network or framework dependency, and
  // keeping it that way is what lets it be unit-tested directly. A value that
  // does not carry a problem code simply is not one of these outcomes.
  const code = problemCodeOf(error);
  switch (code) {
    case "PLAYBACK_ALREADY_ACTIVE_ON_ANOTHER_DEVICE":
      return "ANOTHER_DEVICE";
    case "PLAYBACK_LEASE_LOST":
      return "LEASE_LOST";
    case "PLAYBACK_COORDINATION_UNAVAILABLE":
      return "COORDINATION_UNAVAILABLE";
    case "DEVICE_TRUST_REQUIRED":
    case "DEVICE_ADOPTION_REQUIRED":
      return "DEVICE_TRUST_REQUIRED";
    default:
      return null;
  }
}

function problemCodeOf(error: unknown): string | null {
  if (typeof error !== "object" || error === null) return null;
  const problem = (error as { problem?: unknown }).problem;
  if (typeof problem !== "object" || problem === null) return null;
  const code = (problem as { code?: unknown }).code;
  return typeof code === "string" ? code : null;
}

/**
 * Whether a blocked player should offer to try again.
 *
 * A conflict and an expired lease both clear on their own — the other device
 * stops, or its lease lapses — so retrying is meaningful. A browser that has
 * not completed device trust cannot fix anything by retrying and is sent to its
 * device settings instead.
 */
export function blockIsRetryable(block: PlaybackBlock): boolean {
  return block !== "DEVICE_TRUST_REQUIRED";
}

/**
 * The heartbeat cadence, in milliseconds, with a floor.
 *
 * The server publishes the interval it expects; this only refuses to believe a
 * value that would either hammer the API or let the lease lapse between beats.
 */
export function heartbeatIntervalMS(intervalSeconds: number | undefined): number {
  const seconds = intervalSeconds ?? 25;
  if (!Number.isFinite(seconds) || seconds < 5) return 5_000;
  if (seconds > 120) return 120_000;
  return Math.round(seconds * 1000);
}
