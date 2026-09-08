"use client";

import React, { useState } from "react";
import {
  closestCenter,
  DndContext,
  DragOverlay,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Check, CircleDashed, GripVertical } from "lucide-react";
import { useLocale } from "@/lib/i18n/locale-provider";
import type { CourseRevisionWire, LessonWire, SectionWire } from "@/lib/api/catalog";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { EmptyState } from "@/components/common/empty-state";
import { LessonVideoUpload } from "./lesson-video-upload";
import { isLessonVideoProcessing } from "./lesson-video-upload-state";
import { LessonResourceUpload } from "./lesson-resource-upload";
import { moveIdentity } from "./curriculum-order";

type CurriculumLabels = Dictionary["instructor"]["curriculum"];

function sortableAnnouncements(
  items: { id: string; title: string }[],
  labels: CurriculumLabels,
): Announcements {
  const titleFor = (id: string) => items.find((item) => item.id === id)?.title ?? id;
  const positionFor = (id: string | number) => items.findIndex((item) => item.id === String(id)) + 1;
  return {
    onDragStart: ({ active }) => labels.dragPicked
      .replace("{title}", titleFor(String(active.id))),
    onDragOver: ({ active, over }) => over
      ? labels.dragMoved
          .replace("{title}", titleFor(String(active.id)))
          .replace("{position}", String(positionFor(over.id)))
      : undefined,
    onDragEnd: ({ active, over }) => over
      ? labels.dragDropped
          .replace("{title}", titleFor(String(active.id)))
          .replace("{position}", String(positionFor(over.id)))
      : labels.dragCancelled.replace("{title}", titleFor(String(active.id))),
    onDragCancel: ({ active }) => labels.dragCancelled
      .replace("{title}", titleFor(String(active.id))),
  };
}

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
  orderState,
  orderError,
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
  onReorderSections,
  onReorderLessons,
  onContentChanged,
}: {
  revision: CourseRevisionWire;
  courseID: string;
  busy: boolean;
  orderState: "IDLE" | "SAVING" | "SAVED" | "FAILED";
  orderError: string | null;
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
  onReorderSections: (sectionIDs: string[], movedSectionID: string) => void | Promise<void>;
  onReorderLessons: (sectionID: string, lessonIDs: string[], movedLessonID: string) => void | Promise<void>;
  onContentChanged: () => void | Promise<void>;
}) {
  const { locale } = useLocale();
  const sections = revision.sections ?? [];
  const [activeSectionID, setActiveSectionID] = useState<string | null>(null);
  const sectionAnnouncements = sortableAnnouncements(
    sections.map((section) => ({
      id: section.id,
      title: locale === "ar" ? section.title_ar : section.title_en,
    })),
    labels,
  );
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 200, tolerance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const finishSectionDrag = ({ active, over }: DragEndEvent) => {
    setActiveSectionID(null);
    if (busy || !over || active.id === over.id) return;
    const next = moveIdentity(sections.map((section) => section.id), String(active.id), String(over.id));
    void onReorderSections(next, String(active.id));
  };

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
        {orderState !== "IDLE" ? (
          <p
            className={orderState === "FAILED" ? "mt-2 text-sm text-destructive" : "mt-2 text-sm text-muted-foreground"}
            data-testid="curriculum-order-state"
            role={orderState === "FAILED" ? "alert" : "status"}
            aria-live="polite"
          >
            {orderState === "SAVING"
              ? labels.orderSaving
              : orderState === "SAVED"
                ? labels.orderSaved
                : `${labels.orderFailed}${orderError ? ` ${orderError}` : ""}`}
          </p>
        ) : null}
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
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragStart={({ active }) => setActiveSectionID(String(active.id))}
          onDragCancel={() => setActiveSectionID(null)}
          onDragEnd={finishSectionDrag}
          accessibility={{
            screenReaderInstructions: { draggable: labels.dragInstructions },
            announcements: sectionAnnouncements,
          }}
        >
          <SortableContext items={sections.map((section) => section.id)} strategy={verticalListSortingStrategy}>
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
              onReorderLessons={onReorderLessons}
              onContentChanged={onContentChanged}
            />
              ))}
            </ol>
          </SortableContext>
          <DragOverlay>
            {activeSectionID ? (
              <div className="rounded-lg border border-primary/40 bg-card px-4 py-3 shadow-lg">
                <span className="text-sm font-semibold text-foreground">
                  <bdi>{locale === "ar" ? sections.find((item) => item.id === activeSectionID)?.title_ar : sections.find((item) => item.id === activeSectionID)?.title_en}</bdi>
                </span>
              </div>
            ) : null}
          </DragOverlay>
        </DndContext>
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
  onReorderLessons,
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
  onReorderLessons: (sectionID: string, lessonIDs: string[], movedLessonID: string) => void | Promise<void>;
  onContentChanged: () => void | Promise<void>;
}) {
  const lessons = section.lessons ?? [];
  const title = locale === "ar" ? section.title_ar : section.title_en;
  const [activeLessonID, setActiveLessonID] = useState<string | null>(null);
  const lessonAnnouncements = sortableAnnouncements(
    lessons.map((lesson) => ({
      id: lesson.id,
      title: locale === "ar" ? lesson.title_ar : lesson.title_en,
    })),
    labels,
  );
  const lessonSensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 200, tolerance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: section.id,
    disabled: busy,
  });
  const finishLessonDrag = ({ active, over }: DragEndEvent) => {
    setActiveLessonID(null);
    if (busy || !over || active.id === over.id) return;
    const next = moveIdentity(lessons.map((lesson) => lesson.id), String(active.id), String(over.id));
    void onReorderLessons(section.id, next, String(active.id));
  };

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      data-testid={`section-${section.id}`}
      className={`rounded-lg border border-border bg-muted/30 p-4 ${isDragging ? "relative z-10 opacity-40" : ""}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
        <div className="flex min-w-0 items-start gap-2">
          <button
            id={`section-drag-handle-${section.id}`}
            type="button"
            {...attributes}
            {...listeners}
            disabled={busy}
            aria-label={labels.reorderSection.replace("{title}", title)}
            data-testid={`section-drag-handle-${section.id}`}
            className="mt-0.5 inline-flex size-8 shrink-0 touch-none items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 cursor-grab active:cursor-grabbing"
          >
            <GripVertical className="size-4" aria-hidden />
          </button>
          <h4 className="min-w-0 pt-1.5 font-display text-sm font-bold text-foreground">
          {/* The number is generated, the title is authored — the two must not merge in RTL. */}
          <span className="text-muted-foreground">{index + 1}.</span> <bdi>{title}</bdi>
        </h4>
        </div>
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
          <DndContext
            sensors={lessonSensors}
            collisionDetection={closestCenter}
            onDragStart={({ active }) => setActiveLessonID(String(active.id))}
            onDragCancel={() => setActiveLessonID(null)}
            onDragEnd={finishLessonDrag}
            accessibility={{
              screenReaderInstructions: { draggable: labels.dragInstructions },
              announcements: lessonAnnouncements,
            }}
          >
            <SortableContext items={lessons.map((lesson) => lesson.id)} strategy={verticalListSortingStrategy}>
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
            </SortableContext>
            <DragOverlay>
              {activeLessonID ? (
                <div className="rounded-lg border border-primary/40 bg-card px-3 py-2 shadow-lg">
                  <span className="text-sm font-semibold text-foreground">
                    <bdi>{locale === "ar" ? lessons.find((item) => item.id === activeLessonID)?.title_ar : lessons.find((item) => item.id === activeLessonID)?.title_en}</bdi>
                  </span>
                </div>
              ) : null}
            </DragOverlay>
          </DndContext>
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
  const videoProcessing = isLessonVideoProcessing(
    lesson.video_asset_version_id,
    lesson.video_asset_state,
  );
  const hasVideo = Boolean(lesson.video_asset_version_id) &&
    !videoProcessing &&
    (!lesson.video_asset_state || lesson.video_asset_state === "READY");
  const labMaterials = (lesson.files ?? []).filter((file) => file.kind === "LAB_MATERIAL");
  const title = locale === "ar" ? lesson.title_ar : lesson.title_en;
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: lesson.id,
    disabled: busy,
  });

  return (
    <li
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      data-testid={`lesson-${lesson.id}`}
      className={`rounded-lg border border-border bg-card p-3 ${isDragging ? "relative z-10 opacity-40" : ""}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
        <div className="flex min-w-0 items-start gap-2">
          <button
            id={`lesson-drag-handle-${lesson.id}`}
            type="button"
            {...attributes}
            {...listeners}
            disabled={busy}
            aria-label={labels.reorderLesson.replace("{title}", title)}
            data-testid={`lesson-drag-handle-${lesson.id}`}
            className="inline-flex size-8 shrink-0 touch-none items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50 cursor-grab active:cursor-grabbing"
          >
            <GripVertical className="size-4" aria-hidden />
          </button>
          <p className="min-w-0 pt-1.5 text-sm font-semibold text-foreground">
          <span className="text-muted-foreground">{index + 1}.</span>{" "}
          <bdi>{title}</bdi>
        </p>
        </div>
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
                Ink, not colour. This is "a video is attached" and "one is not" — a completeness
                fact, not a success — and the icon plus the words already carry it, so nothing is
                gained by tinting it. (The success token's own AA failure, which is what first ruled
                green out here, has since been fixed by splitting it; this stayed ink on merit.)
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
            {hasVideo
              ? labels.videoAttached
              : videoProcessing
                ? labels.videoProcessing
                : labels.videoMissing}
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
          assetVersionID={lesson.video_asset_version_id}
          assetState={lesson.video_asset_state}
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
