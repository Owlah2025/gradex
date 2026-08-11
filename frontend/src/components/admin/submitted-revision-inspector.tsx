"use client";

import { useCallback, useEffect, useState } from "react";
import { getTaxonomyTerms, type CourseRevisionWire, type TaxonomyKind, type TaxonomyTerm } from "@/lib/api/catalog";
import { describeApiError } from "@/lib/api/api-error";
import { getMediaAssetStatus } from "@/lib/api/media-upload";
import {
  approveCourseRevision,
  getReviewCourseRevision,
  previewAdminLesson,
  requestCourseRevisionChanges,
  type AdminLessonPreview,
  type ReviewQueueItem,
  type ReviewedCourse,
} from "@/lib/api/review";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ReviewLessonPreview } from "./review-lesson-preview";

type SubmittedRevisionInspectorProps = {
  item: ReviewQueueItem;
  onClose: () => void;
  onReviewed: () => Promise<void>;
};

type LoadedRevision = {
  course: ReviewedCourse;
  revision: CourseRevisionWire;
  terms: TaxonomyTerm[];
};

function videoIDs(revision: CourseRevisionWire): string[] {
  return revision.sections.flatMap((section) =>
    (section.lessons ?? []).flatMap((lesson) => (lesson.video_asset_version_id ? [lesson.video_asset_version_id] : [])),
  );
}

function taxonomyLabel(
  termID: string | undefined,
  kind: TaxonomyKind,
  terms: TaxonomyTerm[],
  locale: "ar" | "en",
): string {
  if (!termID) return locale === "ar" ? "غير محدد" : "Not specified";
  const term = terms.find((candidate) => candidate.id === termID && candidate.kind === kind);
  if (!term) return termID;
  return `${locale === "ar" ? term.label_ar : term.label_en} (${termID})`;
}

function mediaStateLabel(state: string, locale: "ar" | "en"): string {
  const labels: Record<string, [string, string]> = {
    LOADING: ["جارٍ قراءة حالة الوسائط", "Loading media state"],
    READY: ["جاهز للمعاينة", "READY"],
    PROCESSING: ["قيد المعالجة", "PROCESSING"],
    SCAN_PASSED: ["بانتظار المعالجة", "Awaiting processing"],
    FAILED: ["فشلت المعالجة", "FAILED"],
    QUARANTINED: ["غير متاح بعد الفحص", "Unavailable after scanning"],
    UNAVAILABLE: ["حالة الوسائط غير متاحة", "Media state unavailable"],
    NO_VIDEO: ["لا يوجد فيديو مرفق", "No video attached"],
  };
  const value = labels[state] ?? [state, state];
  return locale === "ar" ? value[0] : value[1];
}

/**
 * Reads and renders the graph stored in the submitted revision. It deliberately
 * never reads the Instructor's current draft; actions remain unavailable until
 * the fetched Course and revision prove they match the queue row that opened it.
 */
export function SubmittedRevisionInspector({ item, onClose, onReviewed }: SubmittedRevisionInspectorProps) {
  const { locale, dir } = useLocale();
  const isAr = locale === "ar";
  const [loaded, setLoaded] = useState<LoadedRevision | null>(null);
  const [loadError, setLoadError] = useState("");
  const [loading, setLoading] = useState(true);
  const [mediaStates, setMediaStates] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<AdminLessonPreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [actionSuccess, setActionSuccess] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const [requestReason, setRequestReason] = useState("");
  const [requestingChanges, setRequestingChanges] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    setLoaded(null);
    setMediaStates({});
    setPreview(null);
    setPreviewError("");
    setReviewed(false);
    try {
      const [course, terms] = await Promise.all([
        getReviewCourseRevision(item.course_id, item.revision_id, locale),
        getTaxonomyTerms(locale),
      ]);
      const revision = course.editable_revision;
      if (
        course.id !== item.course_id ||
        !revision ||
        revision.id !== item.revision_id ||
        revision.course_id !== item.course_id
      ) {
        throw new Error(isAr ? "لم تطابق تفاصيل المراجعة الدورة أو المراجعة المحددة." : "Review detail did not match the selected Course or revision.");
      }
      setLoaded({ course, revision, terms });
      const assetVersionIDs = videoIDs(revision);
      if (assetVersionIDs.length > 0) {
        const states = await Promise.all(
          assetVersionIDs.map(async (assetVersionID) => {
            try {
              const status = await getMediaAssetStatus(assetVersionID, locale);
              return [assetVersionID, status.state] as const;
            } catch {
              return [assetVersionID, "UNAVAILABLE"] as const;
            }
          }),
        );
        setMediaStates(Object.fromEntries(states));
      }
    } catch (cause) {
      setLoadError(describeApiError(cause, locale));
    } finally {
      setLoading(false);
    }
  }, [isAr, item.course_id, item.revision_id, locale]);

  useEffect(() => {
    void load();
  }, [load]);

  const canReview = loaded !== null && !loading && !loadError;
  const canAct = canReview && !reviewed;
  const revision = loaded?.revision;
  const terms = loaded?.terms ?? [];

  const csrf = (): string | null => {
    const token = currentCSRFToken();
    if (!token) setActionError(isAr ? "رمز CSRF للجلسة مفقود" : "Session CSRF token is missing");
    return token || null;
  };

  const completeReview = async (operation: (token: string) => Promise<void>, success: string) => {
    if (!canAct || busy) return;
    const token = csrf();
    if (!token) return;
    setBusy(true);
    setActionError("");
    setActionSuccess("");
    try {
      await operation(token);
      setReviewed(true);
      await onReviewed();
      setActionSuccess(success);
    } catch (cause) {
      setActionError(describeApiError(cause, locale));
    } finally {
      setBusy(false);
    }
  };

  const startPreview = async (lessonID: string, assetVersionID: string | undefined) => {
    if (!canReview || !assetVersionID || mediaStates[assetVersionID] !== "READY") return;
    const token = csrf();
    if (!token) return;
    setPreviewError("");
    setPreview(null);
    try {
      const issued = await previewAdminLesson({
        courseID: item.course_id,
        revisionID: item.revision_id,
        lessonID,
        locale,
        csrf: token,
      });
      if (
        issued.course_id !== item.course_id ||
        issued.revision_id !== item.revision_id ||
        issued.lesson_id !== lessonID ||
        issued.video_asset_version_id !== assetVersionID
      ) {
        throw new Error(isAr ? "لم تطابق معاينة الفيديو الدرس المُرسل." : "Video preview did not match the submitted Lesson.");
      }
      setPreview(issued);
    } catch (cause) {
      setPreviewError(describeApiError(cause, locale));
    }
  };

  return (
    <section dir={dir} data-testid="submitted-revision-inspector" className="space-y-5 rounded-xl border border-indigo-200 bg-indigo-50/40 p-5 dark:border-indigo-900 dark:bg-indigo-950/20">
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-indigo-200 pb-4 dark:border-indigo-900">
        <div>
          <h2 className="text-lg font-bold text-slate-900 dark:text-slate-100">
            {isAr ? "فحص المراجعة المُرسلة" : "Submitted Revision Inspector"}
          </h2>
          <p className="mt-1 text-xs text-slate-600 dark:text-slate-300">
            {isAr ? "المحتوى أدناه هو المراجعة المُرسلة فقط." : "The content below is the submitted revision only."}
          </p>
          <p className="mt-2 font-mono text-xs text-slate-600 dark:text-slate-300" data-testid="submitted-course-id">{item.course_id}</p>
          <p className="font-mono text-xs text-slate-600 dark:text-slate-300" data-testid="submitted-revision-id">{item.revision_id}</p>
        </div>
        <button type="button" onClick={onClose} className="rounded border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 dark:border-slate-700 dark:text-slate-300">
          {isAr ? "إغلاق الفحص" : "Close inspector"}
        </button>
      </header>

      {loading && <p data-testid="submitted-revision-loading" aria-live="polite">{isAr ? "جارٍ تحميل المراجعة المُرسلة..." : "Loading submitted revision..."}</p>}
      {loadError && <p role="alert" data-testid="submitted-revision-error" className="rounded border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">{loadError}</p>}

      {revision && canReview && (
        <div className="space-y-6">
          <section className="grid gap-4 rounded-lg bg-white p-4 shadow-sm dark:bg-slate-900 md:grid-cols-2">
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "العنوان العربي" : "Arabic title"}</p><p dir="rtl" data-testid="submitted-title-ar" className="mt-1 text-slate-900 dark:text-slate-100">{revision.title_ar}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "العنوان الإنجليزي" : "English title"}</p><p dir="ltr" data-testid="submitted-title-en" className="mt-1 text-slate-900 dark:text-slate-100">{revision.title_en}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "الوصف العربي" : "Arabic description"}</p><p dir="rtl" data-testid="submitted-description-ar" className="mt-1 whitespace-pre-wrap text-slate-900 dark:text-slate-100">{revision.description_ar || "—"}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "الوصف الإنجليزي" : "English description"}</p><p dir="ltr" data-testid="submitted-description-en" className="mt-1 whitespace-pre-wrap text-slate-900 dark:text-slate-100">{revision.description_en || "—"}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "سنة الدراسة" : "Study year"}</p><p data-testid="submitted-study-year" className="mt-1 text-slate-900 dark:text-slate-100">{revision.study_year || "—"}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "حالة المراجعة" : "Revision state"}</p><p data-testid="submitted-revision-state" className="mt-1 text-slate-900 dark:text-slate-100">{revision.state || "—"}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "التخصص" : "Major"}</p><p data-testid="submitted-major" className="mt-1 text-slate-900 dark:text-slate-100">{taxonomyLabel(revision.major_term_id, "MAJOR", terms, locale)}</p></div>
            <div><p className="text-xs font-semibold text-slate-500">{isAr ? "المادة" : "Subject"}</p><p data-testid="submitted-subject" className="mt-1 text-slate-900 dark:text-slate-100">{taxonomyLabel(revision.subject_term_id, "SUBJECT", terms, locale)}</p></div>
          </section>

          <section aria-labelledby="submitted-outline-heading" className="space-y-3">
            <h3 id="submitted-outline-heading" className="font-semibold text-slate-900 dark:text-slate-100">{isAr ? "الأقسام والدروس المُرسلة" : "Submitted Sections and Lessons"}</h3>
            {revision.sections.length === 0 ? (
              <p data-testid="submitted-sections-empty" className="text-sm text-slate-600 dark:text-slate-300">{isAr ? "لا توجد أقسام مُرسلة." : "No submitted Sections."}</p>
            ) : revision.sections.map((section) => (
              <article key={section.id} data-testid={`submitted-section-${section.id}`} className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
                <p className="text-xs font-semibold text-indigo-700 dark:text-indigo-300">{isAr ? `القسم ${section.position}` : `Section ${section.position}`}</p>
                <p dir="rtl" className="mt-1 font-medium text-slate-900 dark:text-slate-100">{section.title_ar}</p>
                <p dir="ltr" className="text-sm text-slate-700 dark:text-slate-300">{section.title_en}</p>
                <div className="mt-3 space-y-3">
                  {(section.lessons ?? []).map((lesson) => {
                    const mediaState = lesson.video_asset_version_id ? (mediaStates[lesson.video_asset_version_id] ?? "LOADING") : "NO_VIDEO";
                    const canPreview = mediaState === "READY";
                    return (
                      <div key={lesson.id} data-testid={`submitted-lesson-${lesson.id}`} className="rounded border border-slate-200 p-3 dark:border-slate-700">
                        <p className="text-xs font-semibold text-slate-500">{isAr ? `الدرس ${lesson.position}` : `Lesson ${lesson.position}`}</p>
                        <p dir="rtl" className="mt-1 text-slate-900 dark:text-slate-100">{lesson.title_ar}</p>
                        <p dir="ltr" className="text-sm text-slate-700 dark:text-slate-300">{lesson.title_en}</p>
                        <p data-testid={`submitted-lesson-media-state-${lesson.id}`} className="mt-2 text-xs text-slate-600 dark:text-slate-300">{isAr ? "حالة الوسائط: " : "Media state: "}{mediaStateLabel(mediaState, locale)}</p>
                        {lesson.video_asset_version_id && <p className="font-mono text-xs text-slate-500">{lesson.video_asset_version_id}</p>}
                        {lesson.files && lesson.files.length > 0 && (
                          <ul data-testid={`submitted-lesson-materials-${lesson.id}`} className="mt-2 list-inside list-disc text-xs text-slate-600 dark:text-slate-300">
                            {lesson.files.map((file) => <li key={file.id}>{file.kind}: {isAr ? file.display_name_ar : file.display_name_en}</li>)}
                          </ul>
                        )}
                        <button
                          type="button"
                          disabled={!canPreview || busy || reviewed}
                          onClick={() => void startPreview(lesson.id, lesson.video_asset_version_id)}
                          data-testid={`preview-submitted-lesson-${lesson.id}`}
                          className="mt-3 rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white disabled:cursor-not-allowed disabled:opacity-50"
                        >
                          {canPreview ? (isAr ? "معاينة الفيديو المحمي" : "Preview protected video") : mediaStateLabel(mediaState, locale)}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </article>
            ))}
          </section>

          {previewError && <p role="alert" data-testid="review-preview-error" className="rounded border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">{previewError}</p>}
          {preview && <section data-testid="review-preview-player" className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900"><h3 className="mb-3 font-semibold text-slate-900 dark:text-slate-100">{isAr ? "معاينة الدرس المُرسل" : "Submitted Lesson Preview"}</h3><ReviewLessonPreview playbackURL={preview.playback_url} locale={locale} /></section>}

          {actionSuccess && <p role="status" data-testid="review-action-success" className="rounded border border-emerald-300 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300">{actionSuccess}</p>}
          {actionError && <p role="alert" data-testid="review-action-error" className="rounded border border-rose-300 bg-rose-50 p-3 text-sm text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-300">{actionError}</p>}

          <section className="flex flex-wrap gap-3 border-t border-indigo-200 pt-4 dark:border-indigo-900">
            <button type="button" disabled={busy || reviewed} onClick={() => void completeReview((token) => approveCourseRevision({ courseID: item.course_id, revisionID: item.revision_id, locale, csrf: token }).then(() => undefined), isAr ? "تم نشر الدورة بنجاح" : "Course published successfully")} data-testid="approve-inspected-revision" className="rounded bg-emerald-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50">{isAr ? "موافقة ونشر" : "Approve & Publish"}</button>
            <button type="button" disabled={busy || reviewed} onClick={() => setRequestingChanges(true)} data-testid="request-changes-inspected-revision" className="rounded bg-rose-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50">{isAr ? "طلب تعديلات" : "Request Changes"}</button>
          </section>

          {requestingChanges && (
            <div role="dialog" aria-modal="true" aria-labelledby="request-changes-title" className="rounded-lg border border-rose-300 bg-white p-4 shadow-sm dark:border-rose-900 dark:bg-slate-900">
              <h3 id="request-changes-title" className="font-semibold text-slate-900 dark:text-slate-100">{isAr ? "سبب طلب التعديلات" : "Reason for change request"}</h3>
              <textarea value={requestReason} onChange={(event) => setRequestReason(event.target.value)} rows={4} data-testid="request-changes-reason" className="mt-3 w-full rounded border border-slate-300 p-2 text-sm dark:border-slate-700 dark:bg-slate-800" placeholder={isAr ? "اكتب الملاحظات للمحاضر" : "Provide clear feedback for the Instructor"} />
              <div className="mt-3 flex justify-end gap-2"><button type="button" onClick={() => setRequestingChanges(false)} className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700">{isAr ? "إلغاء" : "Cancel"}</button><button type="button" disabled={busy || !requestReason.trim()} onClick={() => void completeReview((token) => requestCourseRevisionChanges({ courseID: item.course_id, revisionID: item.revision_id, reason: requestReason, locale, csrf: token }).then(() => { setRequestingChanges(false); setRequestReason(""); }), isAr ? "تم إرسال طلب التعديلات إلى المحاضر" : "Change request sent to instructor")} data-testid="submit-request-changes" className="rounded bg-rose-600 px-3 py-1.5 text-sm font-semibold text-white disabled:opacity-50">{isAr ? "إرسال الطلب" : "Send request"}</button></div>
            </div>
          )}
        </div>
      )}
      {!loading && !revision && !loadError && <p role="alert">{isAr ? "لا تتوفر مراجعة مُرسلة صالحة." : "No valid submitted revision is available."}</p>}
    </section>
  );
}
