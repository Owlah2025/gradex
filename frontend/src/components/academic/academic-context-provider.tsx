"use client";

import * as React from "react";
import {
  academicContext,
  browserContextStorage,
  clearAcademicContext,
  readAcademicContext,
  resolveAcademicSource,
  sameAcademicContext,
  validateAcademicContext,
  writeAcademicContext,
  type AcademicContextNames,
  type AnonymousAcademicContext,
  type ResolvedAcademicSource,
} from "@/lib/academic/anonymous-context";
import {
  getAcademicProfile,
  type AcademicProfile,
} from "@/lib/api/academic-profile";
import { useSessionView } from "@/lib/identity/use-session";

/**
 * The visitor's academic context, held once for the whole application.
 *
 * Two things are joined here and nowhere else: the browsing preference a visitor can set before
 * they have an account, and the academic profile a signed-in Student's account already holds. They
 * are kept separate in the value — `anonymous` and `profile` are distinct fields, and `source` says
 * which one currently outranks the other — precisely so no screen can mistake one for the other.
 * Nothing in this provider ever writes to the profile.
 *
 * ## Hydration
 *
 * `localStorage` does not exist during the server render, so the first client render must produce
 * exactly what the server produced: `status: "loading"` with no context. The stored value is read
 * in an effect afterwards, the same shape `LocaleProvider` already uses for the saved language.
 * Consumers render their "still deciding" state while `status` is `"loading"` and are never allowed
 * to guess.
 */

export type AcademicContextValue = {
  /** `"loading"` until the browser's stored value has been read. Never trust `anonymous` before then. */
  status: "loading" | "ready";
  /** The preference this browser is holding, or `null`. Never an account fact. */
  anonymous: AnonymousAcademicContext | null;
  /** The signed-in Student's own profile, or `null` for everyone else. Authoritative when complete. */
  profile: AcademicProfile | null;
  /** Which of the two the product should act on right now. */
  source: ResolvedAcademicSource;
  /** Replace the browsing preference. Writes through to storage immediately. */
  setAnonymous: (context: AnonymousAcademicContext | null) => void;
  /** Forget the browsing preference entirely, in memory and in storage. */
  clearAnonymous: () => void;
  /**
   * Correct a stored context against option lists that have just loaded.
   *
   * Pass `null` for a list that has not arrived — an in-flight request is not evidence that a
   * university has disappeared, and treating it as such would wipe a valid context on a slow
   * connection.
   */
  reconcile: (
    institutionSlugs: readonly string[] | null,
    programSlugs: readonly string[] | null,
  ) => void;
  /** Refresh the non-authoritative display cache from live option data. Identity is untouched. */
  refreshNames: (names: Partial<AcademicContextNames>) => void;
};

const AcademicContext = React.createContext<AcademicContextValue | null>(null);

export function AcademicContextProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const session = useSessionView();
  const [status, setStatus] = React.useState<"loading" | "ready">("loading");
  const [anonymous, setAnonymousState] =
    React.useState<AnonymousAcademicContext | null>(null);
  const [profile, setProfile] = React.useState<AcademicProfile | null>(null);

  // Read after mount, never during render: the server has no localStorage, and reading it in the
  // first client render is what produces a hydration mismatch.
  React.useEffect(() => {
    setAnonymousState(readAcademicContext(browserContextStorage()));
    setStatus("ready");
  }, []);

  // Only a Student has an academic profile, and only a signed-in one can be asked for it. Gating on
  // the session keeps the public landing page from issuing a request that can only ever 401.
  const isStudent = session?.role === "STUDENT";
  React.useEffect(() => {
    if (!isStudent) {
      setProfile(null);
      return;
    }
    let cancelled = false;
    getAcademicProfile("en")
      .then((loaded) => {
        if (!cancelled) setProfile(loaded);
      })
      .catch(() => {
        // A profile that cannot be read is not a profile that says something. Falling back to the
        // browsing preference is the safe direction: it narrows discovery and nothing else.
        if (!cancelled) setProfile(null);
      });
    return () => {
      cancelled = true;
    };
  }, [isStudent]);

  const setAnonymous = React.useCallback(
    (next: AnonymousAcademicContext | null) => {
      const normalized =
        next === null || next.institutionSlug === "" ? null : next;
      writeAcademicContext(browserContextStorage(), normalized);
      setAnonymousState(normalized);
    },
    [],
  );

  const clearAnonymous = React.useCallback(() => {
    clearAcademicContext(browserContextStorage());
    setAnonymousState(null);
  }, []);

  const reconcile = React.useCallback(
    (
      institutionSlugs: readonly string[] | null,
      programSlugs: readonly string[] | null,
    ) => {
      setAnonymousState((current) => {
        const validated = validateAcademicContext(
          current,
          institutionSlugs,
          programSlugs,
        );
        // Identity comparison, so a display-name refresh alone never rewrites storage.
        if (sameAcademicContext(current, validated)) return current;
        writeAcademicContext(browserContextStorage(), validated);
        return validated;
      });
    },
    [],
  );

  const refreshNames = React.useCallback(
    (names: Partial<AcademicContextNames>) => {
      setAnonymousState((current) => {
        if (current === null) return current;
        const merged = { ...current.names, ...names };
        const unchanged = (
          Object.keys(merged) as (keyof AcademicContextNames)[]
        ).every((key) => merged[key] === current.names[key]);
        if (unchanged) return current;
        const next = { ...current, names: merged };
        writeAcademicContext(browserContextStorage(), next);
        return next;
      });
    },
    [],
  );

  const value = React.useMemo<AcademicContextValue>(
    () => ({
      status,
      anonymous,
      profile,
      source: resolveAcademicSource({
        anonymous,
        profileSetupState: profile?.setup_state ?? null,
      }),
      setAnonymous,
      clearAnonymous,
      reconcile,
      refreshNames,
    }),
    [
      status,
      anonymous,
      profile,
      setAnonymous,
      clearAnonymous,
      reconcile,
      refreshNames,
    ],
  );

  return (
    <AcademicContext.Provider value={value}>
      {children}
    </AcademicContext.Provider>
  );
}

export function useAcademicContext(): AcademicContextValue {
  const value = React.useContext(AcademicContext);
  if (!value)
    throw new Error(
      "useAcademicContext must be used within an AcademicContextProvider",
    );
  return value;
}

/** Re-exported so consumers build a context without also importing the storage module. */
export { academicContext };
