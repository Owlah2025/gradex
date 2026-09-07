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
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { ChoiceChip, ChoiceGrid, ContextQuestion } from "./context-question";
import { SelectedAnswer } from "./selected-academic-context";

/**
 * Choosing a university and a program, before there is an account.
 *
 * ## Why this is not a form any more
 *
 * It was two native `<select>`s and a Submit button, and the reasoning for the selects still holds
 * for a *filter row* — but this is the first thing a visitor is asked on the landing page, and
 * there the shape was wrong. Two dropdowns and a submit is a form: it asks for everything at once,
 * it says nothing back until it is completed, and it puts a button between the reader and the
 * courses they came for.
 *
 * What replaced it asks one question at a time and answers immediately. The options are the same
 * options, from the same two endpoints, producing the same slug pair — nothing about the identity
 * this component yields has changed. Only the number of decisions held open at once has.
 *
 * There is no Submit because there is nothing left to submit: a program is the last fact needed, so
 * choosing one *is* the completion. `onResolve` fires from the choice itself.
 *
 * ## Dependence
 *
 * Unchanged and still the load-bearing rule. Programs are fetched per institution and re-fetched
 * whenever it changes, and any program held from a previous institution is dropped in the same
 * update rather than carried into a combination the option list cannot render. Nothing here can
 * offer a program that does not belong to the selected university.
 *
 * ## The single-university case
 *
 * The launch catalogue holds one institution. Asking a question with one answer is friction with no
 * information in it, so it is answered on the reader's behalf — derived from the response, never
 * from a hardcoded slug — and shown as an answered step rather than hidden. The reader still sees
 * where Gradex thinks they study; they are simply not asked to type it. A second university turns
 * the step back into a real question by itself, with no code change.
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
  onResolve,
  onSkip,
  skipLabel,
  autoFocus = false,
}: {
  /** Distinguishes this instance's control ids, so two pickers can coexist on one page. */
  idPrefix: string;
  initial: AnonymousAcademicContext | null;
  /** Fires the moment the context is complete. There is no separate submit. */
  onResolve: (context: AnonymousAcademicContext) => void;
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
  const [attempt, setAttempt] = React.useState(0);
  const [programAttempt, setProgramAttempt] = React.useState(0);
  /**
   * Whether the reader has moved past the first question in *this* visit to the picker.
   *
   * It is what the reveal animation is keyed on. Without it, a returning visitor who presses
   * "Change" watches the program question animate in as though they had just answered the
   * university one, and a single-university catalogue plays the reveal on first paint for a step
   * nobody took.
   */
  const [advanced, setAdvanced] = React.useState(false);
  const rootRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    let cancelled = false;
    setInstitutions({ kind: "loading" });
    getPublicInstitutions(language)
      .then((items) => {
        if (cancelled) return;
        setInstitutions({ kind: "ready", items });
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
        if (!cancelled) setPrograms({ kind: "ready", items });
      })
      .catch(() => {
        if (!cancelled) setPrograms({ kind: "failed" });
      });
    return () => {
      cancelled = true;
    };
  }, [institutionSlug, language, programAttempt]);

  /**
   * Focus follows the reader into whichever question is actually open.
   *
   * `autoFocus` here means "the reader asked for this back" — from the panel's own Change control or
   * from the one above the results — and two things about that are easy to get wrong.
   *
   * The step waiting for them is not always the first one: a visitor who already named their
   * university reopens on the *program* question, so focusing the university chooser would target
   * markup that is not rendered. The active question is the one carrying the group role in either
   * case, so that is what is asked for rather than a step named by index.
   *
   * And the questions are not on screen when this component mounts. Reopening remounts it into its
   * loading state, so a focus attempt that ran only on mount reached a group that did not exist yet
   * and left focus on the document — the reader was scrolled to a control they then had to find by
   * tabbing from the top of the page. So it waits for the options, and the ref makes it happen
   * exactly once per request rather than on every subsequent load.
   */
  const focusClaimed = React.useRef(false);
  React.useEffect(() => {
    if (!autoFocus) {
      focusClaimed.current = false;
      return;
    }
    if (focusClaimed.current) return;
    const target = rootRef.current?.querySelector<HTMLElement>('[role="group"] button');
    if (!target) return;
    focusClaimed.current = true;
    target.focus();
  }, [autoFocus, institutions.kind, programs.kind]);

  const chosen =
    institutions.kind === "ready"
      ? institutions.items.find((item) => item.slug === institutionSlug)
      : undefined;

  /**
   * Completes the context and hands it up.
   *
   * Both languages are cached together: the identity is the slug pair and has to survive a locale
   * switch, so a single-language label cache would have to be discarded at exactly that moment.
   */
  function resolve(program: ProgramOption | null) {
    if (institutionSlug === "") return;
    onResolve(
      academicContext(institutionSlug, program?.slug ?? "", {
        institutionAr: chosen?.name_ar ?? "",
        institutionEn: chosen?.name_en ?? "",
        programAr: program?.name_ar ?? "",
        programEn: program?.name_en ?? "",
      }),
    );
  }

  function chooseInstitution(slug: string) {
    setInstitutionSlug(slug);
    setAdvanced(true);
  }

  function reopenInstitution() {
    setInstitutionSlug("");
    setAdvanced(false);
  }

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

  if (institutions.items.length === 0) {
    return (
      <p
        role="status"
        className="text-sm text-muted-foreground"
        data-testid="academic-picker-empty"
      >
        {copy.noInstitutions}
      </p>
    );
  }

  const universityQuestionID = `${idPrefix}-university`;
  const programQuestionID = `${idPrefix}-program`;
  const answered = institutionSlug !== "" && chosen !== undefined;
  const noPrograms = programs.kind === "ready" && programs.items.length === 0;

  return (
    <div ref={rootRef} data-testid="academic-picker" className="space-y-6">
      {answered ? (
        <SelectedAnswer
          testID="academic-picker-institution"
          label={copy.universityLabel}
          value={institutionName(chosen, language)}
          // Nothing to reopen when the catalogue offers one university.
          onChange={institutions.items.length > 1 ? reopenInstitution : undefined}
          changeLabel={copy.change}
          changeAria={copy.changeAria}
        />
      ) : (
        <ContextQuestion id={universityQuestionID} question={copy.universityQuestion}>
          <div data-testid="academic-picker-institution">
            <ChoiceGrid>
              {institutions.items.map((option) => (
                <ChoiceChip
                  key={option.slug}
                  value={option.slug}
                  onSelect={() => chooseInstitution(option.slug)}
                >
                  {institutionName(option, language)}
                </ChoiceChip>
              ))}
            </ChoiceGrid>
          </div>
        </ContextQuestion>
      )}

      {answered ? (
        <ContextQuestion
          id={programQuestionID}
          question={copy.programQuestion}
          hint={noPrograms ? copy.noPrograms : undefined}
          appear={advanced}
        >
          {programs.kind === "loading" ? (
            <LoadingState label={copy.loadingPrograms} testID="academic-picker-programs-loading" />
          ) : programs.kind === "failed" ? (
            // Recoverable on its own: the university is still chosen and its courses are still
            // reachable, so this refuses only the one request that failed.
            <ErrorState
              testID="academic-picker-programs-error"
              title={copy.programsFailed}
              retryLabel={copy.retry}
              onRetry={() => setProgramAttempt((count) => count + 1)}
            />
          ) : (
            <div data-testid="academic-picker-program">
              <ChoiceGrid>
                {programs.kind === "ready" &&
                  programs.items.map((option) => {
                    const college = programContext(option, language);
                    return (
                      <ChoiceChip
                        key={option.slug}
                        value={option.slug}
                        detail={college === "" ? undefined : college}
                        selected={
                          initial?.institutionSlug === institutionSlug &&
                          initial?.programSlug === option.slug
                        }
                        onSelect={() => resolve(option)}
                      >
                        {programName(option, language)}
                      </ChoiceChip>
                    );
                  })}
                {/* The program is genuinely optional — a university on its own already narrows the
                    catalogue — so "not sure" resolves rather than dead-ends. */}
                <ChoiceChip
                  selected={initial?.institutionSlug === institutionSlug && initial?.programSlug === ""}
                  onSelect={() => resolve(null)}
                >
                  {copy.anyProgramChoice}
                </ChoiceChip>
              </ChoiceGrid>
            </div>
          )}
        </ContextQuestion>
      ) : null}

      {onSkip && skipLabel ? (
        <Button type="button" variant="ghost" size="sm" onClick={onSkip} className="-ms-2">
          {skipLabel}
        </Button>
      ) : null}
    </div>
  );
}
