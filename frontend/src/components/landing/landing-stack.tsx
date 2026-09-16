"use client";

import * as React from "react";
import { SectionStack, StackLayer } from "./section-stack";
import { COURSES_ANCHOR } from "./anchors";
import { HeroCourseDivider } from "./hero-course-divider";

/**
 * Hero → Courses, as one continuous surface.
 *
 * The two sections are passed in rather than imported so this stays a layout: it decides the
 * stacking order and carries the anchor id. The study-plan section inside this stack registers the
 * precise post-selection scroll target with the landing journey.
 */
export function LandingStack({
  hero,
  courses,
}: {
  hero: React.ReactNode;
  courses: React.ReactNode;
}) {
  return (
    <SectionStack>
      <StackLayer layer={1}>{hero}</StackLayer>
      {/* The last layer carries the stack away: it rises over the hero, then keeps scrolling rather
          than pinning, and the page returns to ordinary flow beneath it. */}
      <StackLayer
        layer={2}
        tail
        id={COURSES_ANCHOR}
        className="bg-background"
      >
        <HeroCourseDivider />
        {courses}
      </StackLayer>
    </SectionStack>
  );
}
