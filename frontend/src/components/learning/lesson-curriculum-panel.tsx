"use client";

import * as React from "react";
import { ListTree } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { CourseCurriculum } from "./course-curriculum";
import {
  lessonState,
  type CurriculumLessonState,
  type CurriculumSection,
} from "./curriculum-model";
import { useConfirmedLessonProgress } from "./use-progress-store";
import type { ConfirmedLessonProgress } from "./progress-contract";
import type { CurriculumLabels } from "./learning-label-sets";

export type CurriculumPanelLabels = CurriculumLabels & {
  courseOutline: string;
  courseContents: string;
  closeCourseContents: string;
};

type PanelProps = {
  courseID: string;
  locale: "ar" | "en";
  sections: CurriculumSection[];
  currentLessonID: string;
  labels: CurriculumPanelLabels;
};

function Contents({ courseID, locale, sections, currentLessonID, labels }: PanelProps) {
  const { courseOutline: _outline, courseContents: _contents, closeCourseContents: _close, ...curriculum } = labels;
  // The contents were rendered from the read model the page loaded with, so the
  // Lesson being watched goes stale the moment it is completed. Only that one
  // row can have news — the others are not being watched — so the confirmed
  // state is applied to it and the rest of the list is left exactly as the
  // server described it.
  const live = useConfirmedLessonProgress(currentLessonID, currentLessonProgress(sections, currentLessonID));
  const current = React.useMemo(
    () => withConfirmedLesson(sections, currentLessonID, lessonState(live)),
    [sections, currentLessonID, live],
  );
  return (
    <CourseCurriculum
      courseID={courseID}
      locale={locale}
      sections={current}
      currentLessonID={currentLessonID}
      labels={curriculum}
      headingLevel="h3"
    />
  );
}

/** The rendered state of the Lesson being watched, as the progress shape the store speaks. */
function currentLessonProgress(
  sections: CurriculumSection[],
  currentLessonID: string,
): ConfirmedLessonProgress {
  for (const section of sections) {
    const lesson = section.lessons.find((entry) => entry.lessonID === currentLessonID);
    if (!lesson) continue;
    // The contents carry a state, not a position. Reconstructing the position
    // is not needed and would be a guess; what matters downstream is whether
    // the row is completed, started, or untouched.
    return {
      completed: lesson.state === "completed",
      position_seconds: lesson.state === "not-started" ? 0 : 1,
    };
  }
  return { completed: false, position_seconds: 0 };
}

/**
 * Replaces one Lesson's state and keeps its section's counter honest.
 *
 * Updating the row without the counter would leave "2 of 5" beside three ticks,
 * which is a worse defect than the staleness this fixes.
 */
function withConfirmedLesson(
  sections: CurriculumSection[],
  lessonID: string,
  state: CurriculumLessonState,
): CurriculumSection[] {
  let changed = false;
  const next = sections.map((section) => {
    const index = section.lessons.findIndex((lesson) => lesson.lessonID === lessonID);
    if (index === -1 || section.lessons[index].state === state) return section;
    changed = true;
    const lessons = section.lessons.map((lesson, position) =>
      position === index ? { ...lesson, state } : lesson,
    );
    return {
      ...section,
      lessons,
      completedLessons: lessons.filter((lesson) => lesson.state === "completed").length,
    };
  });
  return changed ? next : sections;
}

/**
 * The Course contents behind one control, for the viewports that have no room for a column.
 *
 * # THE MOBILE PROBLEM THIS SOLVES
 *
 * A two-column player stacked at 390px puts the whole curriculum between the video and everything
 * under it. On a three-Lesson Course that is merely long; on a forty-Lesson one the Student scrolls
 * past the entire Course to reach the previous/next controls. So below `lg` the contents move
 * behind one labelled control and open in the shared `Sheet` — the same primitive the site header
 * already uses for its own menu, so no drawer library enters the project for this.
 *
 * # ONLY ONE COPY IS EVER LIVE
 *
 * This and `CurriculumSidebar` are mutually exclusive by breakpoint, and the sheet mounts its
 * contents only while it is open. A Student on a phone therefore meets exactly one set of Lesson
 * links, and a Student on a desktop never meets the sheet at all.
 */
export function CurriculumSheet(props: PanelProps) {
  const [open, setOpen] = React.useState(false);
  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm" data-testid="open-course-contents" className="w-full">
          <ListTree aria-hidden />
          {props.labels.courseContents}
        </Button>
      </SheetTrigger>
      <SheetContent
        side="right"
        closeLabel={props.labels.closeCourseContents}
        className="w-[92vw] max-w-[420px] overflow-y-auto"
      >
        <SheetTitle className="mb-4 mt-1 pe-8">{props.labels.courseOutline}</SheetTitle>
        {/* Choosing a Lesson navigates away, so the sheet closes on the way out rather than staying
            open behind the next page. */}
        <div onClick={() => setOpen(false)}>{<Contents {...props} />}</div>
      </SheetContent>
    </Sheet>
  );
}

/** The same contents as a standing column, from `lg` up, where there is room for one. */
export function CurriculumSidebar(props: PanelProps) {
  return (
    <nav aria-label={props.labels.courseOutline} data-testid="course-contents-sidebar">
      <h2 className="font-display text-sm font-bold uppercase tracking-wide text-muted-foreground">
        {props.labels.courseOutline}
      </h2>
      {/* The column scrolls on its own so a long Course cannot make the page taller than the
          Lesson it belongs to. */}
      <div className="mt-3 max-h-[calc(100vh-11rem)] overflow-y-auto pe-1">{<Contents {...props} />}</div>
    </nav>
  );
}
