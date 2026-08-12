"use client";

import { useState } from "react";
import { TaxonomyTermSelect } from "@/components/catalog/taxonomy-term-select";
import { assignAdminTaxonomy, type TaxonomyTerm } from "@/lib/api/catalog";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

type TaxonomyOverrideFormProps = {
  courseID: string;
  revisionID: string;
  terms: TaxonomyTerm[];
};

export function TaxonomyOverrideForm({ courseID, revisionID, terms }: TaxonomyOverrideFormProps) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [majorTermID, setMajorTermID] = useState("");
  const [subjectTermID, setSubjectTermID] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const save = async () => {
    if (!majorTermID || !subjectTermID) {
      setMessage(isAr ? "التخصص والمادة إجباريان" : "Major and subject are required");
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
      await assignAdminTaxonomy({ courseID, revisionID, majorTermID, subjectTermID, locale, csrf });
      setMessage(isAr ? "تم تطبيق التصنيف على المراجعة المحددة" : "Taxonomy applied to the named revision");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : isAr ? "تعذر تطبيق التصنيف" : "Unable to apply taxonomy");
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded-lg border border-blue-200 bg-blue-50/60 p-4 dark:border-blue-900 dark:bg-blue-950/20">
      <h3 className="text-sm font-bold text-slate-900 dark:text-slate-100">{isAr ? "تجاوز تصنيف الدورة" : "Course Taxonomy Override"}</h3>
      <p className="mt-1 text-xs text-slate-600 dark:text-slate-400">{isAr ? "اختياري؛ يُطبق على المراجعة المُرسلة المفتوحة فقط." : "Optional; applies only to the open submitted revision."}</p>
      <div className="mt-3 grid gap-3 md:grid-cols-2">
        <TaxonomyTermSelect kind="MAJOR" locale={locale} terms={terms} value={majorTermID} onChange={setMajorTermID} disabled={busy} />
        <TaxonomyTermSelect kind="SUBJECT" locale={locale} terms={terms} value={subjectTermID} onChange={setSubjectTermID} disabled={busy} />
      </div>
      <button type="button" disabled={busy} onClick={save} data-testid="taxonomy-override-apply" className="mt-3 rounded bg-blue-700 px-3 py-2 text-xs font-semibold text-white hover:bg-blue-800 disabled:opacity-50">
        {busy ? (isAr ? "جارٍ التطبيق..." : "Applying...") : (isAr ? "تطبيق التجاوز" : "Apply Override")}
      </button>
      {message && <p role="status" className="mt-3 text-xs text-slate-700 dark:text-slate-300">{message}</p>}
    </section>
  );
}
