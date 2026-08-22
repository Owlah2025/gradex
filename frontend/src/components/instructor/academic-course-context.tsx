"use client";

import { useEffect, useState } from "react";
import {
  getAuthoringSubject,
  subjectContext,
  subjectLabel,
  type AuthoringSubject,
} from "@/lib/api/authoring-academic";
import { AcademicSubjectPicker, type AcademicSubjectSelection } from "./academic-subject-picker";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { CourseWire } from "@/lib/api/authoring";
import { RevisionAudienceEditor } from "./revision-audience-editor";
import { SubjectRequestState, type MissingSubjectInput } from "./subject-request-state";

/**
 * The academic identity of an Academic Catalog Course, on the authoring surface
 * (D-093 §4, §5).
 *
 * Subject is Course-level stable identity, not revision metadata: a Course that
 * teaches a different Subject is a different Course. So this panel shows the
 * Subject as identity and offers to change it only while the lifecycle genuinely
 * permits — never as a disabled legacy selector.
 *
 * The three states, all decided from server facts:
 *
 *   never published, not under review  → correctable
 *   candidate under review             → read-only, explained
 *   published                          → read-only academic identity, permanent
 *
 * The server enforces all three independently (T4-A domain command plus a
 * database trigger). Nothing here is the control; it only avoids offering an
 * action that would be refused.
 */
export function AcademicCourseContextPanel({
  course,
  onChangeSubject,
  onCustomizeAudience,
  onResetAudience,
  onRequestSubject,
  busy = false,
}: {
  course: CourseWire;
  onChangeSubject: (subjectID: string) => void;
  onCustomizeAudience: (programIDs: string[]) => void;
  onResetAudience: () => void;
  onRequestSubject: (input: MissingSubjectInput) => Promise<boolean>;
  busy?: boolean;
}) {
  const { locale } = useLocale();
  const isAr = locale === "ar";
  const [subject, setSubject] = useState<AuthoringSubject | null>(null);
  const [editing, setEditing] = useState(false);

  const institutionID = course.institution_id ?? "";
  const subjectID = course.subject_id ?? "";

  // `live_revision_id` is the publication-history fact the whole product uses;
  // it is set once and never cleared.
  const published = Boolean(course.live_revision_id);
  const underReview = course.editable_revision?.state === "PENDING_REVIEW";
  const correctable = !published && !underReview;
  const activeRevision = course.editable_revision ?? course.live_revision;
  const audienceEditable = activeRevision?.state === "DRAFT" || activeRevision?.state === "CHANGES_REQUESTED";

  useEffect(() => {
    if (!institutionID || !subjectID) {
      setSubject(null);
      return;
    }
    getAuthoringSubject({ institutionID, subjectID, locale })
      .then(setSubject)
      .catch(() => setSubject(null));
  }, [institutionID, subjectID, locale]);

  const apply = (selection: AcademicSubjectSelection | null) => {
    if (!selection) return;
    onChangeSubject(selection.subject.id);
    setEditing(false);
  };

  return (
    <section
      data-testid="academic-course-context"
      className="rounded-lg border border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-900 p-4 space-y-3"
    >
      <h3 className="text-sm font-semibold">{isAr ? "الهوية الأكاديمية" : "Academic identity"}</h3>

      {subject ? (
        <div className="space-y-1">
          <p className="text-sm" data-testid="academic-course-subject">
            {subjectLabel(subject, locale)}
          </p>
          {subjectContext(subject, locale) && (
            <p className="text-xs text-slate-600 dark:text-slate-400" data-testid="academic-course-subject-context">
              {subjectContext(subject, locale)}
            </p>
          )}
          <RevisionAudienceEditor
            subject={subject}
            audience={activeRevision?.audience}
            editable={audienceEditable}
            busy={busy}
            onCustomize={onCustomizeAudience}
            onReset={onResetAudience}
          />
        </div>
      ) : subjectID ? (
        <p className="text-xs text-slate-600 dark:text-slate-400">
          {isAr ? "جارٍ تحميل بيانات المادة…" : "Loading Subject details…"}
        </p>
      ) : (
        <SubjectRequestState courseID={course.id} busy={busy} onRequest={onRequestSubject} />
      )}

      {published && (
        <p className="text-xs text-slate-600 dark:text-slate-400" data-testid="academic-course-subject-locked">
          {isAr
            ? "تم نشر هذا الكورس. المادة جزء من هويته ولا يمكن تغييرها."
            : "This Course has been published. Its Subject is part of its identity and cannot change."}
        </p>
      )}

      {!published && underReview && (
        <p className="text-xs text-slate-600 dark:text-slate-400" data-testid="academic-course-subject-in-review">
          {isAr
            ? "الكورس قيد المراجعة. لا يمكن تغيير المادة حتى تنتهي المراجعة."
            : "This Course is under review. The Subject cannot change until the review ends."}
        </p>
      )}

      {correctable && !editing && (
        <button
          type="button"
          onClick={() => setEditing(true)}
          disabled={busy}
          data-testid="academic-course-edit-subject"
          className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs disabled:opacity-50"
        >
          {isAr ? "تغيير المادة" : "Change Subject"}
        </button>
      )}

      {correctable && editing && (
        <div className="space-y-2" data-testid="academic-course-subject-editor">
          {/*
            Scoped to the Course's own Institution. A Course's University is
            stable academic context after creation: the Subject may be corrected
            within it, but a Course does not migrate between universities, and a
            Subject from another Institution is refused by the server and by a
            composite foreign key.
          */}
          <AcademicSubjectPicker
            idPrefix="academic-course"
            initialInstitutionID={institutionID}
            onChange={apply}
            disabled={busy}
          />
          <button
            type="button"
            onClick={() => setEditing(false)}
            data-testid="academic-course-cancel-subject"
            className="rounded-md border border-slate-300 dark:border-slate-700 px-3 py-1 text-xs"
          >
            {isAr ? "إلغاء" : "Cancel"}
          </button>
        </div>
      )}
    </section>
  );
}
