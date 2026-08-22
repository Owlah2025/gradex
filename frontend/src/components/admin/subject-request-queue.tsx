"use client";

import { useCallback, useEffect, useState } from "react";
import {
  duplicateSubjectConflict,
  listSubjects,
  subjectLabel,
  type Subject,
} from "@/lib/api/academic";
import {
  approveSubjectRequestAsNew,
  linkSubjectRequest,
  listAdminSubjectRequests,
  rejectSubjectRequest,
  type SubjectRequestWire,
} from "@/lib/api/subject-requests";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";

export function SubjectRequestQueue() {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [requests, setRequests] = useState<SubjectRequestWire[]>([]);
  const [queries, setQueries] = useState<Record<string, string>>({});
  const [results, setResults] = useState<Record<string, Subject[]>>({});
  const [selected, setSelected] = useState<Record<string, string>>({});
  const [reasons, setReasons] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");

  const refresh = useCallback(async () => {
    setRequests(await listAdminSubjectRequests(locale, "PENDING"));
  }, [locale]);

  useEffect(() => {
    refresh().catch((error) => setMessage(describeApiError(error, locale)));
  }, [locale, refresh]);

  const mutate = async (requestID: string, action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf || busy) return;
    setBusy(requestID);
    setMessage("");
    try {
      await action(csrf);
      await refresh();
      setMessage(isAr ? "تم تحديث طلب المادة." : "Subject request updated.");
    } catch (error) {
      const duplicate = duplicateSubjectConflict(error);
      setMessage(
        duplicate
          ? `${isAr ? "المادة موجودة بالفعل. استخدم الربط بمادة موجودة:" : "This Subject already exists. Use Link to Existing:"} ${subjectLabel(duplicate, locale)}`
          : describeApiError(error, locale),
      );
      // A compare-and-set conflict resolves the request while deliberately
      // leaving the Instructor's chosen Subject untouched, so refresh either way.
      try {
        await refresh();
      } catch (refreshError) {
        setMessage(describeApiError(refreshError, locale));
      }
    } finally {
      setBusy("");
    }
  };

  return (
    <section className="space-y-4 rounded-xl border border-amber-300 p-4" data-testid="subject-request-queue">
      <div>
        <h2 className="text-lg font-semibold">{isAr ? "طلبات المواد" : "Subject Requests"}</h2>
        <p className="text-xs text-slate-600 dark:text-slate-400">
          {isAr ? "راجع المواد التي لم يجدها المدرسون في الكتالوج الأكاديمي." : "Review Subjects Instructors could not find in the Academic Catalog."}
        </p>
      </div>
      {message && <p role="status" data-testid="subject-request-message" className="text-sm">{message}</p>}
      {requests.length === 0 ? (
        <p className="text-sm text-slate-600 dark:text-slate-400" data-testid="subject-request-empty">
          {isAr ? "لا توجد طلبات معلقة." : "No pending Subject requests."}
        </p>
      ) : requests.map((request) => (
        <article key={request.id} className="space-y-3 rounded-lg border border-slate-200 p-4 dark:border-slate-800" data-testid="subject-request-item">
          <dl className="grid gap-2 text-sm md:grid-cols-2">
            <div><dt className="text-xs font-semibold text-slate-500">{isAr ? "المدرس" : "Instructor"}</dt><dd>{request.requester_display_name}</dd></div>
            <div><dt className="text-xs font-semibold text-slate-500">{isAr ? "الكورس" : "Course"}</dt><dd>{isAr ? request.course_title_ar : request.course_title_en}</dd></div>
            <div><dt className="text-xs font-semibold text-slate-500">{isAr ? "الجامعة" : "University"}</dt><dd>{isAr ? request.institution_name_ar : request.institution_name_en}</dd></div>
            <div><dt className="text-xs font-semibold text-slate-500">{isAr ? "الرمز المقترح" : "Proposed code"}</dt><dd>{request.proposed_official_code || "—"}</dd></div>
            <div><dt className="text-xs font-semibold text-slate-500">{isAr ? "الاسم العربي" : "Arabic title"}</dt><dd dir="rtl">{request.proposed_title_ar}</dd></div>
            <div><dt className="text-xs font-semibold text-slate-500">{isAr ? "الاسم الإنجليزي" : "English title"}</dt><dd dir="ltr">{request.proposed_title_en}</dd></div>
          </dl>
          {(request.academic_context || request.note) && (
            <p className="text-xs text-slate-600 dark:text-slate-400" data-testid="subject-request-context">
              {[request.academic_context, request.note].filter(Boolean).join(" — ")}
            </p>
          )}

          <div className="space-y-2 rounded border border-slate-200 p-3 dark:border-slate-800">
            <label className="block text-xs font-semibold">
              {isAr ? "البحث عن مادة موجودة" : "Search existing Subjects"}
              <input
                value={queries[request.id] ?? ""}
                onChange={(event) => setQueries((current) => ({ ...current, [request.id]: event.target.value }))}
                data-testid="subject-request-existing-search"
                className="mt-1 w-full rounded border p-2 font-normal dark:bg-slate-900"
              />
            </label>
            <button
              type="button"
              disabled={busy === request.id}
              onClick={async () => {
                try {
                  const found = await listSubjects(request.institution_id, locale, queries[request.id] ?? "");
                  setResults((current) => ({ ...current, [request.id]: found }));
                } catch (error) {
                  setMessage(describeApiError(error, locale));
                }
              }}
              data-testid="subject-request-existing-search-submit"
              className="rounded border px-3 py-1 text-xs"
            >
              {isAr ? "بحث" : "Search"}
            </button>
            {(results[request.id] ?? []).length > 0 && (
              <select
                value={selected[request.id] ?? ""}
                onChange={(event) => setSelected((current) => ({ ...current, [request.id]: event.target.value }))}
                data-testid="subject-request-existing-result"
                className="w-full rounded border p-2 text-sm dark:bg-slate-900"
              >
                <option value="">{isAr ? "اختر المادة" : "Choose Subject"}</option>
                {(results[request.id] ?? []).map((subject) => (
                  <option key={subject.id} value={subject.id}>{subjectLabel(subject, locale)}</option>
                ))}
              </select>
            )}
            <button
              type="button"
              disabled={busy === request.id || !selected[request.id]}
              onClick={() => void mutate(request.id, async (csrf) => {
                await linkSubjectRequest({ requestID: request.id, subjectID: selected[request.id], locale, csrf });
              })}
              data-testid="subject-request-link"
              className="rounded bg-blue-600 px-3 py-1 text-xs text-white disabled:opacity-50"
            >
              {isAr ? "ربط بمادة موجودة" : "Link to Existing"}
            </button>
          </div>

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              disabled={busy === request.id}
              onClick={() => void mutate(request.id, async (csrf) => {
                await approveSubjectRequestAsNew({ requestID: request.id, locale, csrf });
              })}
              data-testid="subject-request-approve-new"
              className="rounded bg-emerald-700 px-3 py-1 text-xs text-white disabled:opacity-50"
            >
              {isAr ? "اعتماد كمادة جديدة" : "Approve as New"}
            </button>
            <input
              value={reasons[request.id] ?? ""}
              onChange={(event) => setReasons((current) => ({ ...current, [request.id]: event.target.value }))}
              placeholder={isAr ? "سبب الرفض (إجباري)" : "Rejection reason (required)"}
              data-testid="subject-request-reject-reason"
              className="min-w-64 flex-1 rounded border p-2 text-sm dark:bg-slate-900"
            />
            <button
              type="button"
              disabled={busy === request.id || !(reasons[request.id] ?? "").trim()}
              onClick={() => void mutate(request.id, async (csrf) => {
                await rejectSubjectRequest({ requestID: request.id, reason: reasons[request.id], locale, csrf });
              })}
              data-testid="subject-request-reject"
              className="rounded bg-rose-700 px-3 py-1 text-xs text-white disabled:opacity-50"
            >
              {isAr ? "رفض" : "Reject"}
            </button>
          </div>
        </article>
      ))}
    </section>
  );
}
