"use client";

import * as React from "react";
import { ListTree } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { CourseCurriculum } from "./course-curriculum";
import type { CurriculumSection } from "./curriculum-model";
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

function contentsFor({ courseID, locale, sections, currentLessonID, labels }: PanelProps) {
  const { courseOutline: _outline, courseContents: _contents, closeCourseContents: _close, ...curriculum } = labels;
  return (
    <CourseCurriculum
      courseID={courseID}
      locale={locale}
      sections={sections}
      currentLessonID={currentLessonID}
      labels={curriculum}
      headingLevel="h3"
    />
  );
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
        <div onClick={() => setOpen(false)}>{contentsFor(props)}</div>
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
      <div className="mt-3 max-h-[calc(100vh-11rem)] overflow-y-auto pe-1">{contentsFor(props)}</div>
    </nav>
  );
}
