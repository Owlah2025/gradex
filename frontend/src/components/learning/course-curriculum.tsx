"use client";

import * as React from "react";
import Link from "next/link";
import { CheckCircle2, Circle, CirclePlay } from "lucide-react";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import { cn } from "@/lib/utils";
import {
  initiallyOpenSections,
  type CurriculumLesson,
  type CurriculumSection,
} from "./curriculum-model";
import { formatLearningInteger } from "@/lib/formatters/learning";
import type { CurriculumLabels } from "./learning-label-sets";

const stateIcon = {
  completed: CheckCircle2,
  "in-progress": CirclePlay,
  "not-started": Circle,
} as const;

/**
 * The Course's contents, on both the Course page and beside a Lesson.
 *
 * # ONE COMPONENT, TWO PLACES
 *
 * A Student who learns the shape of the outline on the Course page should meet the same shape while
 * they are inside a Lesson. Two components would have drifted immediately: they already disagreed
 * about whether a Lesson row says anything at all about its own state.
 *
 * # WHY DISCLOSURE
 *
 * A Course of three Lessons wants no disclosure and gets none — `initiallyOpenSections` opens every
 * section up to the point where an unbroken column stops being scannable. Past it, the section the
 * Student is actually in opens and the rest stay shut, which is the only arrangement that keeps a
 * forty-Lesson Course usable on a phone. The disclosure itself is the shared Radix accordion, so
 * `aria-expanded`, `aria-controls` and keyboard behaviour are the primitive's rather than
 * reimplemented here.
 *
 * # STATE IS NEVER COLOUR ALONE
 *
 * Each row carries an icon *and* a word: the icon is `aria-hidden` and the state is written out for
 * a screen reader, so "completed" survives both a monochrome rendering and no rendering at all. The
 * Lesson being read is marked with `aria-current="location"` and says "You are here" in words.
 *
 * `location` rather than `page`: the Lesson screen carries a breadcrumb whose last item is the page
 * itself, and that is the canonical `aria-current="page"`. Two elements both announcing "current
 * page" is a worse answer than one saying which page and one saying where in the outline that page
 * sits, which is exactly what these two are.
 */
export function CourseCurriculum({
  courseID,
  locale,
  sections,
  currentLessonID,
  labels,
  headingLevel = "h2",
  materialsByLesson,
  className,
}: {
  courseID: string;
  locale: "ar" | "en";
  sections: CurriculumSection[];
  currentLessonID?: string | null;
  labels: CurriculumLabels;
  headingLevel?: "h2" | "h3";
  /**
   * Per-Lesson material controls, rendered by the server and handed in already built.
   *
   * The downloads are the server's to compose — it holds the authorization paths and decides
   * whether access even permits them — so this component never receives a material, a file name or
   * a path, only the finished subtree to place.
   */
  materialsByLesson?: Record<string, React.ReactNode>;
  className?: string;
}) {
  const [open, setOpen] = React.useState<string[]>(() =>
    initiallyOpenSections(sections, currentLessonID),
  );

  // Moving to another Lesson can move the Student into a different section. Re-opening on that
  // change is what keeps "where I am" visible after a previous/next step in a long Course; it adds
  // to the open set rather than replacing it, so a section the Student opened themselves stays open.
  React.useEffect(() => {
    if (!currentLessonID) return;
    const holder = sections.find((section) =>
      section.lessons.some((lesson) => lesson.lessonID === currentLessonID),
    );
    if (!holder) return;
    setOpen((current) => (current.includes(holder.sectionID) ? current : [...current, holder.sectionID]));
  }, [currentLessonID, sections]);

  if (sections.length === 0) return null;

  return (
    <Accordion
      type="multiple"
      value={open}
      onValueChange={setOpen}
      className={cn("space-y-3", className)}
    >
      {sections.map((section) => (
        <AccordionItem key={section.sectionID} value={section.sectionID}>
          <AccordionTrigger headingLevel={headingLevel} className="py-4">
            <span className="flex min-w-0 flex-col gap-1">
              <span className="min-w-0 break-words">{section.title}</span>
              <span className="font-sans text-xs font-semibold text-muted-foreground">
                {formatLearningInteger(section.completedLessons, locale)}/
                {formatLearningInteger(section.totalLessons, locale)} {labels.completedLessons}
              </span>
            </span>
          </AccordionTrigger>
          <AccordionContent className="pt-0">
            <ol className="space-y-2">
              {section.lessons.map((lesson) => (
                <li key={lesson.lessonID}>
                  <CurriculumRow
                    courseID={courseID}
                    locale={locale}
                    lesson={lesson}
                    current={lesson.lessonID === currentLessonID}
                    labels={labels}
                  />
                  {materialsByLesson?.[lesson.lessonID] ?? null}
                </li>
              ))}
            </ol>
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  );
}

function CurriculumRow({
  courseID,
  locale,
  lesson,
  current,
  labels,
}: {
  courseID: string;
  locale: "ar" | "en";
  lesson: CurriculumLesson;
  current: boolean;
  labels: CurriculumLabels;
}) {
  const Icon = stateIcon[lesson.state];
  const stateText =
    lesson.state === "completed"
      ? labels.completed
      : lesson.state === "in-progress"
        ? labels.lessonInProgress
        : labels.lessonNotStarted;

  return (
    <Link
      href={`/${locale}/learn/courses/${courseID}/lessons/${lesson.lessonID}`}
      aria-current={current ? "location" : undefined}
      // The row's identity and state, readable without parsing localized copy.
      // The Lesson being watched updates here from the Progress write's own
      // response, so a test can prove the outline followed it rather than
      // inferring completion from a word that differs by language.
      data-lesson-id={lesson.lessonID}
      data-lesson-state={lesson.state}
      className={cn(
        "flex items-start gap-3 rounded-md px-3 py-2.5 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        // The current row is marked by weight and a border on the reading edge as well as by tone,
        // and says so in words below — three signals, none of them only colour.
        current && "border-s-2 border-primary bg-accent",
      )}
    >
      <Icon
        aria-hidden
        className={cn(
          "mt-0.5 size-[18px] shrink-0",
          lesson.state === "completed" ? "text-primary" : "text-muted-foreground",
        )}
      />
      <span className="min-w-0 flex-1">
        <span
          className={cn(
            "block break-words text-[15px] text-foreground",
            current ? "font-display font-bold" : "font-medium",
          )}
        >
          {lesson.title}
        </span>
        {/* Spaced, not dot-separated: beside Arabic-Indic digits a middle dot is indistinguishable
            from ٠, and "· ٢ ملفات" read as twenty files rather than two. */}
        <span className="mt-0.5 flex flex-wrap gap-x-2 text-xs text-muted-foreground">
          <span>{stateText}</span>
          {lesson.materialCount > 0 ? (
            <span>
              {formatLearningInteger(lesson.materialCount, locale)} {labels.files}
            </span>
          ) : null}
          {current ? (
            <span className="font-display font-bold text-primary">{labels.currentLessonLabel}</span>
          ) : null}
        </span>
      </span>
    </Link>
  );
}
