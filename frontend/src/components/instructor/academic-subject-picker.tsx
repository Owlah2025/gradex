"use client";

import { useEffect, useRef, useState } from "react";
import {
  getAuthoringSubject,
  listAuthoringInstitutions,
  programName,
  searchAuthoringSubjects,
  subjectContext,
  subjectLabel,
  type AuthoringSubject,
  type InstitutionOption,
} from "@/lib/api/authoring-academic";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";

/**
 * University → Subject selection for Academic Course authoring (T4-B).
 *
 * The Instructor flow is deliberately University then Subject, NOT University →
 * College → Department → Program → Subject. An Instructor knows the Subject they
 * teach; the Academic Catalog derives everything else. Asking for the Program
 * first would also recreate the duplicate-Subject problem the redesign exists to
 * remove, because the same canonical Subject serves several Programs.
 *
 * No identifier is ever shown. A Subject is recognised by its official code and
 * title, and the College/Department line is descriptive context, never a step.
 */

export type AcademicSubjectSelection = {
  institutionID: string;
  subject: AuthoringSubject;
};

const SEARCH_DEBOUNCE_MS = 250;

export function AcademicSubjectPicker({
  onChange,
  onInstitutionChange,
  onRequestMissing,
  initialInstitutionID,
  initialSubjectID,
  disabled = false,
  idPrefix = "new-course",
}: {
  onChange: (selection: AcademicSubjectSelection | null) => void;
  onInstitutionChange?: (institutionID: string) => void;
  onRequestMissing?: () => void;
  initialInstitutionID?: string;
  initialSubjectID?: string;
  disabled?: boolean;
  idPrefix?: string;
}) {
  const { locale, t } = useLocale();
  const isAr = locale === "ar";
  const copy = t.instructor.picker;

  const [institutions, setInstitutions] = useState<InstitutionOption[]>([]);
  const [institutionID, setInstitutionID] = useState(initialInstitutionID ?? "");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<AuthoringSubject[]>([]);
  const [selected, setSelected] = useState<AuthoringSubject | null>(null);
  const [searching, setSearching] = useState(false);
  const [searched, setSearched] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // The picker owns no Course state. It reports a selection upward and the
  // parent decides what to do with it.
  const report = onChange;
  const reportRef = useRef(report);
  reportRef.current = report;
  const institutionReportRef = useRef(onInstitutionChange);
  institutionReportRef.current = onInstitutionChange;

  useEffect(() => {
    listAuthoringInstitutions(locale)
      .then((options) => {
        setInstitutions(options);
        // Only preselect when there is genuinely one choice. This is not a
        // Kuwait University special case: it is "one option needs no decision",
        // and a second Institution in the catalog restores the selector.
        setInstitutionID((current) => current || (options.length === 1 ? options[0].id : ""));
      })
      .catch(() =>
        setError(copy.universityFailed),
      );
  }, [copy.universityFailed, locale]);

  useEffect(() => {
    institutionReportRef.current?.(institutionID);
  }, [institutionID]);

  // Restore a Course's stored Subject without making the Instructor search for
  // something they already chose.
  useEffect(() => {
    if (!initialInstitutionID || !initialSubjectID) return;
    getAuthoringSubject({ institutionID: initialInstitutionID, subjectID: initialSubjectID, locale })
      .then((subject) => {
        if (!subject) return;
        setSelected(subject);
        reportRef.current({ institutionID: initialInstitutionID, subject });
      })
      .catch(() => {
        /* A Subject that cannot be read is reported by the parent surface. */
      });
  }, [initialInstitutionID, initialSubjectID, locale]);

  useEffect(() => {
    if (!institutionID || query.trim().length === 0) {
      setResults([]);
      setSearched(false);
      return;
    }
    setSearching(true);
    const handle = setTimeout(() => {
      searchAuthoringSubjects({ institutionID, query, locale })
        .then((found) => {
          setResults(found);
          setSearched(true);
          setError(null);
        })
        .catch(() => setError(copy.searchFailed))
        .finally(() => setSearching(false));
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [institutionID, query, locale, copy.searchFailed]);

  const choose = (subject: AuthoringSubject) => {
    setSelected(subject);
    setResults([]);
    setQuery("");
    setSearched(false);
    report({ institutionID, subject });
  };

  const clear = () => {
    setSelected(null);
    report(null);
  };

  return (
    <div className="space-y-4" data-testid={`${idPrefix}-academic-picker`}>
      <Field label={copy.universityLabel} htmlFor={`${idPrefix}-institution`}>
        <Select
          id={`${idPrefix}-institution`}
          value={institutionID}
          onChange={(event) => {
            setInstitutionID(event.target.value);
            // The Subject belongs to the Institution, so changing the University
            // invalidates any Subject already chosen.
            clear();
          }}
          disabled={disabled || institutions.length === 0}
          data-testid={`${idPrefix}-institution`}
        >
          <option value="">{copy.universityPlaceholder}</option>
          {institutions.map((institution) => (
            <option key={institution.id} value={institution.id}>
              {isAr ? institution.name_ar : institution.name_en}
            </option>
          ))}
        </Select>
      </Field>

      {selected ? (
        <div
          data-testid={`${idPrefix}-selected-subject`}
          className="space-y-3 rounded-lg border border-border bg-card p-4"
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <p
                className="font-display text-sm font-bold text-foreground"
                data-testid={`${idPrefix}-selected-subject-label`}
              >
                {/* A Latin subject code sits inside an Arabic title here more often than not. */}
                <bdi>{subjectLabel(selected, locale)}</bdi>
              </p>
              {subjectContext(selected, locale) && (
                <p
                  className="mt-0.5 text-xs text-muted-foreground"
                  data-testid={`${idPrefix}-selected-subject-context`}
                >
                  <bdi>{subjectContext(selected, locale)}</bdi>
                </p>
              )}
            </div>
            {!disabled && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={clear}
                data-testid={`${idPrefix}-change-subject`}
                className="shrink-0"
              >
                {copy.change}
              </Button>
            )}
          </div>
          <AutomaticAudience subject={selected} idPrefix={idPrefix} />
        </div>
      ) : (
        <Field label={copy.subjectLabel} htmlFor={`${idPrefix}-subject-search`}>
          <Input
            id={`${idPrefix}-subject-search`}
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            disabled={disabled || !institutionID}
            placeholder={copy.subjectSearchPlaceholder}
            data-testid={`${idPrefix}-subject-search`}
          />
        </Field>
      )}

      {searching && (
        <p
          role="status"
          className="text-sm text-muted-foreground"
          data-testid={`${idPrefix}-subject-searching`}
        >
          {copy.searching}
        </p>
      )}

      {!selected && results.length > 0 && (
        <ul
          className="divide-y divide-border overflow-hidden rounded-lg border border-border"
          data-testid={`${idPrefix}-subject-results`}
        >
          {results.map((subject) => (
            <li key={subject.id}>
              <button
                type="button"
                onClick={() => choose(subject)}
                data-testid={`${idPrefix}-subject-result`}
                className="w-full px-3 py-2.5 text-start hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              >
                <span className="block text-sm font-semibold text-foreground">
                  <bdi>{subjectLabel(subject, locale)}</bdi>
                </span>
                {subjectContext(subject, locale) && (
                  <span className="mt-0.5 block text-xs text-muted-foreground">
                    <bdi>{subjectContext(subject, locale)}</bdi>
                  </span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}

      {!selected && searched && !searching && results.length === 0 && (
        <div className="space-y-2" data-testid={`${idPrefix}-subject-empty`}>
          <p className="text-sm text-muted-foreground">{copy.noMatch}</p>
          {onRequestMissing && institutionID && (
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onRequestMissing}
              data-testid={`${idPrefix}-request-subject`}
            >
              {copy.requestMissing}
            </Button>
          )}
        </div>
      )}

      {error && (
        <p
          role="alert"
          className="text-sm font-medium text-destructive"
          data-testid={`${idPrefix}-academic-error`}
        >
          {error}
        </p>
      )}
    </div>
  );
}

/**
 * The inferred audience, read-only in T4-B.
 *
 * This is derived from the Academic Catalog every time it is displayed and is
 * never persisted: zero `course_program_targets` rows remains the automatic
 * state, and writing rows to render a preview would silently convert every
 * Course into having an explicit override. Choosing a narrower audience is T4-C.
 */
function AutomaticAudience({ subject, idPrefix }: { subject: AuthoringSubject; idPrefix: string }) {
  const { locale, t } = useLocale();
  const copy = t.instructor.picker;

  if (subject.programs.length === 0) {
    // A Subject with no Curriculum mapping is a legitimate Course Subject. The
    // honest statement is that the catalog maps no Program to it — not that the
    // Course has no audience, and not an invented association.
    return (
      <p className="text-xs text-muted-foreground" data-testid={`${idPrefix}-audience-empty`}>
        {copy.audienceEmpty}
      </p>
    );
  }

  return (
    <div data-testid={`${idPrefix}-audience`}>
      <p className="font-display text-[11px] font-bold uppercase tracking-wide text-muted-foreground">
        {copy.audienceTitle}
      </p>
      <ul className="mt-1 space-y-0.5">
        {subject.programs.map((program) => (
          <li
            key={program.program_id}
            className="text-xs text-muted-foreground"
            data-testid={`${idPrefix}-audience-program`}
          >
            <bdi>{programName(program, locale)}</bdi>
            {/*
              Placement appears only where the Curriculum mapping carries it.
              Kuwait University publishes a study plan for Computer Science and
              Data Science & AI but not for the other launch Programs, so this is
              frequently absent and is never derived from the Subject code.
            */}
            {program.recommended_level !== undefined && (
              <span> {`\u2014 ${copy.level} ${program.recommended_level}`}</span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
