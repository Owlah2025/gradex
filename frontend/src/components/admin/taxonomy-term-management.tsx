"use client";

import { useState } from "react";
import {
	createTaxonomyTerm,
	deleteTaxonomyTerm,
	renameTaxonomyTerm,
	retireTaxonomyTerm,
} from "@/lib/api/catalog";
import { taxonomyTermLabel } from "@/components/catalog/taxonomy-term-select";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { TaxonomyKind, TaxonomyTerm } from "@/lib/api/catalog";

type TaxonomyTermManagementProps = {
  terms: TaxonomyTerm[];
  refresh: () => Promise<void>;
};

export function TaxonomyTermManagement({ terms, refresh }: TaxonomyTermManagementProps) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [kind, setKind] = useState<TaxonomyKind>("MAJOR");
  const [labelAr, setLabelAr] = useState("");
  const [labelEn, setLabelEn] = useState("");
  const [academicCode, setAcademicCode] = useState("");
  const [selectedID, setSelectedID] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const csrf = () => {
    const token = currentCSRFToken();
    if (!token) setMessage(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
    return token;
  };

	const run = async (action: (token: string) => Promise<unknown>, success: string) => {
    const token = csrf();
    if (!token) return;
    setBusy(true);
    setMessage(null);
    try {
      await action(token);
      await refresh();
      setMessage(success);
    } catch (error) {
      setMessage(error instanceof Error ? error.message : isAr ? "تعذر تحديث المصطلح" : "Unable to update taxonomy term");
    } finally {
      setBusy(false);
    }
  };

  const create = () => {
    if (!labelAr.trim() || !labelEn.trim()) {
      setMessage(isAr ? "الاسمان العربي والإنجليزي إجباريان" : "Arabic and English labels are required");
      return;
    }
    void run(async (token) => {
      await createTaxonomyTerm({ kind, labelAr: labelAr.trim(), labelEn: labelEn.trim(), academicCode: academicCode.trim() || undefined, locale, csrf: token });
      setLabelAr(""); setLabelEn(""); setAcademicCode("");
    }, isAr ? "تم إنشاء المصطلح وتوثيقه" : "Term created and audited");
  };

  const rename = () => {
    if (!selectedID || !labelAr.trim() || !labelEn.trim()) {
      setMessage(isAr ? "اختر مصطلحاً وأدخل الاسمين" : "Select a term and enter both labels");
      return;
    }
    void run((token) => renameTaxonomyTerm({ termID: selectedID, labelAr: labelAr.trim(), labelEn: labelEn.trim(), locale, csrf: token }), isAr ? "تمت إعادة تسمية المصطلح" : "Term renamed");
  };

  const retire = () => {
    if (!selectedID) { setMessage(isAr ? "اختر مصطلحاً" : "Select a term"); return; }
    void run((token) => retireTaxonomyTerm({ termID: selectedID, locale, csrf: token }), isAr ? "تمت إحالة المصطلح للتقاعد" : "Term retired");
  };

  const remove = () => {
    if (!selectedID) { setMessage(isAr ? "اختر مصطلحاً" : "Select a term"); return; }
    void run((token) => deleteTaxonomyTerm({ termID: selectedID, locale, csrf: token }), isAr ? "تم حذف المصطلح" : "Term deleted");
  };

  return (
    <section className="rounded-lg border border-violet-200 bg-violet-50/60 p-4 dark:border-violet-900 dark:bg-violet-950/20">
      <h3 className="text-sm font-bold text-slate-900 dark:text-slate-100">{isAr ? "قاموس التصنيف" : "Taxonomy Vocabulary"}</h3>
      <div className="mt-3 grid gap-3 md:grid-cols-2">
        <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">{isAr ? "النوع" : "Kind"}
          <select value={kind} onChange={(event) => setKind(event.target.value as TaxonomyKind)} data-testid="taxonomy-term-kind" className="mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900"><option value="MAJOR">{isAr ? "تخصص" : "Major"}</option><option value="SUBJECT">{isAr ? "مادة" : "Subject"}</option></select>
        </label>
        <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">{isAr ? "مصطلح قائم" : "Existing term"}
          <select value={selectedID} onChange={(event) => setSelectedID(event.target.value)} className="mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900"><option value="">{isAr ? "اختر مصطلحاً" : "Select a term"}</option>{terms.map((term) => <option key={term.id} value={term.id}>{taxonomyTermLabel(term, locale)} — {term.kind}</option>)}</select>
        </label>
        <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">{isAr ? "الاسم العربي" : "Arabic label"}<input value={labelAr} onChange={(event) => setLabelAr(event.target.value)} data-testid="taxonomy-term-label-ar" className="mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900" /></label>
        <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">{isAr ? "الاسم الإنجليزي" : "English label"}<input value={labelEn} onChange={(event) => setLabelEn(event.target.value)} data-testid="taxonomy-term-label-en" className="mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900" /></label>
        {kind === "SUBJECT" && <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">{isAr ? "الرمز الأكاديمي (اختياري)" : "Academic code (optional)"}<input value={academicCode} onChange={(event) => setAcademicCode(event.target.value)} data-testid="taxonomy-term-academic-code" className="mt-1 w-full rounded border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-900" /></label>}
      </div>
      <div className="mt-4 flex flex-wrap gap-2"><button type="button" disabled={busy} onClick={create} data-testid="taxonomy-term-create" className="rounded bg-violet-700 px-3 py-2 text-xs font-semibold text-white hover:bg-violet-800 disabled:opacity-50">{isAr ? "إنشاء" : "Create"}</button><button type="button" disabled={busy} onClick={rename} className="rounded bg-slate-700 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-50">{isAr ? "إعادة تسمية" : "Rename"}</button><button type="button" disabled={busy} onClick={retire} className="rounded bg-amber-700 px-3 py-2 text-xs font-semibold text-white hover:bg-amber-800 disabled:opacity-50">{isAr ? "تقاعد" : "Retire"}</button><button type="button" disabled={busy} onClick={remove} className="rounded bg-rose-700 px-3 py-2 text-xs font-semibold text-white hover:bg-rose-800 disabled:opacity-50">{isAr ? "حذف" : "Delete"}</button></div>
      {message && <p role="status" data-testid="taxonomy-term-message" className="mt-3 text-xs text-slate-700 dark:text-slate-300">{message}</p>}
    </section>
  );
}
