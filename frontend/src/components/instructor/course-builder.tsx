"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { TaxonomyAssignmentPanel } from "./taxonomy-assignment-panel";
import type { AcademicSubjectSelection } from "./academic-subject-picker";
import { AcademicCourseContextPanel } from "./academic-course-context";
import { LessonVideoUpload } from "./lesson-video-upload";
import { PublicPreviewUpload } from "./public-preview-upload";
import { LessonResourceUpload } from "./lesson-resource-upload";
import { ChangeRequestNotice } from "./change-request-notice";
import { editsPublishedCourse, publicationMode, revisionWorkflow } from "./revision-workflow";
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
  publishCourseRevision,
  reorderLessons,
  reorderSections,
  updateCourseRevision,
  type CourseWire,
} from "@/lib/api/authoring";
import { isAcademicCourse } from "@/lib/api/catalog";
import { describeApiError } from "@/lib/api/api-error";
import { currentCSRFToken } from "@/lib/identity/session";
import { createSubjectRequest } from "@/lib/api/subject-requests";
import { CourseRoster } from "./course-roster";
import { InstructorCourseList } from "./instructor-course-list";
import { courseDisplayTitle, courseStanding } from "./course-standing";
import { CoursePricingSummary } from "./course-pricing-summary";
import { NewCourseForm } from "./new-course-form";
import { SubmissionPanel } from "./submission-panel";
import { CourseStandingBanner } from "./course-standing-banner";
import { SubmittedCourseSummary } from "./submitted-course-summary";
import { describeSubmissionRejection } from "./submission-readiness";
import { CurriculumBuilder } from "./curriculum-builder";
import { orderLessons, orderSections, persistOptimisticOrder, replaceEditableRevision } from "./curriculum-order";
import { authoringPlan } from "./authoring-plan";
import {
  AuthoringAdvanceOn,
  AuthoringContinue,
  AuthoringWorkflow,
} from "./authoring-workflow";
import { AuthoringSaveStateLine, type AuthoringSaveState } from "./authoring-save-state";
import { ErrorState } from "@/components/common/error-state";
import { EmptyState } from "@/components/common/empty-state";
import {
  WorkspacePage,
  WorkspacePageHeader,
} from "@/components/layout/workspace-page";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

/**
 * Instructor Course Authoring Studio.
 *
 * Every Course, Section, and Lesson rendered here comes from the Go API and is
 * written back through it. The component holds no persisted authored content of
 * its own: successful commands either install the server's canonical response or
 * re-read the owned-Course graph, so a page reload produces the same content.
 */
const STUDY_YEARS = ["PREP", "YEAR_1", "YEAR_2", "YEAR_3", "YEAR_4"] as const;

import { CourseThumbnailUpload } from "./course-thumbnail-upload";

export function CourseBuilder() {
  const { locale, t } = useLocale();

  const [courses, setCourses] = useState<CourseWire[]>([]);
  const [selectedCourseID, setSelectedCourseID] = useState<string | null>(null);
  const [showRoster, setShowRoster] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [orderState, setOrderState] = useState<"IDLE" | "SAVING" | "SAVED" | "FAILED">("IDLE");
  const [orderError, setOrderError] = useState<string | null>(null);
  const orderRequestInFlight = useRef(false);
  const [thumbnailBlocked, setThumbnailBlocked] = useState<Record<string, boolean>>({});

  // A submission rejection is reported twice: once in the page-level error
  // region, and once beside the Submit control itself. The founder's manual
  // test clicked Submit near the bottom of a long page, saw nothing change,
  // and read the click as a no-op — the server's reason was rendered far
  // above the viewport. The second region is focused on failure so the reason
  // is where the click was.
  const [submitRejection, setSubmitRejection] = useState<
    { reasons: string[]; detail?: string | null } | null
  >(null);

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

  /**
   * Save feedback for the course-details form.
   *
   * `SAVED` is set from the resolved server call and nothing else, and "unsaved" is *derived* by
   * comparing the form against the revision the server returned rather than tracked as a flag, so
   * the studio cannot claim a state the server has not reached.
   */
  const [detailsOutcome, setDetailsOutcome] = useState<"NONE" | "SAVING" | "SAVED" | "FAILED">(
    "NONE",
  );
  /** Incremented when the server accepts a details save; the disclosure advances once per step. */
  const [basicsAdvanceToken, setBasicsAdvanceToken] = useState(0);

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
  // D-097: which act the primary control performs. Read from the same durable
  // fact the server reads, so the studio never offers the wrong one.
  const publication = publicationMode(selectedCourse);
  const standing = courseStanding(selectedCourse);
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
    setDetailsOutcome("NONE");
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
      setError(instructor.media.csrfMissing);
      return null;
    }
    return csrf;
  };

  /** Runs one authoring command with a single busy flag, so a double click cannot issue it twice. */
  const command = async (
    action: (csrf: string) => Promise<void>,
    options?: { onFailure?: (message: string, cause: unknown) => boolean | void },
  ) => {
    const csrf = requireCSRF();
    if (!csrf || busy) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await action(csrf);
    } catch (cause) {
      // The server's own reason, verbatim from `describeApiError`, including every submission
      // violation code. A handler may claim the failure by returning true — submission does,
      // because it renders the same refusal in the Instructor's language beside the control that
      // produced it, and showing the raw `CODE · lesson:<uuid>` join as well would put back
      // exactly the identifiers this surface no longer asks anyone to read.
      const message = describeApiError(cause, locale);
      const handled = options?.onFailure?.(message, cause) === true;
      if (!handled) setError(message);
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
      setNotice(`${academic ? details.created : details.createdWithRequest} ${t.courseThumbnail.createNote}`);
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
      setNotice(instructor.academic.changed);
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
      setNotice(details.subjectRequestSent);
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
      setNotice(instructor.academic.audienceCustomized);
    });
  };

  const handleResetAudience = () => {
    if (!selectedCourse || !revision?.id) return;
    void command(async (csrf) => {
      await resetRevisionAudience({ courseID: selectedCourse.id, revisionID: revision.id!, locale, csrf });
      await refreshSelectedCourse();
      setNotice(instructor.academic.audienceReset);
    });
  };

  const handleSaveRevision = (event: React.FormEvent) => {
    event.preventDefault();
    if (!selectedCourse || !revision?.id) return;
    setDetailsOutcome("SAVING");
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
      setNotice(details.saved);
      setDetailsOutcome("SAVED");
      // The disclosure advances only from here: after an intentional submit that the server
      // accepted. Nothing about focus, blur or typing reaches this line.
      setBasicsAdvanceToken((token) => token + 1);
    }, {
      onFailure: () => {
        setDetailsOutcome("FAILED");
        return false;
      },
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

  const handleReorderSections = async (sectionIDs: string[], movedSectionID: string) => {
    if (!selectedCourse || !revision?.id || orderRequestInFlight.current) return;
    const csrf = requireCSRF();
    if (!csrf) return;
    const before = revision;
    orderRequestInFlight.current = true;
    setOrderState("SAVING");
    setOrderError(null);
    try {
      await persistOptimisticOrder({
        before,
        optimistic: { ...before, sections: orderSections(before.sections ?? [], sectionIDs) },
        apply: (canonical) => setCourses((current) => replaceEditableRevision(current, selectedCourse.id, canonical)),
        persist: () => reorderSections({
          courseID: selectedCourse.id, revisionID: revision.id!, sectionIDs, locale, csrf,
        }),
      });
      setOrderState("SAVED");
    } catch (cause) {
      setOrderError(describeApiError(cause, locale));
      setOrderState("FAILED");
    } finally {
      orderRequestInFlight.current = false;
      requestAnimationFrame(() => document.getElementById(`section-drag-handle-${movedSectionID}`)?.focus());
    }
  };

  const handleReorderLessons = async (sectionID: string, lessonIDs: string[], movedLessonID: string) => {
    if (!selectedCourse || !revision?.id || orderRequestInFlight.current) return;
    const section = (revision.sections ?? []).find((candidate) => candidate.id === sectionID);
    if (!section) return;
    const csrf = requireCSRF();
    if (!csrf) return;
    const before = revision;
    const optimisticSections = (before.sections ?? []).map((candidate) =>
      candidate.id === sectionID ? orderLessons(candidate, lessonIDs) : candidate,
    );
    orderRequestInFlight.current = true;
    setOrderState("SAVING");
    setOrderError(null);
    try {
      await persistOptimisticOrder({
        before,
        optimistic: { ...before, sections: optimisticSections },
        apply: (canonical) => setCourses((current) => replaceEditableRevision(current, selectedCourse.id, canonical)),
        persist: () => reorderLessons({
          courseID: selectedCourse.id, revisionID: revision.id!, sectionID, lessonIDs, locale, csrf,
        }),
      });
      setOrderState("SAVED");
    } catch (cause) {
      setOrderError(describeApiError(cause, locale));
      setOrderState("FAILED");
    } finally {
      orderRequestInFlight.current = false;
      requestAnimationFrame(() => document.getElementById(`lesson-drag-handle-${movedLessonID}`)?.focus());
    }
  };

  /*
    One control, two acts. A course that has never been live goes to an
    administrator; one that has been live is published by its own instructor
    (D-097). The branch is on the server's own durable publication fact, and
    the server refuses the wrong call either way, so a stale tab that picks the
    wrong route is rejected rather than acted on.
  */
  const handleSubmit = () => {
    if (!selectedCourse || !revision?.id || thumbnailBlocked[revision.id]) return;
    const publishesDirectly = publication === "SUBSEQUENT_PUBLICATION";
    setSubmitRejection(null);
    void command(
      async (csrf) => {
        const send = publishesDirectly ? publishCourseRevision : submitCourseRevision;
        await send({
          courseID: selectedCourse.id,
          revisionID: revision.id!,
          locale,
          csrf,
        });
        await loadCourses(selectedCourse.id);
        await refreshSelectedCourse();
        setNotice(
          publishesDirectly ? instructor.submission.published : instructor.submission.submitted,
        );
      },
      {
        /*
          A submission rejection is the one server failure an Instructor is expected to act on
          themselves, so it is translated rather than passed through. `describeApiError` renders
          each violation as `CODE · lesson:<uuid>`, which names an object by an identifier that
          appears nowhere in the product.
        */
        onFailure: (message, cause): boolean => {
          const translated = describeSubmissionRejection(
            cause,
            (code) =>
              instructor.submission.violation[
                code as keyof typeof instructor.submission.violation
              ],
          );
          setSubmitRejection(
            translated
              ? {
                  reasons: translated.reasons,
                  // The raw server text is kept only when something in it was not understood.
                  detail: translated.hasUntranslated ? message : null,
                }
              : { reasons: [], detail: message },
          );
          return true;
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
  const details = instructor.details;
  const authoring = instructor.authoring;

  /**
   * Whether the details form differs from the revision the server last returned.
   *
   * Derived, not tracked. An "edited" flag has to be cleared by every path that saves, and the one
   * that forgets leaves a studio permanently claiming unsaved work — or worse, claiming none.
   */
  const detailsDirty =
    Boolean(revision) &&
    (detailTitleAr !== (revision?.title_ar ?? "") ||
      detailTitleEn !== (revision?.title_en ?? "") ||
      detailDescAr !== (revision?.description_ar ?? "") ||
      detailDescEn !== (revision?.description_en ?? "") ||
      detailStudyYear !== (revision?.study_year ?? ""));

  const saveState: AuthoringSaveState =
    detailsOutcome === "SAVING"
      ? "SAVING"
      : detailsOutcome === "FAILED"
        ? "FAILED"
        : detailsDirty
          ? "UNSAVED"
          : detailsOutcome === "SAVED"
            ? "SAVED"
            : "IDLE";

  /*
    The one unsaved-work protection, and deliberately the smallest one that is honest: the browser's
    own prompt, raised only while the form genuinely differs from the server's copy. Nothing is
    cached, nothing is written to local storage, and no course content leaves the tab — a draft
    cache would be a second source of truth for authored material that the server does not know
    about, which is a larger problem than the one it solves.
  */
  useEffect(() => {
    if (!detailsDirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [detailsDirty]);

  const untitled = useCallback(
    (kind: "section" | "lesson", position: number) =>
      kind === "section"
        ? `${instructor.submission.untitledSection} ${position}`
        : `${instructor.submission.untitledLesson} ${position}`,
    [instructor.submission],
  );

  const revisionID = revision?.id ?? null;
  const plan = useMemo(
    () =>
      selectedCourse && revisionID
        ? authoringPlan(selectedCourse, locale, untitled, {
            thumbnailUnresolved: Boolean(thumbnailBlocked[revisionID]),
          })
        : null,
    [selectedCourse, revisionID, locale, untitled, thumbnailBlocked],
  );

  /** Whether the authoring workflow owns this course's panels on this render. */
  const editableWorkflow = Boolean(revisionID && standing.editable && plan);

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

      {/*
        Legacy taxonomy compatibility (D-093 §6).

        This panel edits major_term_id / subject_term_id / study_year, which only
        a LEGACY_TAXONOMY Course carries. It stays mounted until T5 migrates the
        existing Courses that still depend on it, and it disappears entirely once
        an Instructor owns no legacy Course. The server refuses these fields on an
        Academic Course regardless of what is rendered, so this is presentation,
        not the control.
      */}
      {courses.some((course) => !isAcademicCourse(course)) && (
        <TaxonomyAssignmentPanel courses={courses} labels={instructor.legacyTaxonomy} />
      )}

      {isCreating && (
        <NewCourseForm
          draft={{
            titleAr: newTitleAr,
            titleEn: newTitleEn,
            descriptionAr: newDescAr,
            descriptionEn: newDescEn,
            requestedCode,
            requestedTitleAr,
            requestedTitleEn,
            requestedNote,
          }}
          onDraftChange={(patch) => {
            if (patch.titleAr !== undefined) setNewTitleAr(patch.titleAr);
            if (patch.titleEn !== undefined) setNewTitleEn(patch.titleEn);
            if (patch.descriptionAr !== undefined) setNewDescAr(patch.descriptionAr);
            if (patch.descriptionEn !== undefined) setNewDescEn(patch.descriptionEn);
            if (patch.requestedCode !== undefined) setRequestedCode(patch.requestedCode);
            if (patch.requestedTitleAr !== undefined) setRequestedTitleAr(patch.requestedTitleAr);
            if (patch.requestedTitleEn !== undefined) setRequestedTitleEn(patch.requestedTitleEn);
            if (patch.requestedNote !== undefined) setRequestedNote(patch.requestedNote);
          }}
          academic={newAcademic}
          onAcademicChange={(selection) => {
            setNewAcademic(selection);
            if (selection) setRequestingMissingSubject(false);
          }}
          onInstitutionChange={(institutionID) => {
            setNewInstitutionID(institutionID);
            setRequestingMissingSubject(false);
          }}
          requestingMissingSubject={requestingMissingSubject}
          onRequestMissing={() => setRequestingMissingSubject(true)}
          institutionID={newInstitutionID}
          busy={busy}
          labels={details}
          onSubmit={handleCreateCourse}
        />
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
          <div className="space-y-6 md:col-span-2">
            {/*
              The academic identity panel used to be rendered *inside* this header's flex row,
              between the course title and the status pill — a whole titled section wedged into a
              line of chrome. It is a region of the studio, so it sits among the regions.
            */}
            <div
              className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3 border-b border-border pb-4"
              data-testid="selected-course-context"
              data-course-id={selectedCourse.id}
              data-revision-id={revision?.id ?? ""}
            >
              <h2 className="min-w-0 font-display text-xl font-bold text-foreground">
                <bdi>{courseTitle(selectedCourse)}</bdi>
              </h2>
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setShowRoster((current) => !current)}
                  aria-expanded={showRoster}
                  data-testid="course-roster-toggle"
                >
                  {showRoster ? t.instructor.roster.close : t.instructor.roster.open}
                </Button>
              </div>
            </div>

            <CourseStandingBanner
              standing={standing}
              labels={instructor.standing}
              bannerLabels={instructor.standingBanner}
            />

            {showRoster ? <CourseRoster courseID={selectedCourse.id} /> : null}

            {/*
              The academic identity and the price belong to the course whatever state it is in, so
              a revision that is with an administrator still shows both. While the revision *is*
              editable they live inside the authoring workflow's own section, which is where the
              controls that change them belong; rendering them here as well would put the same two
              panels on the screen twice.
            */}
            {!editableWorkflow && (
              <>
                {isAcademicCourse(selectedCourse) && (
                  <AcademicCourseContextPanel
                    course={selectedCourse}
                    labels={instructor.academic}
                    busy={busy}
                    onChangeSubject={handleChangeSubject}
                    onCustomizeAudience={handleCustomizeAudience}
                    onResetAudience={handleResetAudience}
                    onRequestSubject={handleRequestSubject}
                  />
                )}
                <CoursePricingSummary course={selectedCourse} labels={instructor.price} />
              </>
            )}

            {/* Standing notice, not a toast: the Instructor usually returns in a later session. */}
            <ChangeRequestNotice revision={revision} labels={t.instructor.changeRequest} />

            {/* Edits to a candidate behind a live revision reach nobody until an Admin approves. */}
            {editingPublished ? <EditingPublishedNotice labels={t.instructor.revision} /> : null}

            {/*
              Gated on whether the revision *can* be edited, not on whether one exists. A submitted
              revision still exists, so this used to render the whole authoring form — every input
              live, Submit underneath — for a revision the server refuses every write to.
            */}
            {editableWorkflow && revision?.id && plan ? (
              <AuthoringWorkflow
                plan={plan}
                labels={authoring}
                curriculumLabels={instructor.curriculum}
                courseID={selectedCourse.id}
              >
                {{
                  BASICS: (
                    <form
                      onSubmit={handleSaveRevision}
                      className="space-y-4"
                      data-testid="revision-form"
                      aria-label={authoring.section.BASICS.title}
                    >
                      {/*
                        Visible labels, not `aria-label`. These four fields were named only to a
                        screen reader; a sighted Instructor met two identical empty boxes and had to
                        guess which was Arabic from the caret direction.
                      */}
                      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                        <Field label={details.titleAr} htmlFor="revision-title-ar">
                          <Input
                            id="revision-title-ar"
                            type="text"
                            lang="ar"
                            dir="rtl"
                            value={detailTitleAr}
                            onChange={(event) => setDetailTitleAr(event.target.value)}
                            data-testid="revision-title-ar"
                          />
                        </Field>
                        <Field label={details.titleEn} htmlFor="revision-title-en">
                          <Input
                            id="revision-title-en"
                            type="text"
                            lang="en"
                            dir="ltr"
                            value={detailTitleEn}
                            onChange={(event) => setDetailTitleEn(event.target.value)}
                            data-testid="revision-title-en"
                          />
                        </Field>
                        <Field label={details.descriptionAr} htmlFor="revision-description-ar">
                          <Textarea
                            id="revision-description-ar"
                            lang="ar"
                            dir="rtl"
                            rows={3}
                            value={detailDescAr}
                            onChange={(event) => setDetailDescAr(event.target.value)}
                            data-testid="revision-description-ar"
                          />
                        </Field>
                        <Field label={details.descriptionEn} htmlFor="revision-description-en">
                          <Textarea
                            id="revision-description-en"
                            lang="en"
                            dir="ltr"
                            rows={3}
                            value={detailDescEn}
                            onChange={(event) => setDetailDescEn(event.target.value)}
                            data-testid="revision-description-en"
                          />
                        </Field>
                      </div>
                      {/*
                        Legacy study year (D-093 §6). This is part of the legacy classification,
                        which an Academic Course does not carry and must never be asked for — the
                        server refuses it there. It stays available for existing legacy Courses
                        until T5.
                      */}
                      {!isAcademicCourse(selectedCourse) && (
                        <Field
                          label={details.studyYear}
                          htmlFor="revision-study-year"
                          className="max-w-xs"
                        >
                          <Select
                            id="revision-study-year"
                            value={detailStudyYear}
                            onChange={(event) => setDetailStudyYear(event.target.value)}
                            data-testid="revision-study-year"
                          >
                            <option value="">{details.studyYearUnset}</option>
                            {STUDY_YEARS.map((year) => (
                              <option key={year} value={year}>
                                {details.studyYears[year]}
                              </option>
                            ))}
                          </Select>
                        </Field>
                      )}
                      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
                        <Button
                          type="submit"
                          size="sm"
                          disabled={busy}
                          data-testid="save-revision"
                        >
                          {busy ? details.saving : authoring.saveAndContinue}
                        </Button>
                        <AuthoringSaveStateLine state={saveState} labels={authoring} />
                      </div>
                      <AuthoringAdvanceOn section="BASICS" token={basicsAdvanceToken} />
                    </form>
                  ),
                  DETAILS: (
                    <div className="space-y-5">
                      {isAcademicCourse(selectedCourse) && (
                        <AcademicCourseContextPanel
                          course={selectedCourse}
                          labels={instructor.academic}
                          busy={busy}
                          onChangeSubject={handleChangeSubject}
                          onCustomizeAudience={handleCustomizeAudience}
                          onResetAudience={handleResetAudience}
                          onRequestSubject={handleRequestSubject}
                        />
                      )}
                      {/* The launch price is an Admin decision, stated beside the course it applies
                          to rather than in a second panel with a second copy of the course list. */}
                      <CoursePricingSummary course={selectedCourse} labels={instructor.price} />
                      <AuthoringContinue section="DETAILS" label={authoring.continueAction} />
                    </div>
                  ),
                  PREVIEW: (
                    <div className="space-y-5">
                      <CourseThumbnailUpload
                        key={revision.id}
                        courseID={selectedCourse.id}
                        revisionID={revision.id}
                        assetID={revision.thumbnail_asset_version_id}
                        disabled={busy}
                        unresolved={Boolean(thumbnailBlocked[revision.id])}
                        onChanged={refreshSelectedCourse}
                        onBlockedChange={(blocked) =>
                          setThumbnailBlocked((current) => ({ ...current, [revision.id!]: blocked }))
                        }
                      />
                      {/* The public preview is the Course's own trailer and is uploaded here. A
                          Lesson video is a different act, on a different object, inside the
                          curriculum — the two are never offered by the same control. */}
                      <PublicPreviewUpload
                        courseID={selectedCourse.id}
                        revisionID={revision.id}
                        hasPreview={Boolean(revision.preview_asset_version_id)}
                        previewAssetVersionID={revision.preview_asset_version_id}
                        previewAssetState={revision.preview_asset_state}
                        locale={locale}
                        onChanged={refreshSelectedCourse}
                      />
                      <AuthoringContinue section="PREVIEW" label={authoring.continueAction} />
                    </div>
                  ),
                  CURRICULUM: (
                    <div className="space-y-5">
                      <CurriculumBuilder
                        revision={revision}
                        courseID={selectedCourse.id}
                        busy={busy || orderState === "SAVING"}
                        orderState={orderState}
                        orderError={orderError}
                        labels={instructor.curriculum}
                        lessonDrafts={lessonDrafts}
                        sectionTitleAr={secTitleAr}
                        sectionTitleEn={secTitleEn}
                        onSectionTitleChange={(patch) => {
                          if (patch.ar !== undefined) setSecTitleAr(patch.ar);
                          if (patch.en !== undefined) setSecTitleEn(patch.en);
                        }}
                        onLessonDraftChange={(sectionID, draft) =>
                          setLessonDrafts((current) => ({ ...current, [sectionID]: draft }))
                        }
                        onAddSection={handleAddSection}
                        onAddLesson={handleAddLesson}
                        onDeleteSection={handleDeleteSection}
                        onDeleteLesson={handleDeleteLesson}
                        onReorderSections={handleReorderSections}
                        onReorderLessons={handleReorderLessons}
                        onContentChanged={refreshSelectedCourse}
                      />
                      <AuthoringContinue section="CURRICULUM" label={authoring.continueAction} />
                    </div>
                  ),
                  REVIEW: (
                    <SubmissionPanel
                      course={selectedCourse}
                      labels={instructor.submission}
                      mode={publication}
                      busy={busy || Boolean(thumbnailBlocked[revision.id])}
                      rejection={submitRejection}
                      onSubmit={handleSubmit}
                    />
                  ),
                }}
              </AuthoringWorkflow>
            ) : standing.stage === "IN_REVIEW" && revision ? (
              <SubmittedCourseSummary revision={revision} labels={instructor.submitted} />
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
          <div className="md:col-span-2">
            <EmptyState
              title={
                courses.length === 0
                  ? instructor.courses.emptyTitle
                  : instructor.courses.selectPrompt
              }
              description={
                courses.length === 0
                  ? instructor.courses.emptyBody
                  : instructor.courses.selectPromptBody
              }
              action={
                courses.length === 0 ? (
                  <Button type="button" onClick={() => setIsCreating(true)}>
                    {instructor.courses.emptyAction}
                  </Button>
                ) : undefined
              }
            />
          </div>
        )}
      </div>
    </WorkspacePage>
  );
}
