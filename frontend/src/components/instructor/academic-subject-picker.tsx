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
  const { locale } = useLocale();
  const isAr = locale === "ar";

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
        setError(isAr ? "تعذر تحميل الجامعات" : "Unable to load universities"),
      );
  }, [isAr, locale]);

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
        .catch(() => setError(isAr ? "تعذر البحث عن المادة" : "Unable to search Subjects"))
        .finally(() => setSearching(false));
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [institutionID, query, locale, isAr]);

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
      <label className="block text-sm font-medium">
        {isAr ? "الجامعة" : "University"}
        <select
          value={institutionID}
          onChange={(event) => {
            setInstitutionID(event.target.value);
            // The Subject belongs to the Institution, so changing the University
            // invalidates any Subject already chosen.
            clear();
          }}
          disabled={disabled || institutions.length === 0}
          data-testid={`${idPrefix}-institution`}
          className="mt-1 w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
        >
          <option value="">{isAr ? "اختر الجامعة" : "Select a university"}</option>
          {institutions.map((institution) => (
            <option key={institution.id} value={institution.id}>
              {isAr ? institution.name_ar : institution.name_en}
            </option>
          ))}
        </select>
      </label>

      {selected ? (
        <div
          data-testid={`${idPrefix}-selected-subject`}
          className="rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-3 space-y-2"
        >
          <div className="flex items-start justify-between gap-3">
            <div>
              <p className="text-sm font-semibold" data-testid={`${idPrefix}-selected-subject-label`}>
                {subjectLabel(selected, locale)}
              </p>
              {subjectContext(selected, locale) && (
                <p className="text-xs text-slate-600 dark:text-slate-400" data-testid={`${idPrefix}-selected-subject-context`}>
                  {subjectContext(selected, locale)}
                </p>
              )}
            </div>
            {!disabled && (
              <button
                type="button"
                onClick={clear}
                data-testid={`${idPrefix}-change-subject`}
                className="shrink-0 rounded-md border border-slate-300 dark:border-slate-700 px-2 py-1 text-xs"
              >
                {isAr ? "تغيير المادة" : "Change Subject"}
              </button>
            )}
          </div>
          <AutomaticAudience subject={selected} idPrefix={idPrefix} />
        </div>
      ) : (
        <label className="block text-sm font-medium">
          {isAr ? "المادة" : "Subject"}
          <input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            disabled={disabled || !institutionID}
            placeholder={isAr ? "ابحث باسم المادة أو الكود" : "Search by Subject name or code"}
            data-testid={`${idPrefix}-subject-search`}
            className="mt-1 w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
          />
        </label>
      )}

      {searching && (
        <p className="text-xs text-slate-600 dark:text-slate-400" data-testid={`${idPrefix}-subject-searching`}>
          {isAr ? "جارٍ البحث…" : "Searching…"}
        </p>
      )}

      {!selected && results.length > 0 && (
        <ul className="divide-y divide-slate-200 dark:divide-slate-800 rounded-md border border-slate-300 dark:border-slate-700"
            data-testid={`${idPrefix}-subject-results`}>
          {results.map((subject) => (
            <li key={subject.id}>
              <button
                type="button"
                onClick={() => choose(subject)}
                data-testid={`${idPrefix}-subject-result`}
                className="w-full px-3 py-2 text-start hover:bg-slate-50 dark:hover:bg-slate-800"
              >
                <span className="block text-sm font-medium">{subjectLabel(subject, locale)}</span>
                {subjectContext(subject, locale) && (
                  <span className="block text-xs text-slate-600 dark:text-slate-400">
                    {subjectContext(subject, locale)}
                  </span>
                )}
              </button>
            </li>
          ))}
        </ul>
      )}

      {!selected && searched && !searching && results.length === 0 && (
        <div className="space-y-2" data-testid={`${idPrefix}-subject-empty`}>
          <p className="text-xs text-slate-600 dark:text-slate-400">
            {isAr
              ? "لا توجد مادة مطابقة. جرّب كود المادة أو اسمًا آخر."
              : "No matching Subject. Try the Subject code or another name."}
          </p>
          {onRequestMissing && institutionID && (
            <button
              type="button"
              onClick={onRequestMissing}
              data-testid={`${idPrefix}-request-subject`}
              className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs"
            >
              {isAr ? "لم أجد مادتي" : "I can't find my Subject"}
            </button>
          )}
        </div>
      )}

      {error && (
        <p role="alert" className="text-xs text-red-700 dark:text-red-400" data-testid={`${idPrefix}-academic-error`}>
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
  const { locale } = useLocale();
  const isAr = locale === "ar";

  if (subject.programs.length === 0) {
    // A Subject with no Curriculum mapping is a legitimate Course Subject. The
    // honest statement is that the catalog maps no Program to it — not that the
    // Course has no audience, and not an invented association.
    return (
      <p className="text-xs text-slate-600 dark:text-slate-400" data-testid={`${idPrefix}-audience-empty`}>
        {isAr
          ? "لا توجد تخصصات مرتبطة بهذه المادة في الكتالوج الأكاديمي حاليًا."
          : "No Programs are currently associated with this Subject in the Academic Catalog."}
      </p>
    );
  }

  return (
    <div data-testid={`${idPrefix}-audience`}>
      <p className="text-xs font-medium text-slate-700 dark:text-slate-300">
        {isAr ? "الجمهور التلقائي · التخصصات المرتبطة" : "Automatic audience · Associated Programs"}
      </p>
      <ul className="mt-1 space-y-0.5">
        {subject.programs.map((program) => (
          <li key={program.program_id} className="text-xs text-slate-600 dark:text-slate-400"
              data-testid={`${idPrefix}-audience-program`}>
            {programName(program, locale)}
            {/*
              Placement appears only where the Curriculum mapping carries it.
              Kuwait University publishes a study plan for Computer Science and
              Data Science & AI but not for the other launch Programs, so this is
              frequently absent and is never derived from the Subject code.
            */}
            {program.recommended_level !== undefined && (
              <span className="text-slate-500">
                {" — "}
                {isAr ? `المستوى ${program.recommended_level}` : `Level ${program.recommended_level}`}
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
