"use client";

import React, { useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { PricingModal } from "./pricing-modal";
import { LifecycleControls } from "./lifecycle-controls";
import { TaxonomyControls } from "./taxonomy-controls";

export interface ReviewQueueItem {
  course_id: string;
  owner_account_id: string;
  revision_id: string;
  revision_number: number;
  title_ar: string;
  title_en: string;
  submitted_at?: string;
  course_lifecycle: string;
  is_first_publish: boolean;
}

export function ReviewQueue() {
  const { locale, dir } = useLocale();
  const isAr = locale === "ar";

  const [items, setItems] = useState<ReviewQueueItem[]>([
    {
      course_id: "demo-course-1",
      owner_account_id: "inst-1",
      revision_id: "rev-1",
      revision_number: 1,
      title_ar: "مقدمة في البرمجة بالعربية",
      title_en: "Introduction to Programming",
      submitted_at: new Date().toISOString(),
      course_lifecycle: "PENDING_REVIEW",
      is_first_publish: true,
    },
  ]);

  const [selectedCourse, setSelectedCourse] = useState<ReviewQueueItem | null>(null);
  const [requestReason, setRequestReason] = useState("");
  const [showRejectModal, setShowRejectModal] = useState(false);
  const [reasonError, setReasonError] = useState("");
  const [actionSuccess, setActionSuccess] = useState("");

  const [launcherCourseID, setLauncherCourseID] = useState("");
  const [launcherError, setLauncherError] = useState("");
  const [pricingCourseID, setPricingCourseID] = useState<string | null>(null);

  const handleApprove = (courseId: string) => {
    setItems((prev) => prev.filter((i) => i.course_id !== courseId));
    setActionSuccess(isAr ? "تم نشر الدورة بنجاح" : "Course published successfully");
    setTimeout(() => setActionSuccess(""), 4000);
  };

  const handleOpenRequestChanges = (item: ReviewQueueItem) => {
    setSelectedCourse(item);
    setRequestReason("");
    setReasonError("");
    setShowRejectModal(true);
  };

  const handleSubmitRequestChanges = () => {
    if (!requestReason.trim()) {
      setReasonError(isAr ? "سبب طلب التعديلات إجباري" : "Reason for change request is mandatory");
      return;
    }
    if (selectedCourse) {
      setItems((prev) => prev.filter((i) => i.course_id !== selectedCourse.course_id));
      setActionSuccess(isAr ? "تم إرسال طلب التعديلات إلى المحاضر" : "Change request sent to instructor");
      setShowRejectModal(false);
      setSelectedCourse(null);
      setRequestReason("");
      setTimeout(() => setActionSuccess(""), 4000);
    }
  };

  const handleLaunchPricing = (e: React.FormEvent) => {
    e.preventDefault();
    const id = launcherCourseID.trim();
    if (!id) {
      setLauncherError(isAr ? "معرف الدورة (UUID) إجباري" : "Course UUID is required");
      return;
    }
    setLauncherError("");
    setPricingCourseID(id);
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
        <span className="px-3 py-1 bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-300 rounded-full text-sm font-medium">
          {items.length} {isAr ? "معلقة" : "Pending"}
        </span>
      </div>

      <div className="bg-slate-50 dark:bg-slate-800/50 p-4 rounded-xl border border-slate-200 dark:border-slate-700 space-y-3">
        <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">
          {isAr ? "فتح أدوات التسعير لدورة محددة (معرف UUID)" : "Open Pricing Controls for Course UUID"}
        </h2>
        <form onSubmit={handleLaunchPricing} className="flex flex-col sm:flex-row gap-3">
          <div className="flex-1">
            <input
              type="text"
              value={launcherCourseID}
              onChange={(e) => {
                setLauncherCourseID(e.target.value);
                setLauncherError("");
              }}
              placeholder={
                isAr ? "أدخل معرف الدورة (مثال: 00000000-0000-0000-0000-000000000000)" : "Enter Course UUID (e.g. 00000000-0000-0000-0000-000000000000)"
              }
              className="w-full p-2.5 border rounded-lg text-xs bg-white dark:bg-slate-900 border-slate-300 dark:border-slate-700 font-mono text-slate-900 dark:text-slate-100"
            />
            {launcherError && <p className="text-xs text-rose-600 font-medium mt-1">{launcherError}</p>}
          </div>
          <button
            type="submit"
            className="px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-xs font-semibold transition"
          >
            {isAr ? "فتح إدارة التسعير" : "Manage Pricing"}
          </button>
        </form>
      </div>

      {actionSuccess && (
        <div className="p-4 bg-emerald-50 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300 border border-emerald-200 dark:border-emerald-800 rounded-lg">
          {actionSuccess}
        </div>
      )}

      {items.length === 0 ? (
        <div className="text-center py-12 border rounded-lg text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-900/50">
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
                <tr key={item.course_id} className="hover:bg-slate-50 dark:hover:bg-slate-900/50">
                  <td className="p-3 border-e font-medium">
                    <div>{isAr ? item.title_ar : item.title_en}</div>
                    <div className="text-xs text-slate-400 font-mono">{item.course_id}</div>
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
                        onClick={() => handleApprove(item.course_id)}
                        className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded text-xs font-medium transition"
                      >
                        {isAr ? "موافقة ونشر" : "Approve & Publish"}
                      </button>
                      <button
                        onClick={() => handleOpenRequestChanges(item)}
                        className="px-3 py-1.5 bg-rose-600 hover:bg-rose-700 text-white rounded text-xs font-medium transition"
                      >
                        {isAr ? "طلب تعديلات" : "Request Changes"}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showRejectModal && selectedCourse && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center p-4 z-50">
          <div className="bg-white dark:bg-slate-900 rounded-xl p-6 max-w-lg w-full space-y-4 shadow-xl border">
            <h3 className="text-lg font-bold text-slate-900 dark:text-slate-100 border-b pb-2">
              {isAr ? "طلب تعديلات على الدورة" : "Request Changes for Course"}
            </h3>
            <p className="text-sm text-slate-600 dark:text-slate-300">
              {isAr ? selectedCourse.title_ar : selectedCourse.title_en}
            </p>
            <div className="space-y-1">
              <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                {isAr ? "سبب طلب التعديلات (إجباري):" : "Reason for Change Request (Mandatory):"}
              </label>
              <textarea
                value={requestReason}
                onChange={(e) => {
                  setRequestReason(e.target.value);
                  setReasonError("");
                }}
                rows={4}
                className="w-full p-2.5 border rounded-lg text-sm bg-slate-50 dark:bg-slate-800 dark:border-slate-700 text-slate-900 dark:text-slate-100 focus:ring-2 focus:ring-rose-500"
                placeholder={
                  isAr ? "يرجى ذكر الملاحظات التفصيلية للمحاضر..." : "Provide detailed feedback for the instructor..."
                }
              />
              {reasonError && <p className="text-xs text-rose-600 font-medium">{reasonError}</p>}
            </div>
            <div className="flex justify-end gap-2 pt-2 border-t">
              <button
                onClick={() => setShowRejectModal(false)}
                className="px-4 py-2 bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 hover:bg-slate-300 rounded-lg text-sm font-medium"
              >
                {isAr ? "إلغاء" : "Cancel"}
              </button>
              <button
                onClick={handleSubmitRequestChanges}
                className="px-4 py-2 bg-rose-600 hover:bg-rose-700 text-white rounded-lg text-sm font-medium"
              >
                {isAr ? "إرسال التعديلات" : "Submit Request"}
              </button>
            </div>
          </div>
        </div>
      )}

      {pricingCourseID && (
        <PricingModal
          courseID={pricingCourseID}
          onClose={() => setPricingCourseID(null)}
        />
      )}

      {pricingCourseID && <LifecycleControls courseID={pricingCourseID} />}

      {pricingCourseID && <TaxonomyControls courseID={pricingCourseID} />}
    </div>
  );
}
