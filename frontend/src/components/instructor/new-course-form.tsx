"use client";

import type React from "react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { AcademicSubjectPicker, type AcademicSubjectSelection } from "./academic-subject-picker";

type DetailsLabels = Dictionary["instructor"]["details"];

export type NewCourseDraft = {
  titleAr: string;
  titleEn: string;
  descriptionAr: string;
  descriptionEn: string;
  requestedCode: string;
  requestedTitleAr: string;
  requestedTitleEn: string;
  requestedNote: string;
};

/**
 * Creating a Course: the university and subject first, then what it is called.
 *
 * The order is the argument. The academic identity is what the Course *is* — it is the thing
 * students search by and the thing the submission validator refuses without — and the title and
 * description merely describe it. Asking for a title first would invite an Instructor to name a
 * course before deciding what it teaches.
 *
 * Every control here now carries a real label. The subject-request fields were four inputs
 * distinguished only by `placeholder`, which disappears the moment anything is typed into it and
 * is not a name a screen reader can rely on; two of them were `required`, so the form could refuse
 * to submit while pointing at a field whose purpose had just vanished.
 */
export function NewCourseForm({
  draft,
  onDraftChange,
  academic,
  onAcademicChange,
  onInstitutionChange,
  requestingMissingSubject,
  onRequestMissing,
  institutionID,
  busy,
  labels,
  onSubmit,
}: {
  draft: NewCourseDraft;
  onDraftChange: (patch: Partial<NewCourseDraft>) => void;
  academic: AcademicSubjectSelection | null;
  onAcademicChange: (selection: AcademicSubjectSelection | null) => void;
  onInstitutionChange: (institutionID: string) => void;
  requestingMissingSubject: boolean;
  onRequestMissing: () => void;
  institutionID: string;
  busy: boolean;
  labels: DetailsLabels;
  onSubmit: (event: React.FormEvent) => void;
}) {
  const requestComplete =
    requestingMissingSubject &&
    Boolean(institutionID) &&
    Boolean(draft.requestedTitleAr) &&
    Boolean(draft.requestedTitleEn);
  const canCreate = Boolean(academic) || requestComplete;

  return (
    <form
      id="new-course-form"
      onSubmit={onSubmit}
      data-testid="new-course-form"
      aria-labelledby="new-course-title"
      className="space-y-5 rounded-lg border border-border bg-card p-6"
    >
      <h2 id="new-course-title" className="font-display text-lg font-bold text-foreground">
        {labels.createTitle}
      </h2>

      <AcademicSubjectPicker
        onChange={onAcademicChange}
        onInstitutionChange={onInstitutionChange}
        onRequestMissing={onRequestMissing}
      />

      {requestingMissingSubject && (
        <fieldset
          className="space-y-4 rounded-lg border border-border bg-muted/40 p-4"
          data-testid="new-course-subject-request"
        >
          <legend className="px-1 font-display text-sm font-bold text-foreground">
            {labels.subjectRequestTitle}
          </legend>
          <p className="text-sm leading-6 text-muted-foreground">{labels.subjectRequestBody}</p>
          <Field label={labels.subjectRequestCode} htmlFor="subject-request-code">
            <Input
              id="subject-request-code"
              value={draft.requestedCode}
              onChange={(event) => onDraftChange({ requestedCode: event.target.value })}
              data-testid="subject-request-code"
            />
          </Field>
          <Field label={labels.subjectRequestTitleAr} htmlFor="subject-request-title-ar">
            <Input
              id="subject-request-title-ar"
              lang="ar"
              dir="rtl"
              value={draft.requestedTitleAr}
              onChange={(event) => onDraftChange({ requestedTitleAr: event.target.value })}
              required
              data-testid="subject-request-title-ar"
            />
          </Field>
          <Field label={labels.subjectRequestTitleEn} htmlFor="subject-request-title-en">
            <Input
              id="subject-request-title-en"
              lang="en"
              dir="ltr"
              value={draft.requestedTitleEn}
              onChange={(event) => onDraftChange({ requestedTitleEn: event.target.value })}
              required
              data-testid="subject-request-title-en"
            />
          </Field>
          <Field label={labels.subjectRequestNote} htmlFor="subject-request-note">
            <Textarea
              id="subject-request-note"
              rows={2}
              value={draft.requestedNote}
              onChange={(event) => onDraftChange({ requestedNote: event.target.value })}
              data-testid="subject-request-note"
            />
          </Field>
        </fieldset>
      )}

      {/*
        Each language's field is marked with its own `lang` and `dir`. Without them an Arabic title
        typed into a form the browser believes is English gets the wrong caret behaviour and the
        wrong punctuation placement, which is visible the moment a title ends in a full stop.
      */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <Field label={labels.titleAr} htmlFor="new-course-title-ar">
          <Input
            id="new-course-title-ar"
            lang="ar"
            dir="rtl"
            value={draft.titleAr}
            onChange={(event) => onDraftChange({ titleAr: event.target.value })}
            required
            data-testid="new-course-title-ar"
          />
        </Field>
        <Field label={labels.titleEn} htmlFor="new-course-title-en">
          <Input
            id="new-course-title-en"
            lang="en"
            dir="ltr"
            value={draft.titleEn}
            onChange={(event) => onDraftChange({ titleEn: event.target.value })}
            required
            data-testid="new-course-title-en"
          />
        </Field>
        <Field label={labels.descriptionAr} htmlFor="new-course-description-ar">
          <Textarea
            id="new-course-description-ar"
            lang="ar"
            dir="rtl"
            rows={3}
            value={draft.descriptionAr}
            onChange={(event) => onDraftChange({ descriptionAr: event.target.value })}
            data-testid="new-course-description-ar"
          />
        </Field>
        <Field label={labels.descriptionEn} htmlFor="new-course-description-en">
          <Textarea
            id="new-course-description-en"
            lang="en"
            dir="ltr"
            rows={3}
            value={draft.descriptionEn}
            onChange={(event) => onDraftChange({ descriptionEn: event.target.value })}
            data-testid="new-course-description-en"
          />
        </Field>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Button type="submit" disabled={busy || !canCreate} data-testid="create-course">
          {busy ? labels.creating : labels.createAction}
        </Button>
        {!canCreate && !requestingMissingSubject ? (
          /*
            Said beside the control it explains rather than only discovered by clicking. The
            button is disabled, so a reader who cannot see the disabled state has no other way to
            learn why nothing happens.
          */
          <p className="text-sm text-muted-foreground" data-testid="create-course-needs-subject">
            {labels.needsSubject}
          </p>
        ) : null}
      </div>
    </form>
  );
}
