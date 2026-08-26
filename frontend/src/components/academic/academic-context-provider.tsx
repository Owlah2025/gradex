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
import {
  getPublicInstitutions,
  getPublicPrograms,
} from "@/lib/api/public-catalog";

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

/**
 * ## Events worth measuring, if and when a frontend analytics abstraction exists
 *
 * There is none today — no `track`, no provider, no vendor SDK anywhere in `src` — and this tranche
 * deliberately does not introduce one. What the funnel would need is recorded here, next to the
 * transitions that would emit it, so the work is a matter of adding calls rather than rediscovering
 * where the moments are:
 *
 *  - `academic_selection_started` — the picker is shown to a visitor with no context
 *  - `institution_selected` / `program_selected` — each chooser resolves, with the **slug**, never
 *    the localized name
 *  - `academic_context_set` — `setAnonymous` commits a context, with whether it came from the
 *    landing panel or the catalogue filter row
 *  - `academic_context_changed` — `setAnonymous` replaces a different existing context
 *  - `academic_context_cleared` — `clearAnonymous`, or a `navigate` that empties the selection
 *  - `academic_context_invalidated` — `reconcile` drops a retired institution or program
 *  - `academic_context_restored` — the catalogue seeds a bare URL from a remembered context, which
 *    is the measurement of whether persistence is earning its place
 *  - `academic_profile_handoff_shown` — a signed-in Student is asked to confirm their earlier
 *    choice, which is the size of the gap the missing slug-to-identifier contract leaves
 *
 * Slugs and counts only. A university name is display prose in two languages and belongs in no
 * event payload.
 */

export type AcademicContextValue = {
  /**
   * `"loading"` until *both* questions are settled: what this browser has stored, and whether the
   * visitor has an academic profile of their own.
   *
   * Both, deliberately. The stored value arrives in a mount effect and the profile arrives over the
   * network, so a status that meant only "storage read" let the catalogue decide precedence while
   * the profile was still in flight — and a Student with a real saved profile had their browsing
   * preference applied as filters a moment before the profile that outranks it turned up.
   */
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
  /**
   * Refresh the non-authoritative display cache from live option data.
   *
   * Each part carries the slug it describes and is applied only if that slug is the one currently
   * stored. A name written beside a different slug would be a label describing something else,
   * which is the one thing this cache must never become.
   */
  refreshNames: (update: AcademicNameUpdate) => void;
};

export type AcademicNameUpdate = {
  institution?: { slug: string; nameAr: string; nameEn: string };
  program?: { slug: string; nameAr: string; nameEn: string };
};

const AcademicContext = React.createContext<AcademicContextValue | null>(null);

export function AcademicContextProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const [storageRead, setStorageRead] = React.useState(false);
  const [profileSettled, setProfileSettled] = React.useState(false);
  const [anonymous, setAnonymousState] =
    React.useState<AnonymousAcademicContext | null>(null);
  const [profile, setProfile] = React.useState<AcademicProfile | null>(null);

  // Read after mount, never during render: the server has no localStorage, and reading it in the
  // first client render is what produces a hydration mismatch.
  React.useEffect(() => {
    setAnonymousState(readAcademicContext(browserContextStorage()));
    setStorageRead(true);
  }, []);

  /**
   * Asks for the visitor's own academic profile, once per page load.
   *
   * Deliberately not gated on the session's role. `useSessionView` reports `null` both for "not
   * signed in" and for "not rehydrated yet", and those are not the same answer: gating on it meant
   * the profile request was skipped during the rehydration window, precedence was decided without
   * it, and a Student's saved profile lost to a browsing preference it should have outranked.
   *
   * A 401 is the ordinary answer here for an anonymous visitor, exactly as it already is for the
   * access history the public Course page reads. It is one request per load — the same one the
   * catalogue used to make on its own — and it never surfaces as an error or hides a Course.
   */
  React.useEffect(() => {
    let cancelled = false;
    getAcademicProfile("en")
      .then((loaded) => {
        if (cancelled) return;
        setProfile(loaded);
        setProfileSettled(true);
      })
      .catch(() => {
        // A profile that cannot be read is not a profile that says something. Falling back to the
        // browsing preference is the safe direction: it narrows discovery and nothing else.
        if (cancelled) return;
        setProfile(null);
        setProfileSettled(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const status: "loading" | "ready" =
    storageRead && profileSettled ? "ready" : "loading";

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

  const refreshNames = React.useCallback((update: AcademicNameUpdate) => {
    setAnonymousState((current) => {
      if (current === null) return current;
      const merged: AcademicContextNames = { ...current.names };
      if (update.institution?.slug === current.institutionSlug) {
        merged.institutionAr = update.institution.nameAr;
        merged.institutionEn = update.institution.nameEn;
      }
      if (
        current.programSlug !== "" &&
        update.program?.slug === current.programSlug
      ) {
        merged.programAr = update.program.nameAr;
        merged.programEn = update.program.nameEn;
      }
      const unchanged = (
        Object.keys(merged) as (keyof AcademicContextNames)[]
      ).every((key) => merged[key] === current.names[key]);
      if (unchanged) return current;
      const next = { ...current, names: merged };
      writeAcademicContext(browserContextStorage(), next);
      return next;
    });
  }, []);

  /**
   * Fills an empty display cache from the public option lists.
   *
   * A context adopted straight from a URL — a shared link, a bookmark — arrives with slugs and no
   * names, and a slug must never be the thing a reader is asked to recognise. One anonymous
   * catalogue read fixes that everywhere at once, and it runs only while the cache is actually
   * empty. Both languages come back in the same response, so a locale switch needs no second call.
   */
  const institutionSlug = anonymous?.institutionSlug ?? "";
  const programSlug = anonymous?.programSlug ?? "";
  const namesMissing =
    anonymous !== null &&
    (anonymous.names.institutionEn === "" ||
      (anonymous.programSlug !== "" && anonymous.names.programEn === ""));
  React.useEffect(() => {
    if (!namesMissing || institutionSlug === "") return;
    let cancelled = false;
    void (async () => {
      try {
        const institutions = await getPublicInstitutions("en");
        const institution = institutions.find(
          (item) => item.slug === institutionSlug,
        );
        if (cancelled || !institution) return;
        refreshNames({
          institution: {
            slug: institution.slug,
            nameAr: institution.name_ar,
            nameEn: institution.name_en,
          },
        });
        if (programSlug === "") return;
        const programs = await getPublicPrograms(institutionSlug, "en");
        const program = programs.find((item) => item.slug === programSlug);
        if (cancelled || !program) return;
        refreshNames({
          program: {
            slug: program.slug,
            nameAr: program.name_ar,
            nameEn: program.name_en,
          },
        });
      } catch {
        // A name the catalogue cannot supply is a cosmetic loss, not a failure. The identity is
        // intact and every surface still renders; only the label falls back.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [namesMissing, institutionSlug, programSlug, refreshNames]);

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
