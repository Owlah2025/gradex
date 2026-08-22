export const passwordMinimum = 15;
export const passwordMaximum = 128;

export function codePointLength(value: string) {
  return Array.from(value).length;
}

function isSupportedLetter(character: string) {
  return (
    /\p{Letter}/u.test(character) &&
    /[\p{Script=Arabic}\p{Script=Latin}]/u.test(character)
  );
}

export function validDisplayName(value: string) {
  const name = value.trim();
  const length = codePointLength(name);
  if (length < 2 || length > 50) return false;
  const characters = Array.from(name);
  const letters = characters.filter(isSupportedLetter);
  return (
    letters.length >= 2 &&
    characters.every(
      (character) =>
        isSupportedLetter(character) ||
        /[\p{Mark}\p{White_Space}'’\-]/u.test(character),
    )
  );
}

export function validPassword(value: string) {
  const length = codePointLength(value);
  return length >= passwordMinimum && length <= passwordMaximum;
}

export function validEmail(value: string) {
  const email = value.trim();
  return (
    email.length <= 320 &&
    !/\s/.test(email) &&
    /^[^@]+@[^@]+\.[^@]+$/.test(email)
  );
}

/**
 * The flows that present a one-time bearer in the URL fragment.
 *
 * Capture is namespaced by purpose because the slots must never be shared. A
 * single module slot would let this happen: open an email-verification link,
 * navigate client-side to password recovery without a document reload, and
 * recovery would read the verification token out of module memory and submit
 * it as a reset secret. The server would refuse it, but the client would have
 * crossed two credential purposes on its own, which is not a boundary to leave
 * to the server.
 */
export type FragmentTokenPurpose =
  | "EMAIL_VERIFICATION"
  | "PASSWORD_RESET"
  | "STAFF_INVITATION"
  | "COURSE_ACCESS_INVITATION";

type FragmentCapture = { token: string | null; spent: boolean };

type CourseAccessInvitationContext = { invitationId: string; token: string };

// This is deliberately session-scoped rather than a query parameter, cookie,
// or persistent local storage. A Student who must register, verify, and sign
// in navigates through several pages; module memory cannot survive those page
// loads, but the fragment bearer must never be placed into a URL.
const courseAccessInvitationContextKey = "gradex.course-access-invitation.v1";

/**
 * Per-purpose capture slots for this document.
 *
 * Module scope, not component state, and deliberately so. Capture must be
 * monotonic: once a token has been seen it must never be forgotten because the
 * URL was successfully cleaned. Component refs cannot provide that — React
 * remounts the component in development, which recreates the refs, so the
 * second mount re-read an already-stripped fragment, found nothing, and
 * dropped the screen into its missing-token state. A module binding lives for
 * the document, which is the lifetime of a one-time link.
 *
 * A missing entry means capture has not run for that purpose. A completed
 * capture is always an object, whose `token` may itself be null when the
 * fragment genuinely carried none.
 */
const fragmentCaptures = new Map<FragmentTokenPurpose, FragmentCapture>();

/**
 * Captures the one-time token from the URL fragment, once per purpose.
 *
 * The fragment is used rather than the query string because a fragment is
 * never sent to the server, so the token cannot reach access logs or a
 * `Referer` header. Both email verification and password recovery present
 * their secrets this way, through separate slots.
 *
 * Capture is separate from scrubbing on purpose. Scrubbing is best-effort and
 * repeated; capture stays unchanged while a bearer is live. After a terminal
 * outcome, a newly navigated link for the same purpose may establish a fresh
 * capture. Coupling capture to every render is what made a successful scrub
 * look like a missing link.
 */
export function captureTokenFromFragment(
  purpose: FragmentTokenPurpose,
): string | null {
  const fragment = new URLSearchParams(window.location.hash.slice(1));
  const fragmentToken = fragment.get("token");
  let capture = fragmentCaptures.get(purpose);
  if (!capture || (capture.spent && fragmentToken)) {
    capture = { token: fragmentToken, spent: false };
    fragmentCaptures.set(purpose, capture);
  }
  return capture.token;
}

/**
 * Drops the raw bearer from module memory after a terminal outcome.
 *
 * Call this only when the secret's fate is settled: a successful consumption,
 * or a definitive refusal such as expired, already used, superseded, or
 * unknown. A raw bearer must not stay resident for the whole document lifetime
 * once it can no longer be used.
 *
 * Do NOT call it for a network error, timeout, abort, or 5xx. Those leave the
 * server-side secret possibly still live, and the holder must be able to retry
 * with the link they were sent.
 */
export function releaseFragmentToken(purpose: FragmentTokenPurpose) {
  const capture = fragmentCaptures.get(purpose);
  if (!capture) return;
  capture.token = null;
  capture.spent = true;
}

export function retainCourseAccessInvitationContext(
  invitationId: string,
  token: string,
) {
  if (!invitationId || !token) return;
  window.sessionStorage.setItem(
    courseAccessInvitationContextKey,
    JSON.stringify({
      invitationId,
      token,
    } satisfies CourseAccessInvitationContext),
  );
}

export function restoreCourseAccessInvitationContext(invitationId: string) {
  const serialized = window.sessionStorage.getItem(
    courseAccessInvitationContextKey,
  );
  if (!serialized) return null;
  try {
    const context = JSON.parse(
      serialized,
    ) as Partial<CourseAccessInvitationContext>;
    if (
      context.invitationId === invitationId &&
      typeof context.token === "string" &&
      context.token.length > 0
    ) {
      return context.token;
    }
  } catch {
    // A malformed browser-local value is not invitation authority.
  }
  window.sessionStorage.removeItem(courseAccessInvitationContextKey);
  return null;
}

export function releaseCourseAccessInvitationContext() {
  window.sessionStorage.removeItem(courseAccessInvitationContextKey);
}

/**
 * Whether this purpose's token has reached a terminal outcome in this document.
 *
 * Kept alongside capture for the same monotonicity reason: a remount must not
 * offer a fresh form for a link that is already settled.
 */
export function isFragmentTokenSpent(purpose: FragmentTokenPurpose) {
  return fragmentCaptures.get(purpose)?.spent === true;
}

/**
 * Removes the token from the address bar, and keeps it removed.
 *
 * One `replaceState` is not enough under the App Router: the router
 * re-synchronises the address bar from its own snapshot during hydration,
 * which put the token back after it had been removed. That was observed on
 * both the recovery and verification screens, leaving the secret in the
 * address bar and so in history and in anything the reader might screenshot or
 * share.
 *
 * This re-asserts on a short bounded interval and stops once the address bar
 * has stayed clean. It never touches the captured token, so a scrub that
 * succeeds — or one that never manages to — cannot change what the screen
 * shows.
 */
export function scrubTokenFragment() {
  scrubOnce();
  if (typeof window.setInterval !== "function") return;
  const intervalMs = 100;
  const limitMs = 3000;
  let elapsed = 0;
  let clean = 0;
  const timer = window.setInterval(() => {
    elapsed += intervalMs;
    if (window.location.hash) {
      scrubOnce();
      clean = 0;
    } else {
      clean += 1;
    }
    if (clean >= 5 || elapsed >= limitMs) window.clearInterval(timer);
  }, intervalMs);
}

function scrubOnce() {
  if (!window.location.hash) return;
  window.history.replaceState(
    window.history.state,
    "",
    `${window.location.pathname}${window.location.search}`,
  );
}
