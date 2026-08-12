"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ServerPricingPanel } from "./server-pricing-panel";
import { TaxonomyAssignmentPanel } from "./taxonomy-assignment-panel";
import { LessonVideoUpload } from "./lesson-video-upload";
import {
  addLesson,
  addSection,
  createCourse,
  deleteLesson,
  deleteSection,
  getOwnedCourseDetail,
  getOwnedCourses,
  submitCourseRevision,
  updateCourseRevision,
  type CourseWire,
} from "@/lib/api/authoring";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";

/**
 * Instructor Course Authoring Studio.
 *
 * Every Course, Section, and Lesson rendered here comes from the Go API and is
 * written back through it. The component holds no authored content of its own:
 * after each successful command it re-reads the owned-Course graph, so what the
 * Instructor sees is what a page reload would show.
 */
export function CourseBuilder() {
  const { locale, dir } = useLocale();
  const isAr = locale === "ar";

  const [courses, setCourses] = useState<CourseWire[]>([]);
  const [selectedCourseID, setSelectedCourseID] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // A submission rejection is reported twice: once in the page-level error
  // region, and once beside the Submit control itself. The founder's manual
  // test clicked Submit near the bottom of a long page, saw nothing change,
  // and read the click as a no-op — the server's reason was rendered far
  // above the viewport. The second region is focused on failure so the reason
  // is where the click was.
  const [submitError, setSubmitError] = useState<string | null>(null);
  const submitErrorRef = useRef<HTMLParagraphElement | null>(null);

  const [isCreating, setIsCreating] = useState(false);
  const [newTitleAr, setNewTitleAr] = useState("");
  const [newTitleEn, setNewTitleEn] = useState("");
  const [newDescAr, setNewDescAr] = useState("");
  const [newDescEn, setNewDescEn] = useState("");

  const [detailTitleAr, setDetailTitleAr] = useState("");
  const [detailTitleEn, setDetailTitleEn] = useState("");
  const [detailDescAr, setDetailDescAr] = useState("");
  const [detailDescEn, setDetailDescEn] = useState("");

  const [detailStudyYear, setDetailStudyYear] = useState("");

  const [secTitleAr, setSecTitleAr] = useState("");
  const [secTitleEn, setSecTitleEn] = useState("");
  const [lessonDrafts, setLessonDrafts] = useState<Record<string, { ar: string; en: string }>>({});

  const selectedCourse = useMemo(
    () => courses.find((course) => course.id === selectedCourseID) ?? null,
    [courses, selectedCourseID],
  );
  const revision = selectedCourse?.editable_revision ?? null;
  const sections = revision?.sections ?? [];

  const loadCourses = useCallback(
    async (preferCourseID?: string) => {
      const owned = await getOwnedCourses(locale);
      setCourses(owned as CourseWire[]);
      setSelectedCourseID((current) => {
        const target = preferCourseID ?? current;
        if (target && owned.some((course) => course.id === target)) return target;
        return owned[0]?.id ?? null;
      });
    },
    [locale],
  );

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    loadCourses()
      .catch((cause: unknown) => {
        if (!cancelled) setError(describeApiError(cause, locale));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [loadCourses, locale]);

  // The revision editor mirrors the selected server revision. It is reset from
  // the server on every selection change so an abandoned edit cannot leak into
  // the next Course.
  useEffect(() => {
    setDetailTitleAr(revision?.title_ar ?? "");
    setDetailTitleEn(revision?.title_en ?? "");
    setDetailDescAr(revision?.description_ar ?? "");
    setDetailDescEn(revision?.description_en ?? "");
    setDetailStudyYear(revision?.study_year ?? "");
  }, [
    revision?.id,
    revision?.title_ar,
    revision?.title_en,
    revision?.description_ar,
    revision?.description_en,
    revision?.study_year,
  ]);

  /** Re-reads one Course from the server and replaces it in the list. */
  const refreshSelectedCourse = useCallback(async () => {
    if (!selectedCourseID) return;
    const detail = (await getOwnedCourseDetail(selectedCourseID, locale)) as CourseWire;
    setCourses((current) => current.map((course) => (course.id === detail.id ? detail : course)));
  }, [locale, selectedCourseID]);

  const requireCSRF = (): string | null => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
      return null;
    }
    return csrf;
  };

  /** Runs one authoring command with a single busy flag, so a double click cannot issue it twice. */
  const command = async (
    action: (csrf: string) => Promise<void>,
    options?: { onFailure?: (message: string) => void },
  ) => {
    const csrf = requireCSRF();
    if (!csrf || busy) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await action(csrf);
    } catch (cause) {
      // The server's own reason, verbatim from `describeApiError`, including
      // every submission violation code. Nothing is suppressed or reworded.
      const message = describeApiError(cause, locale);
      setError(message);
      options?.onFailure?.(message);
    } finally {
      setBusy(false);
    }
  };

  const handleCreateCourse = (event: React.FormEvent) => {
    event.preventDefault();
    if (!newTitleAr || !newTitleEn) return;
    void command(async (csrf) => {
      const created = await createCourse({
        titleAr: newTitleAr,
        titleEn: newTitleEn,
        descriptionAr: newDescAr,
        descriptionEn: newDescEn,
        locale,
        csrf,
      });
      await loadCourses(created.id);
      setNewTitleAr("");
      setNewTitleEn("");
      setNewDescAr("");
      setNewDescEn("");
      setIsCreating(false);
      setNotice(isAr ? "تم إنشاء الدورة على الخادم." : "Course created on the server.");
    });
  };

  const handleSaveRevision = (event: React.FormEvent) => {
    event.preventDefault();
    if (!selectedCourse || !revision?.id) return;
    void command(async (csrf) => {
      await updateCourseRevision({
        courseID: selectedCourse.id,
        revisionID: revision.id!,
        titleAr: detailTitleAr,
        titleEn: detailTitleEn,
        descriptionAr: detailDescAr,
        descriptionEn: detailDescEn,
        majorTermID: revision.major_term_id,
        subjectTermID: revision.subject_term_id,
        studyYear: detailStudyYear,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
      setNotice(isAr ? "تم حفظ تفاصيل المراجعة." : "Revision details saved.");
    });
  };

  const handleAddSection = (event: React.FormEvent) => {
    event.preventDefault();
    if (!selectedCourse || !revision?.id || !secTitleAr || !secTitleEn) return;
    void command(async (csrf) => {
      await addSection({
        courseID: selectedCourse.id,
        revisionID: revision.id!,
        titleAr: secTitleAr,
        titleEn: secTitleEn,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
      setSecTitleAr("");
      setSecTitleEn("");
    });
  };

  const handleDeleteSection = (sectionID: string) => {
    if (!selectedCourse || !revision?.id) return;
    void command(async (csrf) => {
      await deleteSection({
        courseID: selectedCourse.id,
        revisionID: revision.id!,
        sectionID,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
    });
  };

  const handleAddLesson = (event: React.FormEvent, sectionID: string) => {
    event.preventDefault();
    if (!selectedCourse || !revision?.id) return;
    const draft = lessonDrafts[sectionID];
    if (!draft?.ar || !draft?.en) return;
    void command(async (csrf) => {
      await addLesson({
        courseID: selectedCourse.id,
        revisionID: revision.id!,
        sectionID,
        titleAr: draft.ar,
        titleEn: draft.en,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
      setLessonDrafts((current) => ({ ...current, [sectionID]: { ar: "", en: "" } }));
    });
  };

  const handleDeleteLesson = (lessonID: string) => {
    if (!selectedCourse || !revision?.id) return;
    void command(async (csrf) => {
      await deleteLesson({
        courseID: selectedCourse.id,
        revisionID: revision.id!,
        lessonID,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
    });
  };

  const handleSubmit = () => {
    if (!selectedCourse || !revision?.id) return;
    setSubmitError(null);
    void command(
      async (csrf) => {
        await submitCourseRevision({
          courseID: selectedCourse.id,
          revisionID: revision.id!,
          locale,
          csrf,
        });
        await loadCourses(selectedCourse.id);
        await refreshSelectedCourse();
        setNotice(isAr ? "تم إرسال الدورة إلى مراجعة الإدارة." : "Course submitted for Admin review.");
      },
      {
        onFailure: (message) => {
          setSubmitError(message);
          // The rejection is brought to the click, not left at the top of the
          // page: the region beside Submit is scrolled into view and focused,
          // so a keyboard or screen-reader user lands on it too.
          window.requestAnimationFrame(() => {
            submitErrorRef.current?.scrollIntoView({ block: "center" });
            submitErrorRef.current?.focus();
          });
        },
      },
    );
  };

  const courseTitle = (course: CourseWire) => {
    const rev = course.editable_revision ?? course.live_revision;
    if (!rev) return course.id;
    return isAr ? rev.title_ar : rev.title_en;
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
              ? "إنشاء وإدارة المحتوى التعليمي المحفوظ على الخادم قبل الاعتماد والنشر"
              : "Build and manage server-persisted course drafts before submission & review"}
          </p>
        </div>
        <button
          id="course-builder"
          onClick={() => setIsCreating(!isCreating)}
          data-testid="toggle-new-course"
          className="inline-flex items-center justify-center rounded-md bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700 transition"
        >
          {isCreating ? (isAr ? "إلغاء الإضافة" : "Cancel") : isAr ? "إنشاء دورة جديدة" : "New Course"}
        </button>
      </header>

      {error && (
        <p role="alert" data-testid="authoring-error" className="rounded border border-red-300 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" data-testid="authoring-notice" className="rounded border border-emerald-300 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300">
          {notice}
        </p>
      )}

      <ServerPricingPanel />

      <TaxonomyAssignmentPanel />

      {isCreating && (
        <form
          onSubmit={handleCreateCourse}
          data-testid="new-course-form"
          className="bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-lg p-6 space-y-4"
        >
          <h2 className="text-lg font-semibold">{isAr ? "تفاصيل الدورة الجديدة" : "New Course Details"}</h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <label className="block text-sm font-medium">
              {isAr ? "عنوان الدورة (بالعربية)" : "Course Title (Arabic)"}
              <input
                type="text"
                value={newTitleAr}
                onChange={(event) => setNewTitleAr(event.target.value)}
                required
                data-testid="new-course-title-ar"
                className="mt-1 w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </label>
            <label className="block text-sm font-medium">
              {isAr ? "عنوان الدورة (بالإنجليزية)" : "Course Title (English)"}
              <input
                type="text"
                value={newTitleEn}
                onChange={(event) => setNewTitleEn(event.target.value)}
                required
                data-testid="new-course-title-en"
                className="mt-1 w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </label>
            <label className="block text-sm font-medium">
              {isAr ? "الوصف (بالعربية)" : "Description (Arabic)"}
              <textarea
                value={newDescAr}
                onChange={(event) => setNewDescAr(event.target.value)}
                rows={3}
                data-testid="new-course-description-ar"
                className="mt-1 w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </label>
            <label className="block text-sm font-medium">
              {isAr ? "الوصف (بالإنجليزية)" : "Description (English)"}
              <textarea
                value={newDescEn}
                onChange={(event) => setNewDescEn(event.target.value)}
                rows={3}
                data-testid="new-course-description-en"
                className="mt-1 w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </label>
          </div>
          <button
            type="submit"
            disabled={busy}
            data-testid="create-course"
            className="rounded-md bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700 disabled:opacity-50"
          >
            {isAr ? "حفظ كمسودة" : "Save as Draft"}
          </button>
        </form>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="space-y-3">
          <h2 className="text-md font-semibold text-slate-700 dark:text-slate-300">
            {isAr ? "دوراتي على الخادم" : "My Courses"}
          </h2>
          <div className="space-y-2" data-testid="owned-course-list">
            {loading && (
              <p className="text-sm text-slate-500">{isAr ? "جارٍ التحميل..." : "Loading..."}</p>
            )}
            {!loading && courses.length === 0 && (
              <p className="text-sm text-slate-500 italic">
                {isAr ? "لا توجد دورات بعد." : "No courses yet."}
              </p>
            )}
            {courses.map((course) => (
              <button
                key={course.id}
                type="button"
                onClick={() => setSelectedCourseID(course.id)}
                data-testid={`owned-course-${course.id}`}
                className={`w-full text-start p-4 rounded-lg border transition ${
                  selectedCourseID === course.id
                    ? "border-blue-500 bg-blue-50/50 dark:bg-blue-950/20"
                    : "border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900"
                }`}
              >
                <div className="flex items-center justify-between gap-2">
                  <h3 className="font-semibold text-sm">{courseTitle(course)}</h3>
                  <span className="text-xs px-2 py-0.5 rounded font-mono bg-amber-100 text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">
                    {course.editable_revision?.state ?? course.lifecycle ?? ""}
                  </span>
                </div>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-1 line-clamp-2">
                  {isAr
                    ? course.editable_revision?.description_ar
                    : course.editable_revision?.description_en}
                </p>
              </button>
            ))}
          </div>
        </div>

        {selectedCourse ? (
          <div className="md:col-span-2 space-y-6 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-lg p-6">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-4">
              <div>
                <span
                  data-testid="selected-course-id"
                  className="text-xs font-mono text-blue-600 dark:text-blue-400 font-semibold"
                >
                  ID: {selectedCourse.id}
                </span>
                <h2 className="text-xl font-bold mt-0.5">{courseTitle(selectedCourse)}</h2>
                {revision?.id && (
                  <span data-testid="selected-revision-id" className="text-[11px] font-mono text-slate-500">
                    revision_id: {revision.id}
                  </span>
                )}
              </div>
              <span
                data-testid="revision-state"
                className="text-xs px-3 py-1 rounded-full bg-slate-100 dark:bg-slate-800 font-medium"
              >
                {revision?.state ?? selectedCourse.lifecycle ?? ""}
              </span>
            </div>

            {revision?.id ? (
              <>
                <form onSubmit={handleSaveRevision} className="space-y-3" data-testid="revision-form">
                  <h3 className="text-md font-semibold">{isAr ? "تفاصيل الدورة" : "Course details"}</h3>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <input
                      type="text"
                      value={detailTitleAr}
                      onChange={(event) => setDetailTitleAr(event.target.value)}
                      aria-label={isAr ? "عنوان الدورة (بالعربية)" : "Course Title (Arabic)"}
                      data-testid="revision-title-ar"
                      className="rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
                    />
                    <input
                      type="text"
                      value={detailTitleEn}
                      onChange={(event) => setDetailTitleEn(event.target.value)}
                      aria-label={isAr ? "عنوان الدورة (بالإنجليزية)" : "Course Title (English)"}
                      data-testid="revision-title-en"
                      className="rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
                    />
                    <textarea
                      value={detailDescAr}
                      onChange={(event) => setDetailDescAr(event.target.value)}
                      rows={2}
                      aria-label={isAr ? "الوصف (بالعربية)" : "Description (Arabic)"}
                      data-testid="revision-description-ar"
                      className="rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
                    />
                    <textarea
                      value={detailDescEn}
                      onChange={(event) => setDetailDescEn(event.target.value)}
                      rows={2}
                      aria-label={isAr ? "الوصف (بالإنجليزية)" : "Description (English)"}
                      data-testid="revision-description-en"
                      className="rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
                    />
                  </div>
                  <label className="block text-xs font-semibold text-slate-700 dark:text-slate-300">
                    {isAr ? "السنة الدراسية" : "Study year"}
                    <select
                      value={detailStudyYear}
                      onChange={(event) => setDetailStudyYear(event.target.value)}
                      data-testid="revision-study-year"
                      className="mt-1 block rounded-md border border-slate-300 bg-white p-2 text-xs dark:border-slate-700 dark:bg-slate-800"
                    >
                      <option value="">{isAr ? "غير محددة" : "Not set"}</option>
                      {["PREP", "YEAR_1", "YEAR_2", "YEAR_3", "YEAR_4"].map((year) => (
                        <option key={year} value={year}>
                          {year}
                        </option>
                      ))}
                    </select>
                  </label>
                  <button
                    type="submit"
                    disabled={busy}
                    data-testid="save-revision"
                    className="rounded-md bg-blue-600 px-3 py-2 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50"
                  >
                    {isAr ? "حفظ التفاصيل" : "Save details"}
                  </button>
                </form>

                <div className="space-y-4">
                  <h3 className="text-md font-semibold">
                    {isAr ? "أقسام الدورة والدروس" : "Sections & Lessons"}
                  </h3>

                  {sections.length === 0 ? (
                    <p className="text-sm text-slate-500 italic">
                      {isAr ? "لا يوجد أقسام مضافة بعد." : "No sections added yet."}
                    </p>
                  ) : (
                    <div className="space-y-4">
                      {sections.map((section, index) => (
                        <div
                          key={section.id}
                          data-testid={`section-${section.id}`}
                          className="border border-slate-200 dark:border-slate-800 rounded-md p-4 bg-slate-50/50 dark:bg-slate-950/30"
                        >
                          <div className="flex items-center justify-between gap-2 mb-3">
                            <span className="font-semibold text-sm">
                              {index + 1}. {isAr ? section.title_ar : section.title_en}
                            </span>
                            <button
                              type="button"
                              disabled={busy}
                              onClick={() => handleDeleteSection(section.id)}
                              data-testid={`delete-section-${section.id}`}
                              className="text-[11px] text-red-700 underline disabled:opacity-50 dark:text-red-400"
                            >
                              {isAr ? "حذف القسم" : "Delete section"}
                            </button>
                          </div>

                          <div className="space-y-2 ps-4 border-s-2 border-slate-300 dark:border-slate-700">
                            {(section.lessons ?? []).map((lesson, lessonIndex) => (
                              <div
                                key={lesson.id}
                                data-testid={`lesson-${lesson.id}`}
                                className="bg-white dark:bg-slate-900 p-3 rounded border text-xs flex flex-col gap-1"
                              >
                                <div className="flex flex-wrap items-center justify-between gap-2 font-medium">
                                  <span>
                                    {lessonIndex + 1}. {isAr ? lesson.title_ar : lesson.title_en}
                                  </span>
                                  {lesson.video_asset_version_id ? (
                                    <span
                                      data-testid={`lesson-video-ref-${lesson.id}`}
                                      className="text-emerald-600 dark:text-emerald-400 font-mono text-[10px]"
                                    >
                                      {isAr ? "فيديو مرفق" : "Video attached"}: {lesson.video_asset_version_id}
                                    </span>
                                  ) : (
                                    <span data-testid={`lesson-video-none-${lesson.id}`} className="text-slate-400">
                                      {isAr ? "لا يوجد فيديو" : "No video"}
                                    </span>
                                  )}
                                  <button
                                    type="button"
                                    disabled={busy}
                                    onClick={() => handleDeleteLesson(lesson.id)}
                                    data-testid={`delete-lesson-${lesson.id}`}
                                    className="text-[11px] text-red-700 underline disabled:opacity-50 dark:text-red-400"
                                  >
                                    {isAr ? "حذف الدرس" : "Delete lesson"}
                                  </button>
                                </div>
                                {(lesson.files ?? []).length > 0 && (
                                  <div className="text-[11px] text-slate-500 mt-1">
                                    {(lesson.files ?? []).map((file) => (
                                      <span
                                        key={file.id}
                                        className="me-2 inline-block bg-slate-100 dark:bg-slate-800 px-1.5 py-0.5 rounded"
                                      >
                                        [{file.kind}] {isAr ? file.display_name_ar : file.display_name_en}
                                      </span>
                                    ))}
                                  </div>
                                )}
                                <LessonVideoUpload
                                  courseID={selectedCourse.id}
                                  revisionID={revision.id!}
                                  lessonID={lesson.id}
                                  locale={locale}
                                  onAttached={refreshSelectedCourse}
                                />
                              </div>
                            ))}

                            <form
                              onSubmit={(event) => handleAddLesson(event, section.id)}
                              data-testid={`add-lesson-form-${section.id}`}
                              className="flex flex-col gap-2 pt-2 lg:flex-row"
                            >
                              <input
                                type="text"
                                placeholder={isAr ? "عنوان الدرس بالعربية" : "Lesson Title (Arabic)"}
                                value={lessonDrafts[section.id]?.ar ?? ""}
                                onChange={(event) =>
                                  setLessonDrafts((current) => ({
                                    ...current,
                                    [section.id]: { ar: event.target.value, en: current[section.id]?.en ?? "" },
                                  }))
                                }
                                data-testid={`lesson-title-ar-${section.id}`}
                                className="flex-1 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-xs"
                              />
                              <input
                                type="text"
                                placeholder={isAr ? "عنوان الدرس بالإنجليزية" : "Lesson Title (English)"}
                                value={lessonDrafts[section.id]?.en ?? ""}
                                onChange={(event) =>
                                  setLessonDrafts((current) => ({
                                    ...current,
                                    [section.id]: { ar: current[section.id]?.ar ?? "", en: event.target.value },
                                  }))
                                }
                                data-testid={`lesson-title-en-${section.id}`}
                                className="flex-1 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-xs"
                              />
                              <button
                                type="submit"
                                disabled={busy}
                                data-testid={`add-lesson-${section.id}`}
                                className="rounded-md bg-slate-700 px-3 py-2 text-xs font-medium text-white hover:bg-slate-800 disabled:opacity-50"
                              >
                                {isAr ? "إضافة درس" : "Add Lesson"}
                              </button>
                            </form>
                          </div>
                        </div>
                      ))}
                    </div>
                  )}

                  <form onSubmit={handleAddSection} data-testid="add-section-form" className="flex flex-col gap-2 pt-2 lg:flex-row">
                    <input
                      type="text"
                      placeholder={isAr ? "عنوان القسم بالعربية" : "Section Title (Arabic)"}
                      value={secTitleAr}
                      onChange={(event) => setSecTitleAr(event.target.value)}
                      data-testid="section-title-ar"
                      className="flex-1 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-xs"
                    />
                    <input
                      type="text"
                      placeholder={isAr ? "عنوان القسم بالإنجليزية" : "Section Title (English)"}
                      value={secTitleEn}
                      onChange={(event) => setSecTitleEn(event.target.value)}
                      data-testid="section-title-en"
                      className="flex-1 rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-xs"
                    />
                    <button
                      type="submit"
                      disabled={busy}
                      data-testid="add-section"
                      className="rounded-md bg-blue-600 px-3 py-2 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50"
                    >
                      {isAr ? "إضافة قسم" : "Add Section"}
                    </button>
                  </form>
                </div>

                <div className="border-t pt-4">
                  <button
                    type="button"
                    disabled={busy}
                    onClick={handleSubmit}
                    data-testid="submit-for-review"
                    className="rounded-md bg-emerald-700 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-800 disabled:opacity-50"
                  >
                    {isAr ? "إرسال للمراجعة" : "Submit for Review"}
                  </button>
                  {submitError && (
                    <p
                      ref={submitErrorRef}
                      role="alert"
                      tabIndex={-1}
                      data-testid="submit-error"
                      className="mt-3 rounded border border-red-300 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300"
                    >
                      {submitError}
                    </p>
                  )}
                  <p className="mt-2 text-xs text-slate-500">
                    {isAr
                      ? "يتحقق الخادم من اكتمال الدورة، ويعرض سبب الرفض كما هو."
                      : "The server validates completeness; its rejection reason is shown as-is."}
                  </p>
                </div>
              </>
            ) : (
              <p className="text-sm text-slate-500 italic" data-testid="no-editable-revision">
                {isAr
                  ? "لا توجد مراجعة قابلة للتحرير لهذه الدورة."
                  : "This Course has no editable revision."}
              </p>
            )}
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
