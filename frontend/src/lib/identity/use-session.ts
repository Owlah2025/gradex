"use client";

import * as React from "react";
import { getSessionView, subscribeToSession, type SessionView } from "./session";

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
