"use client";

import { useState } from "react";
import {
  archiveCourse,
  delistCourse,
  reassignCourseOwner,
  relistCourse,
  restoreCourseAccess,
  retireCourse,
  suspendCourseAccess,
  type SuspensionCause,
} from "@/lib/api/catalog";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

export function LifecycleControls({ courseID }: { courseID: string }) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [ownerAccountID, setOwnerAccountID] = useState("");
  const [cause, setCause] = useState<SuspensionCause>("SECURITY");
  const [reason, setReason] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const invoke = async (label: string, action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf) { setMessage(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing"); return; }
    setBusy(true); setMessage(null);
    try { await action(csrf); setMessage(isAr ? `تم ${label} بنجاح` : `${label} completed`); }
    catch (error) { setMessage(error instanceof Error ? error.message : (isAr ? "فشلت العملية" : "Operation failed")); }
    finally { setBusy(false); }
  };

  const reasonRequired = () => {
    if (reason.trim()) return true;
    setMessage(isAr ? "السبب إجباري لهذا الإجراء" : "A reason is required for this action");
    return false;
  };

  return (
    <section className="bg-slate-50 dark:bg-slate-800/50 p-4 rounded-xl border border-slate-200 dark:border-slate-700 space-y-3">
      <h2 className="text-sm font-semibold">{isAr ? "حالة الدورة وإجراءات الطوارئ" : "Course Lifecycle & Emergency Controls"}</h2>
      <p className="text-xs text-slate-500">{isAr ? "الحذف غير معروض هنا؛ الدورات ذات الوصول القائم يجب أرشفتها." : "Deletion is intentionally absent here; Courses with existing access must be archived."}</p>
      <div className="flex flex-wrap gap-2">
        <button disabled={busy} onClick={() => invoke(isAr ? "إلغاء الإدراج" : "Delist", (csrf) => delistCourse({ courseID, locale, csrf }))} className="px-3 py-2 rounded bg-amber-600 text-white text-xs disabled:opacity-50">{isAr ? "إلغاء الإدراج" : "Delist"}</button>
        <button disabled={busy} onClick={() => invoke(isAr ? "إعادة الإدراج" : "Relist", (csrf) => relistCourse({ courseID, locale, csrf }))} className="px-3 py-2 rounded bg-emerald-600 text-white text-xs disabled:opacity-50">{isAr ? "إعادة الإدراج" : "Relist"}</button>
        <button disabled={busy} onClick={() => invoke(isAr ? "التقاعد" : "Retire", (csrf) => retireCourse({ courseID, locale, csrf }))} className="px-3 py-2 rounded bg-slate-700 text-white text-xs disabled:opacity-50">{isAr ? "تقاعد" : "Retire"}</button>
        <button disabled={busy} onClick={() => invoke(isAr ? "الأرشفة" : "Archive", (csrf) => archiveCourse({ courseID, locale, csrf }))} className="px-3 py-2 rounded bg-rose-700 text-white text-xs disabled:opacity-50">{isAr ? "أرشفة" : "Archive"}</button>
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        <input value={ownerAccountID} onChange={(event) => setOwnerAccountID(event.target.value)} placeholder={isAr ? "معرف المدرّس الجديد" : "New Instructor account UUID"} className="p-2 border rounded text-xs bg-white dark:bg-slate-900" />
        <button disabled={busy || !ownerAccountID.trim()} onClick={() => invoke(isAr ? "إعادة تعيين المالك" : "Owner reassignment", (csrf) => reassignCourseOwner({ courseID, locale, csrf, ownerAccountID: ownerAccountID.trim() }))} className="px-3 py-2 rounded bg-blue-700 text-white text-xs disabled:opacity-50">{isAr ? "تغيير المالك" : "Reassign owner"}</button>
      </div>
      <div className="grid gap-2 sm:grid-cols-3">
        <select value={cause} onChange={(event) => setCause(event.target.value as SuspensionCause)} className="p-2 border rounded text-xs bg-white dark:bg-slate-900">
          <option value="LEGAL">{isAr ? "قانوني" : "Legal"}</option><option value="SECURITY">{isAr ? "أمني" : "Security"}</option><option value="MALWARE">{isAr ? "برمجيات خبيثة" : "Malware"}</option><option value="SEVERE_MODERATION">{isAr ? "إشراف جسيم" : "Severe moderation"}</option>
        </select>
        <input value={reason} onChange={(event) => setReason(event.target.value)} placeholder={isAr ? "سبب الإيقاف أو الاستعادة" : "Suspension or restoration reason"} className="p-2 border rounded text-xs bg-white dark:bg-slate-900" />
        <div className="flex gap-2"><button disabled={busy} onClick={() => { if (reasonRequired()) void invoke(isAr ? "إيقاف الوصول" : "Access suspension", (csrf) => suspendCourseAccess({ courseID, locale, csrf, cause, reason: reason.trim() })); }} className="px-3 py-2 rounded bg-rose-600 text-white text-xs disabled:opacity-50">{isAr ? "إيقاف" : "Suspend"}</button><button disabled={busy} onClick={() => { if (reasonRequired()) void invoke(isAr ? "استعادة الوصول" : "Access restoration", (csrf) => restoreCourseAccess({ courseID, locale, csrf, reason: reason.trim() })); }} className="px-3 py-2 rounded bg-emerald-700 text-white text-xs disabled:opacity-50">{isAr ? "استعادة" : "Restore"}</button></div>
      </div>
      {message && <p role="status" className="text-xs text-slate-700 dark:text-slate-300">{message}</p>}
    </section>
  );
}
