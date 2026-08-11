"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  listReviewQueue,
  type ReviewQueueItem,
} from "@/lib/api/review";
import { describeApiError } from "@/lib/api/api-error";
import {
  administerCourse,
  administeredCourseID,
  administeredRevisionID,
  closePricingModal,
  initialAdminCatalogState,
  lifecycleControlsVisible,
  openPricingModal,
  pricingModalVisible,
  stopAdministeringCourse,
  taxonomyControlsVisible,
  type AdminCatalogState,
} from "@/lib/admin/catalog-administration";
import { PricingModal } from "./pricing-modal";
import { LifecycleControls } from "./lifecycle-controls";
import { TaxonomyControls } from "./taxonomy-controls";
import { SubmittedRevisionInspector } from "./submitted-revision-inspector";

export type { ReviewQueueItem } from "@/lib/api/review";

/**
 * Admin Catalog review surface.
 *
 * The queue rendered here is the server's set of `PENDING_REVIEW` revisions,
 * read from `/admin/review/queue`. There is no local fixture and no fallback
 * content: an empty response renders an empty queue, because a Course the
 * founder never submitted must never appear as if it were waiting.
 *
 * Which Course is under administration is tracked separately from whether the
 * pricing dialog is open, so closing that dialog cannot take taxonomy or
 * lifecycle administration away with it.
 */
export function ReviewQueue() {
  const { locale, dir } = useLocale();
  const isAr = locale === "ar";

  const [items, setItems] = useState<ReviewQueueItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [queueError, setQueueError] = useState<string | null>(null);

  const [catalogState, setCatalogState] = useState<AdminCatalogState>(initialAdminCatalogState);
  const courseUnderAdministration = administeredCourseID(catalogState);
  const revisionUnderAdministration = administeredRevisionID(catalogState);

  const [inspectedItem, setInspectedItem] = useState<ReviewQueueItem | null>(null);

  const [launcherCourseID, setLauncherCourseID] = useState("");
  const [launcherError, setLauncherError] = useState("");

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

  const handleAdminister = (item: ReviewQueueItem) => {
    setCatalogState((current) =>
      administerCourse(current, {
        courseID: item.course_id,
        revisionID: item.revision_id,
        titleAr: item.title_ar,
        titleEn: item.title_en,
      }),
    );
  };

  const handleAdministerByID = (e: React.FormEvent) => {
    e.preventDefault();
    const id = launcherCourseID.trim();
    if (!id) {
      setLauncherError(isAr ? "معرف الدورة (UUID) إجباري" : "Course UUID is required");
      return;
    }
    setLauncherError("");
    setCatalogState((current) => administerCourse(current, { courseID: id, revisionID: null }));
  };

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

      <div className="bg-slate-50 dark:bg-slate-800/50 p-4 rounded-xl border border-slate-200 dark:border-slate-700 space-y-3">
        <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
          {isAr ? "إدارة دورة محددة (معرف UUID)" : "Administer a Course by UUID"}
        </h2>
        <p className="text-xs text-slate-500 dark:text-slate-400">
          {isAr
            ? "تبقى إدارة التصنيف وحالة الدورة متاحة بعد إغلاق نافذة التسعير."
            : "Taxonomy and lifecycle administration remain available after the pricing dialog is closed."}
        </p>
        <form onSubmit={handleAdministerByID} className="flex flex-col sm:flex-row gap-3">
          <div className="flex-1">
            <input
              type="text"
              value={launcherCourseID}
              onChange={(e) => {
                setLauncherCourseID(e.target.value);
                setLauncherError("");
              }}
              data-testid="administer-course-id"
              placeholder={
                isAr ? "أدخل معرف الدورة (مثال: 00000000-0000-0000-0000-000000000000)" : "Enter Course UUID (e.g. 00000000-0000-0000-0000-000000000000)"
              }
              className="w-full p-2.5 border rounded-lg text-xs bg-white dark:bg-slate-900 border-slate-300 dark:border-slate-700 font-mono text-slate-900 dark:text-slate-100"
            />
            {launcherError && <p className="text-xs text-rose-600 font-medium mt-1">{launcherError}</p>}
          </div>
          <button
            type="submit"
            data-testid="administer-course"
            className="px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-xs font-semibold transition"
          >
            {isAr ? "إدارة الدورة" : "Administer Course"}
          </button>
        </form>
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
                    <div className="text-xs text-slate-400 font-mono" data-testid={`review-item-course-id-${item.course_id}`}>
                      {item.course_id}
                    </div>
                    <div className="text-xs text-slate-400 font-mono" data-testid={`review-item-revision-id-${item.course_id}`}>
                      {item.revision_id}
                    </div>
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
                    <div className="flex justify-center items-center gap-2">
                      <button
                        type="button"
                        onClick={() => setInspectedItem(item)}
                        data-testid={`inspect-review-item-${item.course_id}`}
                        className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-700 text-white rounded text-xs font-medium transition"
                      >
                        {isAr ? "فحص المراجعة" : "Inspect submission"}
                      </button>
                      <button
                        type="button"
                        onClick={() => handleAdminister(item)}
                        data-testid={`administer-review-item-${item.course_id}`}
                        className="px-3 py-1.5 bg-slate-700 hover:bg-slate-800 text-white rounded text-xs font-medium transition"
                      >
                        {isAr ? "فتح الإدارة" : "Administer"}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {inspectedItem && (
        <SubmittedRevisionInspector
          item={inspectedItem}
          onClose={() => setInspectedItem(null)}
          onReviewed={async () => {
            await loadQueue();
          }}
        />
      )}

      {courseUnderAdministration && (
        <div
          data-testid="administered-course"
          className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-blue-200 bg-blue-50/60 p-4 dark:border-blue-900 dark:bg-blue-950/20"
        >
          <div className="text-xs text-slate-700 dark:text-slate-300">
            <p className="font-semibold">{isAr ? "الدورة قيد الإدارة" : "Course under administration"}</p>
            <p className="font-mono" data-testid="administered-course-id">{courseUnderAdministration}</p>
            {revisionUnderAdministration && (
              <p className="font-mono" data-testid="administered-revision-id">{revisionUnderAdministration}</p>
            )}
          </div>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => setCatalogState(openPricingModal)}
              data-testid="open-pricing-modal"
              className="px-3 py-2 rounded bg-blue-600 hover:bg-blue-700 text-white text-xs font-semibold"
            >
              {isAr ? "إدارة التسعير" : "Manage Pricing"}
            </button>
            <button
              type="button"
              onClick={() => setCatalogState(stopAdministeringCourse)}
              data-testid="stop-administering-course"
              className="px-3 py-2 rounded bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 text-xs font-semibold"
            >
              {isAr ? "إنهاء الإدارة" : "Close Course"}
            </button>
          </div>
        </div>
      )}

      {pricingModalVisible(catalogState) && courseUnderAdministration && (
        <PricingModal
          courseID={courseUnderAdministration}
          courseTitleAr={catalogState.administered?.titleAr}
          courseTitleEn={catalogState.administered?.titleEn}
          onClose={() => setCatalogState(closePricingModal)}
        />
      )}

      {lifecycleControlsVisible(catalogState) && courseUnderAdministration && (
        <LifecycleControls courseID={courseUnderAdministration} />
      )}

      {taxonomyControlsVisible(catalogState) && courseUnderAdministration && (
        <TaxonomyControls
          courseID={courseUnderAdministration}
          defaultRevisionID={revisionUnderAdministration ?? undefined}
        />
      )}
    </div>
  );
}
