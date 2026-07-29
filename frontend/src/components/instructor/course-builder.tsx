"use client";

import React, { useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ServerPricingPanel } from "./server-pricing-panel";
import { TaxonomyAssignmentPanel } from "./taxonomy-assignment-panel";

interface Section {
  id: string;
  title_ar: string;
  title_en: string;
  position: number;
  lessons: Lesson[];
}

interface Lesson {
  id: string;
  title_ar: string;
  title_en: string;
  position: number;
  video_asset_version_id?: string;
  files: LessonFile[];
}

interface LessonFile {
  id: string;
  kind: "RESOURCE" | "LAB_MATERIAL";
  asset_version_id: string;
  display_name_ar: string;
  display_name_en: string;
  position: number;
}

interface Course {
  id: string;
  lifecycle: string;
  title_ar: string;
  title_en: string;
  description_ar: string;
  description_en: string;
  major_term_id?: string;
  subject_term_id?: string;
  study_year?: string;
  preview_asset_version_id?: string;
  sections: Section[];
}

export function CourseBuilder() {
  const { locale, dir } = useLocale();
  const isAr = locale === "ar";

  const [courses, setCourses] = useState<Course[]>([
    {
      id: "course-demo-1",
      lifecycle: "DRAFT",
      title_ar: "مقدمة في البرمجة باللغة العربية",
      title_en: "Introduction to Programming in Arabic",
      description_ar: "دورة تعليمية شاملة للمبتدئين في مجال برمجة الحاسوب.",
      description_en: "A comprehensive introductory course to computer programming.",
      study_year: "YEAR_1",
      sections: [
        {
          id: "sec-1",
          title_ar: "المفاهيم الأساسية",
          title_en: "Basic Concepts",
          position: 1,
          lessons: [
            {
              id: "les-1",
              title_ar: "الدرس الأول: المتغيرات",
              title_en: "Lesson 1: Variables",
              position: 1,
              video_asset_version_id: "11111111-1111-1111-1111-111111111111",
              files: [
                {
                  id: "file-1",
                  kind: "RESOURCE",
                  asset_version_id: "22222222-2222-2222-2222-222222222222",
                  display_name_ar: "ملف الشرح PDF",
                  display_name_en: "Lecture PDF",
                  position: 1,
                },
              ],
            },
          ],
        },
      ],
    },
  ]);

  const [selectedCourse, setSelectedCourse] = useState<Course | null>(courses[0]);

  const [newTitleAr, setNewTitleAr] = useState("");
  const [newTitleEn, setNewTitleEn] = useState("");
  const [newDescAr, setNewDescAr] = useState("");
  const [newDescEn, setNewDescEn] = useState("");
  const [isCreating, setIsCreating] = useState(false);

  const [secTitleAr, setSecTitleAr] = useState("");
  const [secTitleEn, setSecTitleEn] = useState("");

  const handleCreateCourse = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTitleAr || !newTitleEn) return;

    const newCourse: Course = {
      id: `course-${Date.now()}`,
      lifecycle: "DRAFT",
      title_ar: newTitleAr,
      title_en: newTitleEn,
      description_ar: newDescAr,
      description_en: newDescEn,
      sections: [],
    };

    setCourses([...courses, newCourse]);
    setSelectedCourse(newCourse);
    setNewTitleAr("");
    setNewTitleEn("");
    setNewDescAr("");
    setNewDescEn("");
    setIsCreating(false);
  };

  const handleAddSection = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedCourse || !secTitleAr || !secTitleEn) return;

    const newSec: Section = {
      id: `sec-${Date.now()}`,
      title_ar: secTitleAr,
      title_en: secTitleEn,
      position: selectedCourse.sections.length + 1,
      lessons: [],
    };

    const updated = {
      ...selectedCourse,
      sections: [...selectedCourse.sections, newSec],
    };

    setSelectedCourse(updated);
    setCourses(courses.map((c) => (c.id === updated.id ? updated : c)));
    setSecTitleAr("");
    setSecTitleEn("");
  };

  return (
    <div dir={dir} className="max-w-7xl mx-auto px-4 py-8 space-y-8">
      <header className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 border-b pb-6">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-slate-900 dark:text-slate-100">
            {isAr ? "منصة إعداد الدورات التعليمية" : "Course Authoring Studio"}
          </h1>
          <p className="text-sm text-slate-600 dark:text-slate-400 mt-1">
            {isAr
              ? "إنشاء وإدارة المحتوى التعليمي في بيئة خاصة قبل الاعتماد والنشر"
              : "Build and manage private course drafts before submission & review"}
          </p>
        </div>
        <button
          onClick={() => setIsCreating(!isCreating)}
          className="inline-flex items-center justify-center rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition"
        >
          {isCreating
            ? isAr ? "إلغاء الإضافة" : "Cancel"
            : isAr ? "إنشاء دورة جديدة" : "New Course"}
        </button>
      </header>

      <ServerPricingPanel />

      <TaxonomyAssignmentPanel />

      {isCreating && (
        <form
          onSubmit={handleCreateCourse}
          className="bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-lg p-6 space-y-4"
        >
          <h2 className="text-lg font-semibold">
            {isAr ? "تفاصيل الدورة الجديدة" : "New Course Details"}
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">
                {isAr ? "عنوان الدورة (بالعربية)" : "Course Title (Arabic)"}
              </label>
              <input
                type="text"
                value={newTitleAr}
                onChange={(e) => setNewTitleAr(e.target.value)}
                required
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">
                {isAr ? "عنوان الدورة (بالإنجليزية)" : "Course Title (English)"}
              </label>
              <input
                type="text"
                value={newTitleEn}
                onChange={(e) => setNewTitleEn(e.target.value)}
                required
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">
                {isAr ? "الوصف (بالعربية)" : "Description (Arabic)"}
              </label>
              <textarea
                value={newDescAr}
                onChange={(e) => setNewDescAr(e.target.value)}
                rows={3}
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">
                {isAr ? "الوصف (بالإنجليزية)" : "Description (English)"}
              </label>
              <textarea
                value={newDescEn}
                onChange={(e) => setNewDescEn(e.target.value)}
                rows={3}
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </div>
          </div>
          <button
            type="submit"
            className="rounded-md bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700"
          >
            {isAr ? "حفظ كمسودة" : "Save as Draft"}
          </button>
        </form>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="space-y-3">
          <h2 className="text-md font-semibold text-slate-700 dark:text-slate-300">
            {isAr ? "الدورات المحلية (نموذج إعداد)" : "Local Demo Drafts"}
          </h2>
          <div className="space-y-2">
            {courses.map((c) => (
              <div
                key={c.id}
                onClick={() => setSelectedCourse(c)}
                className={`p-4 rounded-lg border cursor-pointer transition ${
                  selectedCourse?.id === c.id
                    ? "border-blue-500 bg-blue-50/50 dark:bg-blue-950/20"
                    : "border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900"
                }`}
              >
                <div className="flex items-center justify-between">
                  <h3 className="font-semibold text-sm">
                    {isAr ? c.title_ar : c.title_en}
                  </h3>
                  <span className="text-xs px-2 py-0.5 rounded font-mono bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">
                    {c.lifecycle}
                  </span>
                </div>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-1 line-clamp-2">
                  {isAr ? c.description_ar : c.description_en}
                </p>
              </div>
            ))}
          </div>
        </div>

        {selectedCourse ? (
          <div className="md:col-span-2 space-y-6 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-lg p-6">
            <div className="flex items-center justify-between border-b pb-4">
              <div>
                <span className="text-xs font-mono text-blue-600 dark:text-blue-400 font-semibold">
                  ID: {selectedCourse.id}
                </span>
                <h2 className="text-xl font-bold mt-0.5">
                  {isAr ? selectedCourse.title_ar : selectedCourse.title_en}
                </h2>
              </div>
              <span className="text-xs px-3 py-1 rounded-full bg-slate-100 dark:bg-slate-800 font-medium">
                {isAr ? "حالة الدورة: مسودة خاصة" : "Status: Private Draft"}
              </span>
            </div>

            <div className="space-y-4">
              <h3 className="text-md font-semibold">
                {isAr ? "أقسام الدورة والدروس" : "Sections & Lessons"}
              </h3>

              {selectedCourse.sections.length === 0 ? (
                <p className="text-sm text-slate-500 italic">
                  {isAr ? "لا يوجد أقسام مضافة بعد." : "No sections added yet."}
                </p>
              ) : (
                <div className="space-y-4">
                  {selectedCourse.sections.map((sec, idx) => (
                    <div
                      key={sec.id}
                      className="border border-slate-200 dark:border-slate-800 rounded-md p-4 bg-slate-50/50 dark:bg-slate-950/30"
                    >
                      <div className="flex items-center justify-between mb-3">
                        <span className="font-semibold text-sm">
                          {idx + 1}. {isAr ? sec.title_ar : sec.title_en}
                        </span>
                      </div>
                      <div className="space-y-2 pl-4 pr-4 border-l-2 border-slate-300 dark:border-slate-700">
                        {sec.lessons.map((les, lIdx) => (
                          <div
                            key={les.id}
                            className="bg-white dark:bg-slate-900 p-3 rounded border text-xs flex flex-col gap-1"
                          >
                            <div className="flex items-center justify-between font-medium">
                              <span>
                                {lIdx + 1}. {isAr ? les.title_ar : les.title_en}
                              </span>
                              {les.video_asset_version_id ? (
                                <span className="text-emerald-600 dark:text-emerald-400 font-mono text-[10px]">
                                  Video Ref: {les.video_asset_version_id.substring(0, 8)}...
                                </span>
                              ) : (
                                <span className="text-slate-400">
                                  {isAr ? "لا يوجد فيديو" : "No Video Ref"}
                                </span>
                              )}
                            </div>
                            {les.files.length > 0 && (
                              <div className="text-[11px] text-slate-500 mt-1">
                                {les.files.map((f) => (
                                  <span key={f.id} className="mr-2 inline-block bg-slate-100 dark:bg-slate-800 px-1.5 py-0.5 rounded">
                                    [{f.kind}] {isAr ? f.display_name_ar : f.display_name_en}
                                  </span>
                                ))}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}

              <form onSubmit={handleAddSection} className="flex flex-col gap-2 pt-2 lg:flex-row">
                <input
                  type="text"
                  placeholder={isAr ? "عنوان القسم بالعربية" : "Section Title (Arabic)"}
                  value={secTitleAr}
                  onChange={(e) => setSecTitleAr(e.target.value)}
                  className="flex-1 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-xs"
                />
                <input
                  type="text"
                  placeholder={isAr ? "عنوان القسم بالإنجليزية" : "Section Title (English)"}
                  value={secTitleEn}
                  onChange={(e) => setSecTitleEn(e.target.value)}
                  className="flex-1 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-xs"
                />
                <button
                  type="submit"
                  className="rounded-md bg-blue-600 px-3 py-2 text-xs font-medium text-white hover:bg-blue-700"
                >
                  {isAr ? "إضافة قسم" : "Add Section"}
                </button>
              </form>
            </div>
          </div>
        ) : (
          <div className="md:col-span-2 border border-dashed rounded-lg p-12 text-center text-slate-500">
            {isAr ? "اختر دورة لعرض المحتوى والتعديل" : "Select a course to edit content"}
          </div>
        )}
      </div>
    </div>
  );
}
