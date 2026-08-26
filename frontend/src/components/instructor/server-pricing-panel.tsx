"use client";

import React, { useState, useEffect, useCallback } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { formatFils } from "@/lib/formatters/currency";
import { courseDisplayTitle } from "./course-standing";
import {
  getOwnedCourses,
  getOwnedCourseDetail,
  type OwnedCourseSummary,
  type OwnedCourseDetail,
} from "@/lib/api/catalog";

function getDisplayRevision(c: OwnedCourseSummary) {
  return c.editable_revision || c.live_revision || null;
}

function getCourseDisplayTitle(c: OwnedCourseSummary, isAr: boolean, untitled: string) {
  return courseDisplayTitle(c, isAr ? "ar" : "en", untitled);
}

export function ServerPricingPanel() {
  const { locale, t } = useLocale();
  const isAr = locale === "ar";
  const untitled = t.instructor.courses.untitled;

  const [ownedCourses, setOwnedCourses] = useState<OwnedCourseSummary[]>([]);
  const [isLoadingOwned, setIsLoadingOwned] = useState(false);
  const [ownedError, setOwnedError] = useState<string | null>(null);

  const [selectedServerCourseID, setSelectedServerCourseID] = useState<string | null>(null);
  const [serverCourseDetail, setServerCourseDetail] = useState<OwnedCourseDetail | null>(null);
  const [isLoadingDetail, setIsLoadingDetail] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);

  const fetchOwnedCourses = useCallback(async () => {
    setIsLoadingOwned(true);
    setOwnedError(null);
    setSelectedServerCourseID(null);
    setServerCourseDetail(null);
    setDetailError(null);
    try {
      const list = await getOwnedCourses(locale);
      setOwnedCourses(list);
    } catch (err: unknown) {
      setOwnedError(
        err instanceof Error
          ? err.message
          : isAr
            ? "فشل في تحميل أسعار الدورات من الخادم"
            : "Failed to load server course prices"
      );
    } finally {
      setIsLoadingOwned(false);
    }
  }, [isAr, locale]);

  useEffect(() => {
    fetchOwnedCourses();
  }, [fetchOwnedCourses]);

  const handleSelectServerCourse = async (courseID: string) => {
    setSelectedServerCourseID(courseID);
    setServerCourseDetail(null);
    setDetailError(null);
    setIsLoadingDetail(true);
    try {
      const detail = await getOwnedCourseDetail(courseID, locale);
      setServerCourseDetail(detail);
    } catch (err: unknown) {
      setDetailError(
        err instanceof Error
          ? err.message
          : isAr
            ? "فشل في تحميل تفاصيل الدورة من الخادم"
            : "Failed to load course details from server"
      );
    } finally {
      setIsLoadingDetail(false);
    }
  };

  const activeRev = serverCourseDetail ? getDisplayRevision(serverCourseDetail) : null;
  const sections = activeRev?.sections ?? [];

  return (
    <div className="bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-xl p-6 space-y-4 shadow-sm">
      <div className="flex justify-between items-center border-b pb-3">
        <div>
          <h2 className="text-lg font-bold text-slate-900 dark:text-slate-100">
            {isAr ? "أسعار الخادم الرسمية (قراءة فقط من وقائع الخادم)" : "Official Server Prices (Read-only Server State)"}
          </h2>
          <p className="text-xs text-slate-500">
            {isAr
              ? "عرض أسعار الدورات والأقسام المستقرة المسترجعة مباشرة من استجابة الخادم"
              : "Inspect Course and stable-Section prices fetched live from the server API"}
          </p>
        </div>
        <button
          onClick={fetchOwnedCourses}
          className="px-3 py-1.5 bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 rounded text-xs font-medium hover:bg-slate-300 transition"
        >
          {isAr ? "تحديث البيانات" : "Refresh Server Reads"}
        </button>
      </div>

      {isLoadingOwned ? (
        <p className="text-xs text-slate-500 italic py-2">
          {isAr ? "جاري تحميل أسعار الدورات من الخادم..." : "Loading server course prices..."}
        </p>
      ) : ownedError ? (
        <p className="text-xs text-rose-600 font-medium py-2">{ownedError}</p>
      ) : ownedCourses.length === 0 ? (
        <p className="text-xs text-slate-500 italic py-2">
          {isAr
            ? "لا توجد دورات مملوكة على الخادم حالياً."
            : "No owned courses found on the server."}
        </p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="space-y-2">
            <span className="text-xs font-semibold block text-slate-700 dark:text-slate-300">
              {isAr ? "اختر دورة من الخادم:" : "Select Server Course:"}
            </span>
            {ownedCourses.map((c) => (
              <button
                key={c.id}
                onClick={() => handleSelectServerCourse(c.id)}
                className={`w-full text-start p-3 rounded-lg border text-xs transition ${
                  selectedServerCourseID === c.id
                    ? "border-blue-500 bg-blue-50/50 dark:bg-blue-950/20"
                    : "border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-800"
                }`}
              >
                <div className="font-semibold text-slate-900 dark:text-slate-100">
                  {getCourseDisplayTitle(c, isAr, untitled)}
                </div>
                {/* The Course identifier is deliberately not rendered. It carried no meaning for
                    the Instructor and became the clipboard source for an Admin workflow that
                    should never have needed one: every Admin surface now finds a Course by its
                    title and owner. */}
                <div className="mt-1 font-semibold text-emerald-600 dark:text-emerald-400">
                  {isAr ? "سعر الدورة: " : "Course Price: "}
                  {formatFils(c.price_minor_units, locale)}
                </div>
              </button>
            ))}
          </div>

          <div className="md:col-span-2 border rounded-lg p-4 bg-white dark:bg-slate-800">
            {isLoadingDetail ? (
              <p className="text-xs text-slate-500 italic">
                {isAr ? "جاري تحميل تفاصيل أسعار الأقسام..." : "Loading section price details..."}
              </p>
            ) : detailError ? (
              <p className="text-xs text-rose-600 font-medium">{detailError}</p>
            ) : serverCourseDetail ? (
              <div className="space-y-3">
                <div className="border-b pb-2">
                  <h3 className="text-sm font-bold text-slate-900 dark:text-slate-100">
                    {getCourseDisplayTitle(serverCourseDetail, isAr, untitled)}
                  </h3>
                  <div className="text-xs font-semibold text-emerald-600 dark:text-emerald-400 mt-1">
                    {isAr ? "سعر الدورة الحالي: " : "Current Course Price: "}
                    {formatFils(serverCourseDetail.price_minor_units, locale)}
                  </div>
                </div>

                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-slate-700 dark:text-slate-300">
                    {isAr ? "أسعار الأقسام المستقرة:" : "Stable Section Prices:"}
                  </h4>
                  {sections.length === 0 ? (
                    <p className="text-xs text-slate-500 italic">
                      {isAr ? "لا توجد أقسام في هذه الدورة." : "No sections in this course."}
                    </p>
                  ) : (
                    sections.map((sec) => (
                      <div
                        key={sec.id}
                        className="flex justify-between items-center p-2.5 bg-slate-50 dark:bg-slate-900 rounded border border-slate-200 dark:border-slate-700 text-xs"
                      >
                        <div>
                          <span className="font-semibold text-slate-900 dark:text-slate-100">
                            {isAr ? sec.title_ar : sec.title_en}
                          </span>
                        </div>
                        <span className="font-mono text-emerald-600 dark:text-emerald-400 font-semibold">
                          {formatFils(sec.price_minor_units, locale)}
                        </span>
                      </div>
                    ))
                  )}
                </div>
              </div>
            ) : (
              <p className="text-xs text-slate-500 italic">
                {isAr ? "اختر دورة من القائمة لعرض تفاصيل أسعار الخادم" : "Select a course to inspect server prices"}
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
