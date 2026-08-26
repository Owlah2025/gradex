"use client";

import * as React from "react";
import {
  getSessionResolution,
  getSessionView,
  subscribeToSession,
  type SessionResolution,
  type SessionView,
} from "./session";

// The store is memory-only, so a server render has no session to report and
// must return null rather than reading anything request-scoped.
function serverSnapshot(): SessionView | null {
  return null;
}

/**
 * Subscribes a component to the in-memory session.
 *
 * Returns the session view without its CSRF token. Components that need the
 * token for a state-changing request read it through `currentCSRFToken()` at
 * call time instead, so it never enters render state.
 */
export function useSessionView(): SessionView | null {
  return React.useSyncExternalStore(
    subscribeToSession,
    getSessionView,
    serverSnapshot,
  );
}

/**
 * Subscribes a component to whether the session question has been answered.
 *
 * Separate from `useSessionView` because it answers a different question. The view says *who* the
 * principal is, and is `null` both for a visitor and for a page that has not asked yet; this says
 * only whether the asking is done. A component deciding whether a `/me` request is even possible
 * needs the second and must not infer it from the first.
 *
 * A server render has asked nobody, so it reports `UNRESOLVED` rather than guessing.
 */
function serverResolution(): SessionResolution {
  return "UNRESOLVED";
}

export function useSessionResolution(): SessionResolution {
  return React.useSyncExternalStore(
    subscribeToSession,
    getSessionResolution,
    serverResolution,
  );
}
