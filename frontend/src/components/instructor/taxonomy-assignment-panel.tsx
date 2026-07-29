"use client";

import { useEffect, useState } from "react";
import { TaxonomyTermSelect } from "@/components/catalog/taxonomy-term-select";
import {
  assignInstructorTaxonomy,
  getOwnedCourses,
  getTaxonomyTerms,
  type OwnedCourseSummary,
  type TaxonomyTerm,
} from "@/lib/api/catalog";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

export function TaxonomyAssignmentPanel() {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [courses, setCourses] = useState<OwnedCourseSummary[]>([]);
  const [terms, setTerms] = useState<TaxonomyTerm[]>([]);
  const [courseID, setCourseID] = useState("");
  const [majorTermID, setMajorTermID] = useState("");
  const [subjectTermID, setSubjectTermID] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    Promise.all([getOwnedCourses(locale), getTaxonomyTerms(locale)])
      .then(([ownedCourses, activeTerms]) => {
        setCourses(ownedCourses);
        setTerms(activeTerms);
      })
      .catch((error: unknown) => setMessage(error instanceof Error ? error.message : isAr ? "تعذر تحميل التصنيف" : "Unable to load taxonomy"));
  }, [isAr, locale]);

  const selectedCourse = courses.find((course) => course.id === courseID);
  const revision = selectedCourse?.editable_revision;

  const selectCourse = (selectedID: string) => {
    setCourseID(selectedID);
    const selected = courses.find((course) => course.id === selectedID)?.editable_revision;
    setMajorTermID(selected?.major_term_id ?? "");
    setSubjectTermID(selected?.subject_term_id ?? "");
    setMessage(null);
  };

  const save = async () => {
    if (!revision?.id || !majorTermID || !subjectTermID) {
      setMessage(isAr ? "اختر دورة مسودة وتخصصاً ومادةً" : "Select an editable draft, major, and subject");
      return;
    }
    const csrf = currentCSRFToken();
    if (!csrf) {
      setMessage(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return;
    }
    setBusy(true);
    setMessage(null);
    try {
      await assignInstructorTaxonomy({ courseID, revisionID: revision.id, majorTermID, subjectTermID, locale, csrf });
      setMessage(isAr ? "تم حفظ تصنيف المراجعة المحددة" : "Taxonomy saved for the named revision");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : isAr ? "تعذر حفظ التصنيف" : "Unable to save taxonomy");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded-xl border border-indigo-200 bg-indigo-50/50 p-5 shadow-sm dark:border-indigo-900 dark:bg-indigo-950/20">
      <h2 className="text-sm font-bold text-slate-900 dark:text-slate-100">{isAr ? "تصنيف المسودة المحددة" : "Explicit Draft Taxonomy"}</h2>
      <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">
        {isAr ? "يحفظ الاختيار على مراجعة المسودة المعروضة فقط، وليس على أحدث مراجعة مفترضة." : "Saves to the displayed editable revision only; no latest-revision lookup is used."}
      </p>
      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
          {isAr ? "الدورة" : "Course"}
          <select value={courseID} onChange={(event) => selectCourse(event.target.value)} className="mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900">
            <option value="">{isAr ? "اختر دورة" : "Select a Course"}</option>
            {courses.filter((course) => course.editable_revision?.id).map((course) => (
              <option key={course.id} value={course.id}>{isAr ? course.editable_revision?.title_ar : course.editable_revision?.title_en}</option>
            ))}
          </select>
        </label>
        <TaxonomyTermSelect kind="MAJOR" locale={locale} terms={terms} value={majorTermID} onChange={setMajorTermID} disabled={!revision?.id || busy} />
        <TaxonomyTermSelect kind="SUBJECT" locale={locale} terms={terms} value={subjectTermID} onChange={setSubjectTermID} disabled={!revision?.id || busy} />
      </div>
      {revision?.id && <p className="mt-2 text-[11px] font-mono text-indigo-700 dark:text-indigo-300">revision_id: {revision.id}</p>}
      <button type="button" disabled={busy || !revision?.id} onClick={save} className="mt-4 rounded bg-indigo-700 px-3 py-2 text-xs font-semibold text-white hover:bg-indigo-800 disabled:opacity-50">
        {busy ? (isAr ? "جارٍ الحفظ..." : "Saving...") : (isAr ? "حفظ التصنيف" : "Save Taxonomy")}
      </button>
      {message && <p role="status" className="mt-3 text-xs text-slate-700 dark:text-slate-300">{message}</p>}
    </section>
  );
}
