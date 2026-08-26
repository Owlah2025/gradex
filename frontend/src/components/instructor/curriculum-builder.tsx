"use client";

import React, { useState } from "react";
import { Check, CircleDashed } from "lucide-react";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { CourseRevisionWire, LessonWire, SectionWire } from "@/lib/api/catalog";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { EmptyState } from "@/components/common/empty-state";
import { LessonVideoUpload } from "./lesson-video-upload";
import { LessonResourceUpload } from "./lesson-resource-upload";

type CurriculumLabels = Dictionary["instructor"]["curriculum"];

export type LessonDraft = { ar: string; en: string };

/**
 * Sections and lessons: the part of the studio an Instructor spends their time in.
 *
 * Three things were wrong with what this replaces, beyond the palette.
 *
 * A lesson that had a video announced it as `Video attached: 4f9a…-…` in a monospace face — the
 * asset-version UUID, printed at the Instructor as though it were the answer to a question they
 * had asked. Whether the video is there is the whole question. Which row of the media table holds
 * it is not something anyone outside this repository can act on. Lab materials had the same
 * problem in miniature, rendering `[LAB_MATERIAL]` as a visible prefix.
 *
 * Adding a section or lesson meant typing into inputs identified only by `placeholder`, so the
 * moment a title was typed the field stopped saying which language it wanted, and a screen reader
 * never had a name for it at all.
 *
 * Deleting a section removed every lesson inside it — and every video uploaded to those lessons —
 * from a small underlined link, with no confirmation and no undo on the server. That is the one
 * action here that genuinely earns a dialog, so it is the one that gets one; adding a section back
 * costs a sentence of typing, and is not guarded.
 */
export function CurriculumBuilder({
  revision,
  courseID,
  busy,
  labels,
  lessonDrafts,
  sectionTitleAr,
  sectionTitleEn,
  onSectionTitleChange,
  onLessonDraftChange,
  onAddSection,
  onAddLesson,
  onDeleteSection,
  onDeleteLesson,
  onContentChanged,
}: {
  revision: CourseRevisionWire;
  courseID: string;
  busy: boolean;
  labels: CurriculumLabels;
  lessonDrafts: Record<string, LessonDraft>;
  sectionTitleAr: string;
  sectionTitleEn: string;
  onSectionTitleChange: (patch: { ar?: string; en?: string }) => void;
  onLessonDraftChange: (sectionID: string, draft: LessonDraft) => void;
  onAddSection: (event: React.FormEvent) => void;
  onAddLesson: (event: React.FormEvent, sectionID: string) => void;
  onDeleteSection: (sectionID: string) => void;
  onDeleteLesson: (lessonID: string) => void;
  onContentChanged: () => void | Promise<void>;
}) {
  const { locale } = useLocale();
  const sections = revision.sections ?? [];

  /**
   * The one pending destructive action, held as a single value rather than a flag per row.
   * A curriculum can carry hundreds of lessons, and only one of them is ever being confirmed.
   */
  const [pendingDelete, setPendingDelete] = useState<
    { kind: "section"; id: string } | { kind: "lesson"; id: string } | null
  >(null);

  const confirmDelete = () => {
    if (!pendingDelete) return;
    if (pendingDelete.kind === "section") onDeleteSection(pendingDelete.id);
    else onDeleteLesson(pendingDelete.id);
    setPendingDelete(null);
  };

  const lessonCount = sections.reduce(
    (total, section) => total + (section.lessons?.length ?? 0),
    0,
  );

  return (
    <section className="space-y-4" aria-labelledby="curriculum-title" data-testid="curriculum">
      <div>
        <h3 id="curriculum-title" className="font-display text-base font-bold text-foreground">
          {labels.title}
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">{labels.lead}</p>
        {sections.length > 0 ? (
          <p className="mt-1 text-xs text-muted-foreground" data-testid="curriculum-counts">
            {/*
              Label then number, rather than "1 sections". English needs one/other and Arabic needs
              six plural forms; picking one form for both is how "١ أقسام" gets shipped.
            */}
            {labels.sectionCount}: {sections.length} · {labels.lessonCount}: {lessonCount}
          </p>
        ) : null}
      </div>

      {sections.length === 0 ? (
        <div data-testid="curriculum-empty">
          <EmptyState
            density="compact"
            title={labels.emptyTitle}
            description={labels.emptyBody}
          />
        </div>
      ) : (
        <ol className="space-y-4">
          {sections.map((section, index) => (
            <SectionRow
              key={section.id}
              section={section}
              index={index}
              courseID={courseID}
              revisionID={revision.id!}
              busy={busy}
              labels={labels}
              locale={locale}
              draft={lessonDrafts[section.id] ?? { ar: "", en: "" }}
              onLessonDraftChange={(draft) => onLessonDraftChange(section.id, draft)}
              onAddLesson={(event) => onAddLesson(event, section.id)}
              onRequestDeleteSection={() => setPendingDelete({ kind: "section", id: section.id })}
              onRequestDeleteLesson={(lessonID) =>
                setPendingDelete({ kind: "lesson", id: lessonID })
              }
              onContentChanged={onContentChanged}
            />
          ))}
        </ol>
      )}

      <form
        onSubmit={onAddSection}
        data-testid="add-section-form"
        className="rounded-lg border border-border bg-card p-4"
      >
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_1fr_auto] lg:items-end">
          <Field label={labels.addSectionTitleAr} htmlFor="section-title-ar">
            <Input
              id="section-title-ar"
              lang="ar"
              dir="rtl"
              value={sectionTitleAr}
              onChange={(event) => onSectionTitleChange({ ar: event.target.value })}
              data-testid="section-title-ar"
            />
          </Field>
          <Field label={labels.addSectionTitleEn} htmlFor="section-title-en">
            <Input
              id="section-title-en"
              lang="en"
              dir="ltr"
              value={sectionTitleEn}
              onChange={(event) => onSectionTitleChange({ en: event.target.value })}
              data-testid="section-title-en"
            />
          </Field>
          <Button type="submit" disabled={busy} data-testid="add-section">
            {labels.addSection}
          </Button>
        </div>
      </form>

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
        title={
          pendingDelete?.kind === "lesson"
            ? labels.confirmDeleteLessonTitle
            : labels.confirmDeleteSectionTitle
        }
        body={
          pendingDelete?.kind === "lesson"
            ? labels.confirmDeleteLessonBody
            : labels.confirmDeleteSectionBody
        }
        confirmLabel={labels.confirmDelete}
        cancelLabel={labels.cancel}
        busy={busy}
        onConfirm={confirmDelete}
        testID="curriculum-delete-confirm"
      />
    </section>
  );
}

function SectionRow({
  section,
  index,
  courseID,
  revisionID,
  busy,
  labels,
  locale,
  draft,
  onLessonDraftChange,
  onAddLesson,
  onRequestDeleteSection,
  onRequestDeleteLesson,
  onContentChanged,
}: {
  section: SectionWire;
  index: number;
  courseID: string;
  revisionID: string;
  busy: boolean;
  labels: CurriculumLabels;
  locale: "ar" | "en";
  draft: LessonDraft;
  onLessonDraftChange: (draft: LessonDraft) => void;
  onAddLesson: (event: React.FormEvent) => void;
  onRequestDeleteSection: () => void;
  onRequestDeleteLesson: (lessonID: string) => void;
  onContentChanged: () => void | Promise<void>;
}) {
  const lessons = section.lessons ?? [];
  const title = locale === "ar" ? section.title_ar : section.title_en;

  return (
    <li
      data-testid={`section-${section.id}`}
      className="rounded-lg border border-border bg-muted/30 p-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
        <h4 className="min-w-0 font-display text-sm font-bold text-foreground">
          {/* The number is generated, the title is authored — the two must not merge in RTL. */}
          <span className="text-muted-foreground">{index + 1}.</span> <bdi>{title}</bdi>
        </h4>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={busy}
          onClick={onRequestDeleteSection}
          data-testid={`delete-section-${section.id}`}
          className="text-destructive hover:bg-destructive/10 hover:text-destructive"
        >
          {labels.deleteSection}
        </Button>
      </div>

      <div className="mt-3 space-y-3 border-s-2 border-border ps-4">
        {lessons.length === 0 ? (
          <p className="text-sm text-muted-foreground" data-testid={`section-empty-${section.id}`}>
            {labels.noLessons}
          </p>
        ) : (
          <ol className="space-y-3">
            {lessons.map((lesson, lessonIndex) => (
              <LessonRow
                key={lesson.id}
                lesson={lesson}
                index={lessonIndex}
                courseID={courseID}
                revisionID={revisionID}
                busy={busy}
                labels={labels}
                locale={locale}
                onRequestDelete={() => onRequestDeleteLesson(lesson.id)}
                onContentChanged={onContentChanged}
              />
            ))}
          </ol>
        )}

        <form
          onSubmit={onAddLesson}
          data-testid={`add-lesson-form-${section.id}`}
          className="grid grid-cols-1 gap-3 pt-1 lg:grid-cols-[1fr_1fr_auto] lg:items-end"
        >
          <Field label={labels.addLessonTitleAr} htmlFor={`lesson-title-ar-${section.id}`}>
            <Input
              id={`lesson-title-ar-${section.id}`}
              lang="ar"
              dir="rtl"
              controlSize="sm"
              value={draft.ar}
              onChange={(event) => onLessonDraftChange({ ar: event.target.value, en: draft.en })}
              data-testid={`lesson-title-ar-${section.id}`}
            />
          </Field>
          <Field label={labels.addLessonTitleEn} htmlFor={`lesson-title-en-${section.id}`}>
            <Input
              id={`lesson-title-en-${section.id}`}
              lang="en"
              dir="ltr"
              controlSize="sm"
              value={draft.en}
              onChange={(event) => onLessonDraftChange({ ar: draft.ar, en: event.target.value })}
              data-testid={`lesson-title-en-${section.id}`}
            />
          </Field>
          <Button
            type="submit"
            variant="outline"
            size="sm"
            disabled={busy}
            data-testid={`add-lesson-${section.id}`}
          >
            {labels.addLesson}
          </Button>
        </form>
      </div>
    </li>
  );
}

function LessonRow({
  lesson,
  index,
  courseID,
  revisionID,
  busy,
  labels,
  locale,
  onRequestDelete,
  onContentChanged,
}: {
  lesson: LessonWire;
  index: number;
  courseID: string;
  revisionID: string;
  busy: boolean;
  labels: CurriculumLabels;
  locale: "ar" | "en";
  onRequestDelete: () => void;
  onContentChanged: () => void | Promise<void>;
}) {
  const hasVideo = Boolean(lesson.video_asset_version_id);
  const labMaterials = (lesson.files ?? []).filter((file) => file.kind === "LAB_MATERIAL");

  return (
    <li
      data-testid={`lesson-${lesson.id}`}
      className="rounded-lg border border-border bg-card p-3"
    >
      <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
        <p className="min-w-0 text-sm font-semibold text-foreground">
          <span className="text-muted-foreground">{index + 1}.</span>{" "}
          <bdi>{locale === "ar" ? lesson.title_ar : lesson.title_en}</bdi>
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {/*
            The state, not the identifier. A dot carries the same information as the old
            colour-only treatment, but the words carry it on their own.
          */}
          <span
            data-testid={
              hasVideo ? `lesson-video-ref-${lesson.id}` : `lesson-video-none-${lesson.id}`
            }
            data-video-attached={hasVideo ? "true" : "false"}
            className={
              /*
                Not the `gx-success` token: at 12px it measures 4.39:1 on this card, under AA. The
                icon carries the distinction visually and the words carry it outright, so the ink
                does not have to.
              */
              hasVideo
                ? "inline-flex items-center gap-1 text-xs font-semibold text-foreground"
                : "inline-flex items-center gap-1 text-xs font-semibold text-muted-foreground"
            }
          >
            {hasVideo ? (
              <Check className="size-3.5 shrink-0" aria-hidden />
            ) : (
              <CircleDashed className="size-3.5 shrink-0" aria-hidden />
            )}
            {hasVideo ? labels.videoAttached : labels.videoMissing}
          </span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={onRequestDelete}
            data-testid={`delete-lesson-${lesson.id}`}
            className="text-destructive hover:bg-destructive/10 hover:text-destructive"
          >
            {labels.deleteLesson}
          </Button>
        </div>
      </div>

      {/* Lab Materials are shown but not editable here: D-088 covers Lesson video and Lesson
          Resources only. The kind is named, not printed as its enum. */}
      {labMaterials.length > 0 ? (
        <div className="mt-2">
          <p className="font-display text-[11px] font-bold uppercase tracking-wide text-muted-foreground">
            {labels.labMaterials}
          </p>
          <ul className="mt-1 flex flex-wrap gap-1.5">
            {labMaterials.map((file) => (
              <li
                key={file.id}
                className="max-w-full truncate rounded-pill bg-muted px-2 py-0.5 text-xs text-muted-foreground"
              >
                <bdi>{locale === "ar" ? file.display_name_ar : file.display_name_en}</bdi>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="mt-3 space-y-2">
        <LessonVideoUpload
          courseID={courseID}
          revisionID={revisionID}
          lessonID={lesson.id}
          locale={locale}
          onAttached={onContentChanged}
        />
        <LessonResourceUpload
          courseID={courseID}
          revisionID={revisionID}
          lessonID={lesson.id}
          locale={locale}
          files={lesson.files ?? []}
          onChanged={onContentChanged}
        />
      </div>
    </li>
  );
}
