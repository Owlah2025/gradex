"use client";

import * as React from "react";
import {
  getPublicInstitutions,
  getPublicPrograms,
  type InstitutionOption,
  type ProgramOption,
} from "@/lib/api/public-catalog";
import {
  institutionName,
  programContext,
  programName,
} from "@/components/catalog/academic-filter-state";
import { academicContext } from "@/lib/academic/anonymous-context";
import type { AnonymousAcademicContext } from "@/lib/academic/anonymous-context";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Select } from "@/components/ui/select";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";

/**
 * Choosing a university and a program, before there is an account.
 *
 * ## Why two native selects and not a combobox
 *
 * The launch catalogue holds one institution and five programs. A search-and-filter listbox at that
 * volume adds a widget to learn, a keyboard model to reimplement, a popover to anchor correctly in
 * RTL, and a mobile experience worse than the platform's own picker — in exchange for filtering a
 * list that fits on screen. Native `<select>` keeps type-ahead, the iOS/Android wheel, screen-reader
 * naming, and RTL alignment for free, which is what this control actually needs.
 *
 * That is a decision about *this* data, not a principle. If the institution list grows past what a
 * reader can scan, this is the component that should gain a searchable listbox, and the identity it
 * produces — a pair of slugs — would not change.
 *
 * ## Dependence
 *
 * Programs are fetched per institution and re-fetched whenever it changes, and any program held
 * from a previous institution is dropped at the same moment rather than carried into a combination
 * the option list cannot render. Nothing here can offer a program that does not belong to the
 * selected university.
 */

type ProgramState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "ready"; items: ProgramOption[] }
  | { kind: "failed" };

type InstitutionState =
  | { kind: "loading" }
  | { kind: "ready"; items: InstitutionOption[] }
  | { kind: "failed" };

export function AcademicContextPicker({
  idPrefix,
  initial,
  submitLabel,
  onSubmit,
  onSkip,
  skipLabel,
  autoFocus = false,
}: {
  /** Distinguishes this instance's control ids, so two pickers can coexist on one page. */
  idPrefix: string;
  initial: AnonymousAcademicContext | null;
  submitLabel: string;
  onSubmit: (context: AnonymousAcademicContext) => void;
  /** Omitted where there is nothing to skip to — the catalogue's own change control, for instance. */
  onSkip?: () => void;
  skipLabel?: string;
  autoFocus?: boolean;
}) {
  const { locale, t } = useLocale();
  const copy = t.academicContext;
  const language = locale as "ar" | "en";

  const [institutions, setInstitutions] = React.useState<InstitutionState>({
    kind: "loading",
  });
  const [programs, setPrograms] = React.useState<ProgramState>({ kind: "idle" });
  const [institutionSlug, setInstitutionSlug] = React.useState(
    initial?.institutionSlug ?? "",
  );
  const [programSlug, setProgramSlug] = React.useState(initial?.programSlug ?? "");
  const [attempt, setAttempt] = React.useState(0);
  const [programAttempt, setProgramAttempt] = React.useState(0);
  const institutionRef = React.useRef<HTMLSelectElement>(null);

  React.useEffect(() => {
    let cancelled = false;
    setInstitutions({ kind: "loading" });
    getPublicInstitutions(language)
      .then((items) => {
        if (cancelled) return;
        setInstitutions({ kind: "ready", items });
        // One launch institution: choosing it removes a step that has only one answer. Derived from
        // the response, never from a hardcoded slug, so a second university changes this by itself.
        setInstitutionSlug((current) =>
          current === "" && items.length === 1 ? items[0].slug : current,
        );
      })
      .catch(() => {
        if (!cancelled) setInstitutions({ kind: "failed" });
      });
    return () => {
      cancelled = true;
    };
  }, [language, attempt]);

  React.useEffect(() => {
    let cancelled = false;
    if (institutionSlug === "") {
      setPrograms({ kind: "idle" });
      return;
    }
    setPrograms({ kind: "loading" });
    getPublicPrograms(institutionSlug, language)
      .then((items) => {
        if (cancelled) return;
        setPrograms({ kind: "ready", items });
        // A program carried in from storage or from a previous university survives only if this
        // university actually offers it. Anything else is dropped rather than silently submitted.
        setProgramSlug((current) =>
          current !== "" && !items.some((item) => item.slug === current)
            ? ""
            : current,
        );
      })
      .catch(() => {
        if (!cancelled) setPrograms({ kind: "failed" });
      });
    return () => {
      cancelled = true;
    };
  }, [institutionSlug, language, programAttempt]);

  React.useEffect(() => {
    if (autoFocus) institutionRef.current?.focus();
  }, [autoFocus]);

  function changeInstitution(next: string) {
    setInstitutionSlug(next);
    // Cleared in the same update as the parent, so no render ever shows a program belonging to a
    // university that is no longer selected.
    setProgramSlug("");
  }

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (institutionSlug === "") return;
    const institution =
      institutions.kind === "ready"
        ? institutions.items.find((item) => item.slug === institutionSlug)
        : undefined;
    const program =
      programs.kind === "ready"
        ? programs.items.find((item) => item.slug === programSlug)
        : undefined;
    // Both languages are cached together: the identity is the slug pair and has to survive a locale
    // switch, so a single-language label cache would have to be discarded at exactly that moment.
    onSubmit(
      academicContext(institutionSlug, programSlug, {
        institutionAr: institution?.name_ar ?? "",
        institutionEn: institution?.name_en ?? "",
        programAr: program?.name_ar ?? "",
        programEn: program?.name_en ?? "",
      }),
    );
  }

  const institutionID = `${idPrefix}-institution`;
  const programID = `${idPrefix}-program`;
  const noInstitutions =
    institutions.kind === "ready" && institutions.items.length === 0;
  const noPrograms = programs.kind === "ready" && programs.items.length === 0;

  if (institutions.kind === "loading") {
    return <LoadingState label={copy.loading} testID="academic-picker-loading" />;
  }

  if (institutions.kind === "failed") {
    return (
      <ErrorState
        testID="academic-picker-error"
        title={copy.loadFailed}
        retryLabel={copy.retry}
        onRetry={() => setAttempt((count) => count + 1)}
      />
    );
  }

  if (noInstitutions) {
    return (
      <p role="status" className="text-sm text-muted-foreground" data-testid="academic-picker-empty">
        {copy.noInstitutions}
      </p>
    );
  }

  return (
    <form onSubmit={submit} data-testid="academic-picker" className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={copy.universityLabel} htmlFor={institutionID}>
          <Select
            id={institutionID}
            ref={institutionRef}
            value={institutionSlug}
            onChange={(event) => changeInstitution(event.target.value)}
            data-testid="academic-picker-institution"
          >
            <option value="">{copy.chooseUniversity}</option>
            {institutions.items.map((option) => (
              <option key={option.slug} value={option.slug}>
                {institutionName(option, language)}
              </option>
            ))}
          </Select>
        </Field>

        <Field
          label={copy.programLabel}
          htmlFor={programID}
          hint={
            institutionSlug === ""
              ? copy.chooseUniversity
              : programs.kind === "loading"
                ? copy.loadingPrograms
                : noPrograms
                  ? copy.noPrograms
                  : copy.programOptional
          }
        >
          <Select
            id={programID}
            value={programSlug}
            disabled={institutionSlug === "" || programs.kind !== "ready" || noPrograms}
            onChange={(event) => setProgramSlug(event.target.value)}
            data-testid="academic-picker-program"
          >
            <option value="">{copy.anyProgram}</option>
            {programs.kind === "ready" &&
              programs.items.map((option) => {
                const college = programContext(option, language);
                return (
                  <option key={option.slug} value={option.slug}>
                    {programName(option, language)}
                    {college === "" ? "" : ` · ${college}`}
                  </option>
                );
              })}
          </Select>
        </Field>
      </div>

      {/* A failed program list is recoverable on its own: the university is still chosen and its
          courses are still reachable, so this refuses only the one request that failed. */}
      {programs.kind === "failed" && (
        <ErrorState
          testID="academic-picker-programs-error"
          title={copy.programsFailed}
          retryLabel={copy.retry}
          onRetry={() => setProgramAttempt((count) => count + 1)}
        />
      )}

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <Button type="submit" disabled={institutionSlug === ""}>
          {submitLabel}
        </Button>
        {onSkip && skipLabel ? (
          <Button type="button" variant="ghost" onClick={onSkip}>
            {skipLabel}
          </Button>
        ) : null}
      </div>
    </form>
  );
}
