"use client";

import { useEffect, useState } from "react";
import { listOwnSubjectRequests, type SubjectRequestWire } from "@/lib/api/subject-requests";
import { useLocale } from "@/lib/i18n/locale-provider";

export type MissingSubjectInput = {
  proposedOfficialCode?: string;
  proposedTitleAr: string;
  proposedTitleEn: string;
  note?: string;
};

export function SubjectRequestState({
  courseID,
  busy,
  onRequest,
}: {
  courseID: string;
  busy: boolean;
  onRequest: (input: MissingSubjectInput) => Promise<boolean>;
}) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [requests, setRequests] = useState<SubjectRequestWire[]>([]);
  const [error, setError] = useState("");
  const [showForm, setShowForm] = useState(false);
  const [code, setCode] = useState("");
  const [titleAr, setTitleAr] = useState("");
  const [titleEn, setTitleEn] = useState("");
  const [note, setNote] = useState("");

  const refresh = async () => {
    const current = await listOwnSubjectRequests(locale, courseID);
    setRequests(current);
    setError("");
  };

  useEffect(() => {
    refresh().catch(() => setError(isAr ? "تعذر تحميل طلب المادة" : "Unable to load the Subject request"));
  }, [courseID, isAr, locale]);

  const submit = async () => {
    const saved = await onRequest({
      proposedOfficialCode: code,
      proposedTitleAr: titleAr,
      proposedTitleEn: titleEn,
      note,
    });
    if (!saved) return;
    try {
      await refresh();
      setShowForm(false);
    } catch {
      setError(isAr ? "تم إرسال الطلب، لكن تعذر تحديث حالته." : "The request was sent, but its status could not be refreshed.");
    }
  };

  const latest = requests[0];
  return (
    <div className="space-y-3" data-testid="academic-course-subject-request-state">
      {error && <p role="alert" className="text-xs text-red-700 dark:text-red-400">{error}</p>}
      {latest?.status === "PENDING" ? (
        <div data-testid="subject-request-pending">
          <p className="text-sm font-medium">{isAr ? "قيد المراجعة" : "Pending review"}</p>
          <p className="text-xs text-slate-600 dark:text-slate-400">
            {isAr
              ? "يمكنك متابعة إعداد الكورس. لن يصبح جاهزًا للإرسال حتى تربطه الإدارة بمادة رسمية."
              : "Keep building the Course. Submission stays unavailable until Admin links an official Subject."}
          </p>
        </div>
      ) : (
        <>
          {latest?.status === "REJECTED" && (
            <div data-testid="subject-request-rejected" className="rounded border border-rose-300 p-3">
              <p className="text-sm font-medium">{isAr ? "تم رفض طلب المادة" : "Subject request rejected"}</p>
              <p className="text-xs text-slate-600 dark:text-slate-400">{latest.resolution_reason}</p>
            </div>
          )}
          {!showForm ? (
            <button type="button" onClick={() => setShowForm(true)} disabled={busy}
              data-testid="academic-course-request-subject"
              className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs disabled:opacity-50">
              {isAr ? "لم أجد مادتي" : "I can't find my Subject"}
            </button>
          ) : (
            <div className="space-y-2" data-testid="academic-course-subject-request-form">
              <input value={code} onChange={(event) => setCode(event.target.value)}
                placeholder={isAr ? "الرمز الرسمي (اختياري)" : "Official code (optional)"}
                data-testid="academic-course-request-code" className="w-full rounded border p-2 text-sm dark:bg-slate-800" />
              <input value={titleAr} onChange={(event) => setTitleAr(event.target.value)}
                placeholder={isAr ? "اسم المادة بالعربية" : "Arabic Subject title"}
                data-testid="academic-course-request-title-ar" className="w-full rounded border p-2 text-sm dark:bg-slate-800" />
              <input value={titleEn} onChange={(event) => setTitleEn(event.target.value)}
                placeholder={isAr ? "اسم المادة بالإنجليزية" : "English Subject title"}
                data-testid="academic-course-request-title-en" className="w-full rounded border p-2 text-sm dark:bg-slate-800" />
              <textarea value={note} onChange={(event) => setNote(event.target.value)}
                placeholder={isAr ? "ملاحظة للإدارة" : "Note for Admin"}
                data-testid="academic-course-request-note" className="w-full rounded border p-2 text-sm dark:bg-slate-800" />
              <button type="button" disabled={busy || !titleAr.trim() || !titleEn.trim()} onClick={() => void submit()}
                data-testid="academic-course-submit-subject-request"
                className="rounded bg-blue-600 px-3 py-1 text-xs text-white disabled:opacity-50">
                {isAr ? "إرسال الطلب" : "Send request"}
              </button>
            </div>
          )}
        </>
      )}
    </div>
  );
}
