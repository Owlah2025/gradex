"use client";

import * as React from "react";
import { SectionStack, StackLayer } from "./section-stack";
import { useLandingJourney } from "./landing-journey";
import { COURSES_ANCHOR } from "./anchors";

/**
 * Hero → Courses, as one continuous surface.
 *
 * The two sections are passed in rather than imported so this stays a layout: it decides the
 * stacking order, carries the anchor id, and hands the journey the element it needs to scroll to.
 * It knows nothing about academic contexts, catalogue filters or copy.
 *
 * The registration is why this is a client component at all. The journey scrolls to a real layer of
 * the stack — the box that carries `scroll-margin-top` — instead of looking it up by selector,
 * which would tie the hand-off to an id spelled somewhere else.
 */
export function LandingStack({
  hero,
  courses,
}: {
  hero: React.ReactNode;
  courses: React.ReactNode;
}) {
  const journey = useLandingJourney();

  return (
    <SectionStack>
      <StackLayer layer={1}>{hero}</StackLayer>
      {/* The last layer carries the stack away: it rises over the hero, then keeps scrolling rather
          than pinning, and the page returns to ordinary flow beneath it. */}
      <StackLayer
        layer={2}
        tail
        id={COURSES_ANCHOR}
        ref={journey?.registerCourses}
        className="rounded-t-xl border-t border-border bg-background shadow-lg"
      >
        {courses}
      </StackLayer>
    </SectionStack>
  );
}
