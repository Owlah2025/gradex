"use client";

import { useEffect, useState } from "react";
import {
  getPublicInstitutions,
  getPublicLevels,
  getPublicPrograms,
  getPublicSubjects,
  type InstitutionOption,
  type ProgramOption,
  type SubjectOption,
} from "@/lib/api/public-catalog";
import { Button } from "@/components/ui/button";
import {
  applySelection,
  clearedSelection,
  hasSelection,
  institutionName,
  programContext,
  programName,
  subjectName,
  type CatalogueSelection,
} from "./academic-filter-state";

/**
 * Academic discovery filters (T6).
 *
 * Three dependent choosers — University, then Program, then Subject — that
 * answer the questions a Student actually has: where do you study, what is your
 * program, which subject are you looking for. Every option is rendered by its
 * localized name; no slug, code-only value, or identifier is ever displayed as
 * the label, and the visitor never has to know one to reach a Course.
 *
 * They are real <select> elements with real <label>s rather than custom
 * widgets, so keyboard operation, screen-reader naming, and RTL come from the
 * platform instead of being reimplemented.
 */

const copy = {
  ar: {
    heading: "تصفية حسب الخطة الدراسية",
    institution: "الجامعة",
    program: "التخصص",
    level: "المستوى الدراسي",
    subject: "المقرر",
    anyInstitution: "كل الجامعات",
    anyProgram: "كل التخصصات",
    anyLevel: "كل المستويات",
    levelValue: "المستوى",
    anySubject: "كل المقررات",
    chooseInstitutionFirst: "اختر الجامعة أولاً",
    chooseProgramFirst: "اختر التخصص أولاً",
    clear: "مسح التصفية",
    unavailable: "تعذر تحميل بيانات الجامعات. يمكنك متابعة التصفح والبحث.",
    loading: "جارٍ تحميل الخيارات…",
  },
  en: {
    heading: "Filter by study plan",
    institution: "University",
    program: "Program",
    level: "Academic level",
    subject: "Subject",
    anyInstitution: "All universities",
    anyProgram: "All programs",
    anyLevel: "All levels",
    levelValue: "Level",
    anySubject: "All subjects",
    chooseInstitutionFirst: "Choose a university first",
    chooseProgramFirst: "Choose a program first",
    clear: "Clear filters",
    unavailable:
      "Academic data could not be loaded. You can still browse and search.",
    loading: "Loading options…",
  },
};

function Field({
  id,
  label,
  value,
  disabled,
  hint,
  onChange,
  children,
}: {
  id: string;
  label: string;
  value: string;
  disabled: boolean;
  hint?: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
}) {
  const hintId = hint ? `${id}-hint` : undefined;
  return (
    <div className="flex min-w-[12rem] flex-1 flex-col gap-1">
      <label className="text-sm font-semibold text-slate-700" htmlFor={id}>
        {label}
      </label>
      <select
        id={id}
        value={value}
        disabled={disabled}
        aria-describedby={hintId}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-md border border-slate-300 bg-white px-3 py-2 text-sm disabled:cursor-not-allowed disabled:bg-slate-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      >
        {children}
      </select>
      {hint && (
        <p id={hintId} className="text-xs text-slate-500">
          {hint}
        </p>
      )}
    </div>
  );
}

export function AcademicFilters({
  locale,
  selection,
  onChange,
  onInstitutionsLoaded,
  onProgramsLoaded,
}: {
  locale: "ar" | "en";
  selection: CatalogueSelection;
  onChange: (next: CatalogueSelection) => void;
  /**
   * Reports the option lists this row has just read, so a caller holding a remembered selection can
   * check it against what the catalogue actually offers.
   *
   * `null` means "could not be read", which is deliberately not the same as an empty array: a
   * failed request is no evidence that a university has been retired, and treating it as such would
   * discard a valid remembered context every time the network hiccuped.
   */
  onInstitutionsLoaded?: (items: InstitutionOption[] | null) => void;
  onProgramsLoaded?: (items: ProgramOption[] | null) => void;
}) {
  const t = copy[locale];
  const [institutions, setInstitutions] = useState<InstitutionOption[] | null>(
    null,
  );
  const [programs, setPrograms] = useState<ProgramOption[]>([]);
  const [subjects, setSubjects] = useState<SubjectOption[]>([]);
  const [levels, setLevels] = useState<number[]>([]);
  const [unavailable, setUnavailable] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getPublicInstitutions(locale)
      .then((items) => {
        if (cancelled) return;
        setInstitutions(items);
        onInstitutionsLoaded?.(items);
      })
      .catch(() => {
        // Academic data being unavailable must not take the catalogue with it:
        // browsing and search stay usable and the filters simply say so.
        if (cancelled) return;
        setInstitutions([]);
        setUnavailable(true);
        onInstitutionsLoaded?.(null);
      });
    return () => {
      cancelled = true;
    };
  }, [locale, onInstitutionsLoaded]);

  useEffect(() => {
    let cancelled = false;
    if (selection.institution === "") {
      setPrograms([]);
      return;
    }
    getPublicPrograms(selection.institution, locale)
      .then((items) => {
        if (cancelled) return;
        setPrograms(items);
        onProgramsLoaded?.(items);
      })
      .catch(() => {
        if (cancelled) return;
        setPrograms([]);
        onProgramsLoaded?.(null);
      });
    return () => {
      cancelled = true;
    };
  }, [locale, selection.institution, onProgramsLoaded]);

  // Only the levels a study plan actually records are offered. A level nothing
  // is recorded at would be a study plan the university does not have.
  useEffect(() => {
    let cancelled = false;
    if (selection.institution === "") {
      setLevels([]);
      return;
    }
    getPublicLevels(selection.institution, selection.program, locale)
      .then((items) => {
        if (!cancelled) setLevels(items);
      })
      .catch(() => {
        if (!cancelled) setLevels([]);
      });
    return () => {
      cancelled = true;
    };
  }, [locale, selection.institution, selection.program]);

  useEffect(() => {
    let cancelled = false;
    if (selection.institution === "") {
      setSubjects([]);
      return;
    }
    getPublicSubjects(selection.institution, selection.program, locale)
      .then((items) => {
        if (!cancelled) setSubjects(items);
      })
      .catch(() => {
        if (!cancelled) setSubjects([]);
      });
    return () => {
      cancelled = true;
    };
  }, [locale, selection.institution, selection.program]);

  function change(field: keyof CatalogueSelection, value: string) {
    onChange(applySelection(selection, { [field]: value }));
  }

  return (
    <section
      aria-label={t.heading}
      className="mt-8 rounded-lg border border-slate-200 bg-white p-5"
      data-testid="academic-filters"
    >
      <h2 className="font-display text-base font-bold">{t.heading}</h2>
      {unavailable && (
        <p role="status" className="mt-2 text-sm text-slate-600">
          {t.unavailable}
        </p>
      )}
      <div className="mt-4 flex flex-wrap gap-4">
        <Field
          id="catalogue-institution"
          label={t.institution}
          value={selection.institution}
          disabled={institutions === null}
          hint={institutions === null ? t.loading : undefined}
          onChange={(value) => change("institution", value)}
        >
          <option value="">{t.anyInstitution}</option>
          {(institutions ?? []).map((option) => (
            <option key={option.slug} value={option.slug}>
              {institutionName(option, locale)}
            </option>
          ))}
        </Field>

        <Field
          id="catalogue-program"
          label={t.program}
          value={selection.program}
          disabled={selection.institution === ""}
          hint={
            selection.institution === "" ? t.chooseInstitutionFirst : undefined
          }
          onChange={(value) => change("program", value)}
        >
          <option value="">{t.anyProgram}</option>
          {programs.map((option) => {
            const context = programContext(option, locale);
            return (
              <option key={option.slug} value={option.slug}>
                {programName(option, locale)}
                {context === "" ? "" : ` · ${context}`}
              </option>
            );
          })}
        </Field>

        <Field
          id="catalogue-level"
          label={t.level}
          value={selection.level}
          disabled={selection.institution === "" || levels.length === 0}
          hint={
            selection.institution === "" ? t.chooseInstitutionFirst : undefined
          }
          onChange={(value) => change("level", value)}
        >
          <option value="">{t.anyLevel}</option>
          {levels.map((level) => (
            <option key={level} value={String(level)}>
              {`${t.levelValue} ${level}`}
            </option>
          ))}
        </Field>

        <Field
          id="catalogue-subject"
          label={t.subject}
          value={selection.subject}
          disabled={selection.institution === ""}
          hint={
            selection.institution === "" ? t.chooseInstitutionFirst : undefined
          }
          onChange={(value) => change("subject", value)}
        >
          <option value="">{t.anySubject}</option>
          {subjects.map((option) => (
            <option key={option.value} value={option.value}>
              {subjectName(option, locale)}
            </option>
          ))}
        </Field>
      </div>

      {hasSelection(selection) && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-4"
          onClick={() => onChange(clearedSelection())}
        >
          {t.clear}
        </Button>
      )}
    </section>
  );
}
