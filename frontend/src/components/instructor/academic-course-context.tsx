"use client";

import { useEffect, useState } from "react";
import { getAuthoringSubject, type AuthoringSubject } from "@/lib/api/authoring-academic";
import { AcademicSubjectPicker, type AcademicSubjectSelection } from "./academic-subject-picker";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { CourseWire } from "@/lib/api/authoring";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { RevisionAudienceEditor } from "./revision-audience-editor";
import { SubjectRequestState, type MissingSubjectInput } from "./subject-request-state";
import { academicIdentity } from "./academic-identity";

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
 *
 * The identity itself is read from the Course payload, which has carried the expanded academic
 * context — university, subject, code, owning units, both languages — all along. This panel used
 * to render "Loading Subject details…" while re-fetching a Subject it had already been handed, so
 * the Instructor's own university appeared a request later than the Course it belongs to. The
 * fetch remains, because the audience editor needs the Subject's Programme associations and those
 * are not on the Course payload; it no longer gates the identity.
 */
export function AcademicCourseContextPanel({
  course,
  labels,
  onChangeSubject,
  onCustomizeAudience,
  onResetAudience,
  onRequestSubject,
  busy = false,
}: {
  course: CourseWire;
  labels: Dictionary["instructor"]["academic"];
  onChangeSubject: (subjectID: string) => void;
  onCustomizeAudience: (programIDs: string[]) => void;
  onResetAudience: () => void;
  onRequestSubject: (input: MissingSubjectInput) => Promise<boolean>;
  busy?: boolean;
}) {
  const { locale } = useLocale();
  const [subject, setSubject] = useState<AuthoringSubject | null>(null);
  const [editing, setEditing] = useState(false);

  const institutionID = course.institution_id ?? "";
  const subjectID = course.subject_id ?? "";
  const identity = academicIdentity(course, locale);

  // `live_revision_id` is the publication-history fact the whole product uses;
  // it is set once and never cleared.
  const published = Boolean(course.live_revision_id);
  const underReview = course.editable_revision?.state === "PENDING_REVIEW";
  const correctable = !published && !underReview;
  const activeRevision = course.editable_revision ?? course.live_revision;
  const audienceEditable =
    activeRevision?.state === "DRAFT" || activeRevision?.state === "CHANGES_REQUESTED";

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
      aria-labelledby="academic-identity-title"
      className="rounded-lg border border-border bg-card p-4"
    >
      <h3
        id="academic-identity-title"
        className="font-display text-base font-bold text-foreground"
      >
        {labels.title}
      </h3>
      <p className="mt-1 text-sm text-muted-foreground">{labels.lead}</p>

      {identity ? (
        /*
         * Named terms rather than unlabelled lines. The Instructor is being asked to confirm that
         * this is the right course for the right subject at the right university, and a stack of
         * bare strings does not say which of them is which.
         */
        <dl className="mt-4 space-y-3" data-testid="academic-course-subject">
          <AcademicTerm label={labels.institutionLabel} value={identity.institution} />
          {identity.subject ? (
            <AcademicTerm
              label={labels.subjectLabel}
              value={identity.subject}
              testID="academic-course-subject-name"
            />
          ) : null}
          {identity.subjectCode ? (
            <AcademicTerm label={labels.codeLabel} value={identity.subjectCode} />
          ) : null}
          {identity.units.length > 0 ? (
            <AcademicTerm
              label={labels.unitLabel}
              value={identity.units.join(" · ")}
              testID="academic-course-subject-context"
            />
          ) : null}
        </dl>
      ) : null}

      {subjectID ? (
        subject ? (
          <div className="mt-4">
            <RevisionAudienceEditor
              subject={subject}
              audience={activeRevision?.audience}
              labels={labels.audience}
              editable={audienceEditable}
              busy={busy}
              onCustomize={onCustomizeAudience}
              onReset={onResetAudience}
            />
          </div>
        ) : identity ? null : (
          <p className="mt-4 text-sm text-muted-foreground">{labels.loading}</p>
        )
      ) : (
        <div className="mt-4">
          <SubjectRequestState courseID={course.id} busy={busy} onRequest={onRequestSubject} />
        </div>
      )}

      {published && (
        <p className="mt-4 text-sm text-muted-foreground" data-testid="academic-course-subject-locked">
          {labels.lockedPublished}
        </p>
      )}

      {!published && underReview && (
        <p
          className="mt-4 text-sm text-muted-foreground"
          data-testid="academic-course-subject-in-review"
        >
          {labels.lockedInReview}
        </p>
      )}

      {correctable && !editing && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-4"
          onClick={() => setEditing(true)}
          disabled={busy}
          data-testid="academic-course-edit-subject"
        >
          {labels.change}
        </Button>
      )}

      {correctable && editing && (
        <div className="mt-4 space-y-3" data-testid="academic-course-subject-editor">
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
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setEditing(false)}
            data-testid="academic-course-cancel-subject"
          >
            {labels.cancel}
          </Button>
        </div>
      )}
    </section>
  );
}

function AcademicTerm({
  label,
  value,
  testID,
}: {
  label: string;
  value: string;
  testID?: string;
}) {
  return (
    <div>
      <dt className="font-display text-xs font-bold uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      {/* Arabic institution names sit beside Latin course codes in the same list. */}
      <dd className="mt-0.5 text-sm text-foreground" data-testid={testID}>
        <bdi>{value}</bdi>
      </dd>
    </div>
  );
}
