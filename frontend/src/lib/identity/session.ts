import type { SessionRole } from "./return-to";

/**
 * The authenticated representation returned by the session routes.
 *
 * `csrf_token` is a secret. It is held in memory for the lifetime of the page
 * and is never written to `localStorage`, `sessionStorage`, a cookie, a URL, or
 * a log line. The opaque session credential itself never reaches JavaScript at
 * all; it lives only in the `__Host-` cookie the server sets.
 */
export type AuthenticatedSession = {
  status: "AUTHENTICATED";
  role: SessionRole;
  display_name: string;
  /**
   * The principal is authenticated but restricted: the server refuses it every
   * capability except changing its password and signing out.
   *
   * This exists so the browser can send the visitor to the mandatory
   * password-change screen instead of walking into a wall of 403s. It is
   * derived server-side from the credential state and carries no secret, so it
   * is safe in the rendering view.
   *
   * Optional in the type because a response from an older server would omit
   * it. `passwordChangeRequired()` treats a missing value as "not restricted",
   * which is the same behaviour as before the field existed.
   */
  password_change_required?: boolean;
  csrf_token: string;
  idle_expires_at: string;
  absolute_expires_at: string;
};

/**
 * The session view safe to pass into rendering. Carries no secret.
 *
 * `password_change_required` is narrowed to a required boolean: `viewOf`
 * normalizes it, so a consumer never has to distinguish "false" from "absent".
 */
export type SessionView = Omit<
  AuthenticatedSession,
  "csrf_token" | "password_change_required"
> & { password_change_required: boolean };

/**
 * Whether this page load has learned yet whether anyone is signed in.
 *
 * `SessionView | null` cannot say this. `null` means both "no session" and "not asked yet", and a
 * caller that treats the second as the first acts on an answer nobody has given. That distinction
 * is the difference between two real defects: gating work on a *classified role* skips it during
 * rehydration, and gating it on `view === null` performs it for visitors it can never apply to.
 *
 * This is deliberately not the same axis as `role`. It says only that the question was asked and
 * answered, never who the principal turned out to be — so a session whose role is missing or
 * outside the known set is still `AUTHENTICATED` here, and callers decide for themselves what an
 * unclassifiable principal may do.
 *
 * It carries no secret and adds no authority: it is an observation about a request that has already
 * happened.
 */
export type SessionResolution = "UNRESOLVED" | "ANONYMOUS" | "AUTHENTICATED";

type Listener = (view: SessionView | null) => void;

let current: AuthenticatedSession | null = null;
let resolution: SessionResolution = "UNRESOLVED";
// Cached so repeated reads return the same reference. React's
// useSyncExternalStore compares snapshots by identity and would loop forever
// if this rebuilt an equal object on every call.
let currentView: SessionView | null = null;
const listeners = new Set<Listener>();

function viewOf(session: AuthenticatedSession | null): SessionView | null {
  if (!session) return null;
  // Destructured rather than spread-and-delete so a future field added to the
  // session type cannot silently leak through this boundary.
  const {
    status,
    role,
    display_name,
    idle_expires_at,
    absolute_expires_at,
  } = session;
  return {
    status,
    role,
    display_name,
    // Normalized to a boolean here so every consumer — the redirect guard, the
    // navigation, a test — reads the same shape and no caller has to remember
    // that the field can be absent.
    password_change_required: session.password_change_required === true,
    idle_expires_at,
    absolute_expires_at,
  };
}

function publish(session: AuthenticatedSession | null) {
  current = session;
  currentView = viewOf(session);
  for (const listener of listeners) listener(currentView);
}

/** Replaces the in-memory session and notifies subscribers. */
export function setSession(session: AuthenticatedSession): void {
  resolution = "AUTHENTICATED";
  publish(session);
}

/**
 * Drops the in-memory session, including its CSRF token.
 *
 * Every caller of this reaches it having *learned* that there is no session: the rehydrator whose
 * resolve call came back without one, and sign-out. So it is the moment the question is answered
 * in the negative, and the resolution moves off `UNRESOLVED` even though the view was already null.
 */
export function clearSession(): void {
  resolution = "ANONYMOUS";
  publish(null);
}

/** The current session without its secret, or null when signed out. */
export function getSessionView(): SessionView | null {
  return currentView;
}

/**
 * Whether the session question has been answered, and how.
 *
 * Returned as a plain string so `useSyncExternalStore` can compare snapshots by value. That matters
 * for the `UNRESOLVED` → `ANONYMOUS` transition specifically: the view is `null` on both sides of
 * it, so a subscriber watching only the view sees an identical snapshot and never re-renders.
 */
export function getSessionResolution(): SessionResolution {
  return resolution;
}

/**
 * The current CSRF token, or null when signed out.
 *
 * Callers pass this straight into a request header. It must not be stored,
 * rendered, or included in an error message.
 */
export function currentCSRFToken(): string | null {
  return current?.csrf_token ?? null;
}

/** Subscribes to session changes. Returns an unsubscribe function. */
export function subscribeToSession(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Test-only reset so one module instance cannot leak state between cases. */
export function resetSessionForTest(): void {
  current = null;
  currentView = null;
  resolution = "UNRESOLVED";
  listeners.clear();
}
