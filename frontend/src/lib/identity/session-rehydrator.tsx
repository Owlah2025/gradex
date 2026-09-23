"use client";

import * as React from "react";
import { getSession } from "@/lib/api/identity";
import { useLocale } from "@/lib/i18n/locale-provider";
import { clearSession, setSession } from "./session";

/**
 * Restores the in-memory session once per page load.
 *
 * The session credential lives in an `HttpOnly` cookie that JavaScript cannot
 * read, and the CSRF token is deliberately never persisted, so after a reload
 * the browser has authority it cannot see. One resolve call rehydrates both the
 * display state and the memory-only token. This read does not rotate the
 * credential or extend idle expiry.
 *
 * A failure here means "not signed in" and must stay silent: the resolve route
 * returns the same shape for a missing, expired, revoked, and never-existing
 * credential, and surfacing it would turn a normal guest visit into an error.
 */
export function SessionRehydrator() {
  const { locale } = useLocale();

  React.useEffect(() => {
    let active = true;
    getSession(locale)
      .then((session) => {
        if (active) setSession(session);
      })
      .catch(() => {
        if (active) clearSession();
      });
    return () => {
      active = false;
    };
    // Runs once per load. The locale only changes response copy, and re-running
    // on a language toggle would issue a pointless second resolve.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return null;
}
