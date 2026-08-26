"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { ServerPricingPanel } from "./server-pricing-panel";
import { TaxonomyAssignmentPanel } from "./taxonomy-assignment-panel";
import { AcademicSubjectPicker, type AcademicSubjectSelection } from "./academic-subject-picker";
import { AcademicCourseContextPanel } from "./academic-course-context";
import { LessonVideoUpload } from "./lesson-video-upload";
import { PublicPreviewUpload } from "./public-preview-upload";
import { LessonResourceUpload } from "./lesson-resource-upload";
import { ChangeRequestNotice } from "./change-request-notice";
import { editsPublishedCourse, revisionWorkflow } from "./revision-workflow";
import { EditingPublishedNotice, RevisionWorkflowPanel } from "./revision-workflow-panel";
import {
  addLesson,
  addSection,
  createCandidateRevision,
  createCourse,
  deleteLesson,
  deleteSection,
  getOwnedCourseDetail,
  getOwnedCourses,
  setCourseSubject,
  setRevisionAudience,
  resetRevisionAudience,
  submitCourseRevision,
  updateCourseRevision,
  type CourseWire,
} from "@/lib/api/authoring";
import { isAcademicCourse } from "@/lib/api/catalog";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
import { createSubjectRequest } from "@/lib/api/subject-requests";
import { CourseRoster } from "./course-roster";
import { InstructorCourseList } from "./instructor-course-list";
import { courseDisplayTitle } from "./course-standing";
import { ErrorState } from "@/components/common/error-state";
import {
  WorkspacePage,
  WorkspacePageHeader,
} from "@/components/layout/workspace-page";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

/**
 * Instructor Course Authoring Studio.
 *
 * Every Course, Section, and Lesson rendered here comes from the Go API and is
 * written back through it. The component holds no authored content of its own:
 * after each successful command it re-reads the owned-Course graph, so what the
 * Instructor sees is what a page reload would show.
 */
export function CourseBuilder() {
  const { locale, t } = useLocale();
  const isAr = locale === "ar";

  /**
   * Wire enum → the Instructor's language. The raw enum stays on `data-revision-state` for tests
   * and support, but it is never what the Instructor is asked to interpret.
   */
  const stateLabel = (state: string | undefined, lifecycle: string | undefined): string => {
    const wire = state ?? lifecycle;
    if (!wire) return "";
    const labels = t.instructor.revisionState as Record<string, string | undefined>;
    return labels[wire] ?? wire;
  };

  const [courses, setCourses] = useState<CourseWire[]>([]);
  const [selectedCourseID, setSelectedCourseID] = useState<string | null>(null);
  const [showRoster, setShowRoster] = useState(false);
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
  // The academic identity of the Course being created. A Course cannot be
  // created without it (D-093 §1), so Create stays disabled until it is present.
  const [newAcademic, setNewAcademic] = useState<AcademicSubjectSelection | null>(null);
  const [newInstitutionID, setNewInstitutionID] = useState("");
  const [requestingMissingSubject, setRequestingMissingSubject] = useState(false);
  const [requestedCode, setRequestedCode] = useState("");
  const [requestedTitleAr, setRequestedTitleAr] = useState("");
  const [requestedTitleEn, setRequestedTitleEn] = useState("");
  const [requestedNote, setRequestedNote] = useState("");

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
  const workflow = revisionWorkflow(selectedCourse);
  const editingPublished = editsPublishedCourse(selectedCourse);

  const selectCourse = (courseID: string) => {
    setSelectedCourseID(courseID);
    setShowRoster(false);
  };

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

  /**
   * Begins a new revision of a published Course.
   *
   * The server is idempotent here — it returns any existing active candidate rather than cloning a
   * second one — so a double click or a reload cannot fork the Course. On failure `command` surfaces
   * the server's own reason and nothing local is mutated, so the Instructor is never left believing
   * a revision exists when it does not.
   */
  const handleStartRevision = () => {
    if (!selectedCourse) return;
    void command(async (csrf) => {
      await createCandidateRevision({ courseID: selectedCourse.id, locale, csrf });
      await loadCourses(selectedCourse.id);
      setNotice(t.instructor.revision.editingPublishedTitle);
    });
  };

  const handleCreateCourse = (event: React.FormEvent) => {
    event.preventDefault();
    if (!newTitleAr || !newTitleEn) return;
    // A Course is created on the Academic Catalog model or not at all. There is
    // no legacy creation path in the ordinary UI after T4-B.
    const academic = newAcademic;
    const validRequest = requestingMissingSubject && newInstitutionID && requestedTitleAr && requestedTitleEn;
    if (!academic && !validRequest) return;
    void command(async (csrf) => {
      const created = await createCourse({
        titleAr: newTitleAr,
        titleEn: newTitleEn,
        descriptionAr: newDescAr,
        descriptionEn: newDescEn,
        institutionID: academic?.institutionID ?? newInstitutionID,
        subjectID: academic?.subject.id,
        locale,
        csrf,
      });
      if (!academic) {
        await createSubjectRequest({
          institutionID: newInstitutionID,
          courseID: created.id,
          proposedOfficialCode: requestedCode,
          proposedTitleAr: requestedTitleAr,
          proposedTitleEn: requestedTitleEn,
          note: requestedNote,
          locale,
          csrf,
        });
      }
      await loadCourses(created.id);
      setNewTitleAr("");
      setNewTitleEn("");
      setNewDescAr("");
      setNewDescEn("");
      setNewAcademic(null);
      setNewInstitutionID("");
      setRequestingMissingSubject(false);
      setRequestedCode("");
      setRequestedTitleAr("");
      setRequestedTitleEn("");
      setRequestedNote("");
      setIsCreating(false);
      setNotice(
        academic
          ? (isAr ? "تم إنشاء الكورس على الخادم." : "Course created on the server.")
          : (isAr ? "تم إنشاء مسودة الكورس وإرسال طلب المادة للمراجعة." : "Course draft created and Subject request sent for review."),
      );
    });
  };

  /**
   * Corrects the canonical Subject of an Academic Course that has never been
   * published. The server owns every lifecycle rule; a refusal is reported the
   * same way any other command failure is.
   */
  const handleChangeSubject = (subjectID: string) => {
    if (!selectedCourse) return;
    void command(async (csrf) => {
      await setCourseSubject({ courseID: selectedCourse.id, subjectID, locale, csrf });
      await refreshSelectedCourse();
      setNotice(isAr ? "تم تحديث مادة الكورس." : "Course Subject updated.");
    });
  };

  const handleRequestSubject = async (input: {
    proposedOfficialCode?: string;
    proposedTitleAr: string;
    proposedTitleEn: string;
    note?: string;
  }): Promise<boolean> => {
    if (!selectedCourse?.institution_id || busy) return false;
    const csrf = requireCSRF();
    if (!csrf) return false;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await createSubjectRequest({
        institutionID: selectedCourse.institution_id,
        courseID: selectedCourse.id,
        ...input,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
      setNotice(isAr ? "تم إرسال طلب المادة للمراجعة." : "Subject request sent for review.");
      return true;
    } catch (cause) {
      setError(describeApiError(cause, locale));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const handleCustomizeAudience = (programIDs: string[]) => {
    if (!selectedCourse || !revision?.id) return;
    void command(async (csrf) => {
      await setRevisionAudience({
        courseID: selectedCourse.id,
        revisionID: revision.id!,
        programIDs,
        locale,
        csrf,
      });
      await refreshSelectedCourse();
      setNotice(isAr ? "تم تخصيص جمهور الكورس." : "Course audience customized.");
    });
  };

  const handleResetAudience = () => {
    if (!selectedCourse || !revision?.id) return;
    void command(async (csrf) => {
      await resetRevisionAudience({ courseID: selectedCourse.id, revisionID: revision.id!, locale, csrf });
      await refreshSelectedCourse();
      setNotice(isAr ? "تمت استعادة الجمهور التلقائي." : "Automatic audience restored.");
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
        // Omitted entirely for an Academic Course: its identity is the
        // Course-level Subject, and the server refuses the legacy vocabulary.
        majorTermID: isAcademicCourse(selectedCourse) ? undefined : revision.major_term_id,
        subjectTermID: isAcademicCourse(selectedCourse) ? undefined : revision.subject_term_id,
        studyYear: isAcademicCourse(selectedCourse) ? undefined : detailStudyYear,
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

  /**
   * A Course's name, never its database key. The previous fallback printed `course.id` when no
   * revision was expanded, which put a bare UUID in the heading of the Instructor's own studio.
   */
  const courseTitle = (course: CourseWire) =>
    courseDisplayTitle(course, locale, t.instructor.courses.untitled);

  const instructor = t.instructor;
  const studio = instructor.studio;

  return (
    <WorkspacePage className="space-y-8">
      <WorkspacePageHeader
        title={studio.title}
        description={studio.intro}
        actions={
          <Button
            type="button"
            id="course-builder"
            variant={isCreating ? "outline" : "default"}
            aria-expanded={isCreating}
            aria-controls="new-course-form"
            onClick={() => setIsCreating(!isCreating)}
            data-testid="toggle-new-course"
          >
            {isCreating ? studio.cancelNewCourse : studio.newCourse}
          </Button>
        }
      />

      {error && (
        <ErrorState testID="authoring-error" title={studio.actionFailed} detail={error} />
      )}
      {notice && (
        <div data-testid="authoring-notice">
          <Alert tone="success" title={notice} />
        </div>
      )}

      <ServerPricingPanel />

      {/*
        Legacy taxonomy compatibility (D-093 §6).

        This panel edits major_term_id / subject_term_id / study_year, which only
        a LEGACY_TAXONOMY Course carries. It stays mounted until T5 migrates the
        existing Courses that still depend on it, and it disappears entirely once
        an Instructor owns no legacy Course. The server refuses these fields on an
        Academic Course regardless of what is rendered, so this is presentation,
        not the control.
      */}
      {courses.some((course) => !isAcademicCourse(course)) && <TaxonomyAssignmentPanel />}

      {isCreating && (
        <form
          id="new-course-form"
          onSubmit={handleCreateCourse}
          data-testid="new-course-form"
          className="bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-lg p-6 space-y-4"
        >
          <h2 className="text-lg font-semibold">{isAr ? "إنشاء كورس جديد" : "Create a new Course"}</h2>

          {/*
            University then Subject, before any Course copy. The academic identity
            is what the Course IS; the title and description describe it.
          */}
          <AcademicSubjectPicker
            onChange={(selection) => {
              setNewAcademic(selection);
              if (selection) setRequestingMissingSubject(false);
            }}
            onInstitutionChange={(institutionID) => {
              setNewInstitutionID(institutionID);
              setRequestingMissingSubject(false);
            }}
            onRequestMissing={() => setRequestingMissingSubject(true)}
          />

          {requestingMissingSubject && (
            <fieldset className="space-y-3 rounded-md border border-amber-300 p-4" data-testid="new-course-subject-request">
              <legend className="px-1 text-sm font-semibold">
                {isAr ? "طلب إضافة مادة" : "Request a missing Subject"}
              </legend>
              <p className="text-xs text-slate-600 dark:text-slate-400">
                {isAr
                  ? "يمكنك متابعة إعداد محتوى الكورس، لكن لا يمكن إرساله للمراجعة حتى تربطه الإدارة بمادة رسمية."
                  : "You can keep building the Course, but it cannot be submitted until Admin links an official Subject."}
              </p>
              <input
                value={requestedCode}
                onChange={(event) => setRequestedCode(event.target.value)}
                placeholder={isAr ? "الرمز الرسمي (اختياري)" : "Official code (optional)"}
                data-testid="subject-request-code"
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
              <input
                value={requestedTitleAr}
                onChange={(event) => setRequestedTitleAr(event.target.value)}
                placeholder={isAr ? "اسم المادة الرسمي بالعربية" : "Official Arabic Subject title"}
                required
                data-testid="subject-request-title-ar"
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
              <input
                value={requestedTitleEn}
                onChange={(event) => setRequestedTitleEn(event.target.value)}
                placeholder={isAr ? "اسم المادة الرسمي بالإنجليزية" : "Official English Subject title"}
                required
                data-testid="subject-request-title-en"
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
              <textarea
                value={requestedNote}
                onChange={(event) => setRequestedNote(event.target.value)}
                placeholder={isAr ? "سياق أو ملاحظة للإدارة" : "Context or note for Admin"}
                rows={2}
                data-testid="subject-request-note"
                className="w-full rounded-md border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 p-2 text-sm"
              />
            </fieldset>
          )}

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
            disabled={busy || (!newAcademic && !(requestingMissingSubject && newInstitutionID && requestedTitleAr && requestedTitleEn))}
            data-testid="create-course"
            className="rounded-md bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-700 disabled:opacity-50"
          >
            {isAr ? "إنشاء الكورس" : "Create Course"}
          </button>
          {!newAcademic && !requestingMissingSubject && (
            <p className="text-xs text-slate-600 dark:text-slate-400" data-testid="create-course-needs-subject">
              {isAr
                ? "اختر الجامعة والمادة أولًا."
                : "Choose the university and Subject first."}
            </p>
          )}
        </form>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="space-y-3">
          <h2 className="font-display text-base font-bold text-foreground">
            {instructor.courses.heading}
          </h2>
          <InstructorCourseList
            courses={courses}
            selectedCourseID={selectedCourseID}
            loading={loading}
            onSelect={selectCourse}
            onCreate={() => setIsCreating(true)}
            labels={instructor.courses}
            standingLabels={instructor.standing}
          />
        </div>

        {selectedCourse ? (
          <div className="md:col-span-2 space-y-6 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded-lg p-6">
            <div className="flex flex-wrap items-center justify-between gap-2 border-b pb-4">
              <div
                data-testid="selected-course-context"
                data-course-id={selectedCourse.id}
                data-revision-id={revision?.id ?? ""}
              >
                <div className="flex flex-wrap items-center gap-3">
                  <h2 className="text-xl font-bold mt-0.5">{courseTitle(selectedCourse)}</h2>
                  <button
                    type="button"
                    onClick={() => setShowRoster((current) => !current)}
                    aria-expanded={showRoster}
                    data-testid="course-roster-toggle"
                    className="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium hover:bg-slate-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 dark:border-slate-700 dark:hover:bg-slate-800"
                  >
                    {showRoster ? t.instructor.roster.close : t.instructor.roster.open}
                  </button>
                </div>
              </div>
              {isAcademicCourse(selectedCourse) && (
                <AcademicCourseContextPanel
                  course={selectedCourse}
                  busy={busy}
                  onChangeSubject={handleChangeSubject}
                  onCustomizeAudience={handleCustomizeAudience}
                  onResetAudience={handleResetAudience}
                  onRequestSubject={handleRequestSubject}
                />
              )}
              <span
                data-testid="revision-state"
                data-revision-state={revision?.state ?? selectedCourse.lifecycle ?? ""}
                className="text-xs px-3 py-1 rounded-full bg-slate-100 dark:bg-slate-800 font-medium"
              >
                {stateLabel(revision?.state, selectedCourse.lifecycle)}
              </span>
            </div>

            {showRoster ? <CourseRoster courseID={selectedCourse.id} /> : null}

            {/* Standing notice, not a toast: the Instructor usually returns in a later session. */}
            <ChangeRequestNotice revision={revision} labels={t.instructor.changeRequest} />

            {/* Edits to a candidate behind a live revision reach nobody until an Admin approves. */}
            {editingPublished ? <EditingPublishedNotice labels={t.instructor.revision} /> : null}

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
                  {/*
                    Legacy study year (D-093 §6). This is part of the legacy
                    classification, which an Academic Course does not carry and
                    must never be asked for — the server refuses it there. It
                    stays available for existing legacy Courses until T5.
                  */}
                  {!isAcademicCourse(selectedCourse) && (
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
                  )}
                  <button
                    type="submit"
                    disabled={busy}
                    data-testid="save-revision"
                    className="rounded-md bg-blue-600 px-3 py-2 text-xs font-medium text-white hover:bg-blue-700 disabled:opacity-50"
                  >
                    {isAr ? "حفظ التفاصيل" : "Save details"}
                  </button>
                </form>

                <PublicPreviewUpload
                  courseID={selectedCourse.id}
                  revisionID={revision.id}
                  hasPreview={Boolean(revision.preview_asset_version_id)}
                  locale={locale}
                  onChanged={refreshSelectedCourse}
                />

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
                                {/* Lab Materials are shown but not editable here: D-088
                                    covers Lesson video and Lesson Resources only. */}
                                {(lesson.files ?? []).some((file) => file.kind === "LAB_MATERIAL") && (
                                  <div className="text-[11px] text-slate-500 mt-1">
                                    {(lesson.files ?? [])
                                      .filter((file) => file.kind === "LAB_MATERIAL")
                                      .map((file) => (
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
                                <LessonResourceUpload
                                  courseID={selectedCourse.id}
                                  revisionID={revision.id!}
                                  lessonID={lesson.id}
                                  locale={locale}
                                  files={lesson.files ?? []}
                                  onChanged={refreshSelectedCourse}
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
              <RevisionWorkflowPanel
                workflow={workflow}
                busy={busy}
                labels={t.instructor.revision}
                onStart={handleStartRevision}
              />
            )}
          </div>
        ) : (
          <div className="md:col-span-2 border border-dashed rounded-lg p-12 text-center text-slate-500">
            {isAr ? "اختر دورة لعرض المحتوى والتعديل" : "Select a course to edit content"}
          </div>
        )}
      </div>
    </WorkspacePage>
  );
}
