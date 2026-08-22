"use client";

import { useEffect, useState } from "react";
import { programName, type AuthoringSubject } from "@/lib/api/authoring-academic";
import type { RevisionAudienceWire } from "@/lib/api/catalog";
import { useLocale } from "@/lib/i18n/locale-provider";

type RevisionAudienceEditorProps = {
  subject: AuthoringSubject;
  audience?: RevisionAudienceWire;
  editable: boolean;
  busy: boolean;
  onCustomize: (programIDs: string[]) => void;
  onReset: () => void;
};

export function RevisionAudienceEditor({
  subject,
  audience,
  editable,
  busy,
  onCustomize,
  onReset,
}: RevisionAudienceEditorProps) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const customized = audience?.mode === "CUSTOMIZED";
  const effectivePrograms = customized ? audience.programs : subject.programs;
  const [editing, setEditing] = useState(false);
  const [selectedProgramIDs, setSelectedProgramIDs] = useState<string[]>([]);

  useEffect(() => {
    setSelectedProgramIDs((audience?.programs ?? []).map((program) => program.program_id));
  }, [audience]);

  const begin = () => {
    if (!customized) setSelectedProgramIDs(subject.programs.map((program) => program.program_id));
    setEditing(true);
  };

  const toggle = (programID: string, checked: boolean) => {
    setSelectedProgramIDs((current) => checked
      ? [...current, programID]
      : current.filter((id) => id !== programID));
  };

  return (
    <div className="space-y-2">
      {effectivePrograms.length > 0 ? (
        <div className="text-xs text-slate-600 dark:text-slate-400" data-testid="academic-course-audience">
          <p className="font-medium text-slate-700 dark:text-slate-300" data-testid="academic-course-audience-mode">
            {customized
              ? (isAr ? "جمهور مخصص" : "Customized audience")
              : (isAr ? "الجمهور التلقائي" : "Automatic audience")}
          </p>
          <ul className="mt-1 space-y-0.5">
            {effectivePrograms.map((program) => <li key={program.program_id}>{programName(program, locale)}</li>)}
          </ul>
        </div>
      ) : (
        <p className="text-xs text-slate-600 dark:text-slate-400" data-testid="academic-course-audience-empty">
          {isAr
            ? "لا توجد تخصصات مرتبطة بهذه المادة في الكتالوج الأكاديمي حاليًا."
            : "No Programs are currently associated with this Subject in the Academic Catalog."}
        </p>
      )}

      {editable && !editing && !customized && subject.programs.length > 0 && (
        <button type="button" onClick={begin} disabled={busy} data-testid="academic-course-customize-audience"
          className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs disabled:opacity-50">
          {isAr ? "تخصيص الجمهور" : "Customize Audience"}
        </button>
      )}

      {editable && customized && !editing && (
        <div className="flex flex-wrap gap-2">
          <button type="button" onClick={begin} disabled={busy} data-testid="academic-course-edit-audience"
            className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs disabled:opacity-50">
            {isAr ? "تعديل الجمهور" : "Edit audience"}
          </button>
          <button type="button" onClick={onReset} disabled={busy} data-testid="academic-course-use-automatic-audience"
            className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs disabled:opacity-50">
            {isAr ? "استخدام الجمهور التلقائي" : "Use automatic audience"}
          </button>
        </div>
      )}

      {editable && editing && (
        <div className="space-y-2" data-testid="academic-course-audience-editor">
          {subject.programs.map((program) => (
            <label key={program.program_id} className="flex items-center gap-2 text-xs">
              <input type="checkbox" checked={selectedProgramIDs.includes(program.program_id)}
                onChange={(event) => toggle(program.program_id, event.target.checked)} disabled={busy}
                data-testid="academic-course-audience-option" />
              {programName(program, locale)}
            </label>
          ))}
          <div className="flex gap-2">
            <button type="button" disabled={busy || selectedProgramIDs.length === 0}
              onClick={() => { onCustomize(selectedProgramIDs); setEditing(false); }}
              data-testid="academic-course-save-audience"
              className="rounded bg-blue-600 px-3 py-1 text-xs text-white disabled:opacity-50">
              {isAr ? "حفظ الجمهور" : "Save audience"}
            </button>
            <button type="button" onClick={() => setEditing(false)} data-testid="academic-course-cancel-audience"
              className="rounded border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs">
              {isAr ? "إلغاء" : "Cancel"}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
