"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  listReviewQueue,
  type ReviewQueueItem,
} from "@/lib/api/review";
import { describeApiError } from "@/lib/api/api-error";
import { TaxonomyVocabularyPanel } from "./taxonomy-vocabulary-panel";

export type { ReviewQueueItem } from "@/lib/api/review";

/**
 * Admin Catalog review surface.
 *
 * The queue rendered here is the server's set of `PENDING_REVIEW` revisions,
 * read from `/admin/review/queue`. There is no local fixture and no fallback
 * content: an empty response renders an empty queue, because a Course the
 * founder never submitted must never appear as if it were waiting.
 *
 * Selecting one queue row opens the review workspace for that Course at its own
 * address (`/<locale>/admin/courses/<id>/review`), which is where inspection,
 * taxonomy override, pricing, preview and the decision live. The workspace used
 * to be component state here, so a review could not be linked, reloaded or
 * returned to with Back; the route is what makes it addressable, and what lets
 * the Courses directory send an Admin straight to the right Course.
 */
export function ReviewQueue() {
  const { locale, dir } = useLocale();
  const isAr = locale === "ar";

  const [items, setItems] = useState<ReviewQueueItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [queueError, setQueueError] = useState<string | null>(null);

  const loadQueue = useCallback(async () => {
    setQueueError(null);
    try {
      setItems(await listReviewQueue(locale));
    } catch (cause) {
      setItems([]);
      setQueueError(describeApiError(cause, locale));
    }
  }, [locale]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    loadQueue().finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [loadQueue]);

  return (
    <div dir={dir} className="max-w-6xl mx-auto p-6 space-y-6">
      <div className="flex justify-between items-center border-b pb-4">
        <div>
          <h1 className="text-2xl font-bold text-slate-900 dark:text-slate-100">
            {isAr ? "قائمة مراجعة وتسعير الدورات" : "Course Review & Pricing Admin"}
          </h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">
            {isAr
              ? "إدارة مراجعة الدورات، تحديد أسعار الدورات والأقسام، وعرض سجل التغييرات التاريخي"
              : "Review submitted courses, manage Course/Section pricing, and inspect audit history"}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => void loadQueue()}
            data-testid="refresh-review-queue"
            className="px-3 py-1.5 rounded border border-slate-300 dark:border-slate-700 text-xs font-medium text-slate-700 dark:text-slate-300"
          >
            {isAr ? "تحديث" : "Refresh"}
          </button>
          <span
            data-testid="review-queue-count"
            className="px-3 py-1 bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-300 rounded-full text-sm font-medium"
          >
            {items.length} {isAr ? "معلقة" : "Pending"}
          </span>
        </div>
      </div>

      {queueError && (
        <p
          role="alert"
          data-testid="review-queue-error"
          className="p-4 bg-rose-50 text-rose-800 dark:bg-rose-950/40 dark:text-rose-300 border border-rose-200 dark:border-rose-900 rounded-lg text-sm"
        >
          {queueError}
        </p>
      )}

      {loading ? (
        <div data-testid="review-queue-loading" className="text-center py-12 border rounded-lg text-slate-500 dark:text-slate-400">
          {isAr ? "جارٍ تحميل قائمة المراجعة..." : "Loading the review queue..."}
        </div>
      ) : items.length === 0 ? (
        <div
          data-testid="review-queue-empty"
          className="text-center py-12 border rounded-lg text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-900/50"
        >
          {isAr ? "لا توجد دورات قيد المراجعة حالياً." : "No courses pending review currently."}
        </div>
      ) : (
        <div className="overflow-x-auto border rounded-lg shadow-sm">
          <table className="w-full text-sm text-start">
            <thead className="bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 font-semibold border-b">
              <tr>
                <th className="p-3 border-e">{isAr ? "عنوان الدورة" : "Course Title"}</th>
                <th className="p-3 border-e">{isAr ? "رقم المراجعة" : "Revision #"}</th>
                <th className="p-3 border-e">{isAr ? "نوع النشر" : "Publish Type"}</th>
                <th className="p-3 border-e">{isAr ? "تاريخ التقديم" : "Submitted Date"}</th>
                <th className="p-3 text-center">{isAr ? "الإجراءات" : "Actions"}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200 dark:divide-slate-800">
              {items.map((item) => (
                <tr
                  key={item.revision_id}
                  data-testid={`review-item-${item.course_id}`}
                  className="hover:bg-slate-50 dark:hover:bg-slate-900/50"
                >
                  <td className="p-3 border-e font-medium">
                    <div>{isAr ? item.title_ar : item.title_en}</div>
                    <div className="text-xs font-normal text-slate-500">{isAr ? item.title_en : item.title_ar}</div>
                  </td>
                  <td className="p-3 border-e">v{item.revision_number}</td>
                  <td className="p-3 border-e">
                    {item.is_first_publish ? (
                      <span className="px-2 py-0.5 bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-300 text-xs rounded">
                        {isAr ? "نشر لأول مرة" : "First Publication"}
                      </span>
                    ) : (
                      <span className="px-2 py-0.5 bg-purple-100 text-purple-800 dark:bg-purple-900/40 dark:text-purple-300 text-xs rounded">
                        {isAr ? "تعديل مراجعة" : "Pending Revision"}
                      </span>
                    )}
                  </td>
                  <td className="p-3 border-e text-slate-500">
                    {item.submitted_at ? new Date(item.submitted_at).toLocaleDateString() : "-"}
                  </td>
                  <td className="p-3">
                    <div className="flex justify-center items-center">
                      {/* A link, not in-page state: the review workspace has its own address, so a
                          decision can be reloaded, shared and returned to with Back. */}
                      <Link
                        href={`/${locale}/admin/courses/${item.course_id}/review`}
                        data-testid={`inspect-review-item-${item.course_id}`}
                        className="rounded-md bg-primary px-3 py-1.5 text-xs font-semibold text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                      >
                        {isAr ? "فتح مساحة المراجعة" : "Open review workspace"}
                      </Link>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <TaxonomyVocabularyPanel />
    </div>
  );
}
