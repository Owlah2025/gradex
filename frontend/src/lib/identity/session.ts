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

type Listener = (view: SessionView | null) => void;

let current: AuthenticatedSession | null = null;
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
  publish(session);
}

/** Drops the in-memory session, including its CSRF token. */
export function clearSession(): void {
  publish(null);
}

/** The current session without its secret, or null when signed out. */
export function getSessionView(): SessionView | null {
  return currentView;
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
  listeners.clear();
}
