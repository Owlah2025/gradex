"use client";

import * as React from "react";
import {
  getPublicInstitutions,
  getPublicPrograms,
  type InstitutionOption,
  type ProgramOption,
} from "@/lib/api/public-catalog";

/**
 * The two anonymous option lists, and the dependence between them.
 *
 * Lifted out of a component so the rule that matters lives in one place: programs belong to an
 * institution, are re-fetched whenever it changes, and a request that has been superseded never
 * writes its result. Nothing here can offer a program that does not belong to the selected
 * university, and nothing here decides what a selection *means* — that stays with
 * `AcademicContextProvider`, which owns the visitor's actual context.
 *
 * Both lists come from the public catalogue endpoints the catalogue itself uses. No fixture, no
 * second source of academic truth.
 */

export type OptionsState<T> =
  | { kind: "loading" }
  | { kind: "ready"; items: T[] }
  | { kind: "failed" };

export type AcademicOptions = {
  institutions: OptionsState<InstitutionOption>;
  /** `idle` until an institution is chosen — there is nothing to ask for before that. */
  programs: OptionsState<ProgramOption> | { kind: "idle" };
  retryInstitutions: () => void;
  retryPrograms: () => void;
};

export function useAcademicOptions(
  language: "ar" | "en",
  institutionSlug: string,
): AcademicOptions {
  const [institutions, setInstitutions] = React.useState<
    OptionsState<InstitutionOption>
  >({ kind: "loading" });
  const [programs, setPrograms] = React.useState<
    OptionsState<ProgramOption> | { kind: "idle" }
  >({ kind: "idle" });
  const [institutionAttempt, setInstitutionAttempt] = React.useState(0);
  const [programAttempt, setProgramAttempt] = React.useState(0);

  React.useEffect(() => {
    let cancelled = false;
    setInstitutions({ kind: "loading" });
    getPublicInstitutions(language)
      .then((items) => {
        if (!cancelled) setInstitutions({ kind: "ready", items });
      })
      .catch(() => {
        if (!cancelled) setInstitutions({ kind: "failed" });
      });
    return () => {
      cancelled = true;
    };
  }, [language, institutionAttempt]);

  React.useEffect(() => {
    if (institutionSlug === "") {
      setPrograms({ kind: "idle" });
      return;
    }
    let cancelled = false;
    setPrograms({ kind: "loading" });
    getPublicPrograms(institutionSlug, language)
      .then((items) => {
        if (!cancelled) setPrograms({ kind: "ready", items });
      })
      .catch(() => {
        if (!cancelled) setPrograms({ kind: "failed" });
      });
    return () => {
      cancelled = true;
    };
  }, [institutionSlug, language, programAttempt]);

  const retryInstitutions = React.useCallback(
    () => setInstitutionAttempt((count) => count + 1),
    [],
  );
  const retryPrograms = React.useCallback(
    () => setProgramAttempt((count) => count + 1),
    [],
  );

  return { institutions, programs, retryInstitutions, retryPrograms };
}
