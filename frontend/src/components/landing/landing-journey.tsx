"use client";

import * as React from "react";

/**
 * The two hand-offs between the landing page's stacked sections.
 *
 * Personalization and the course strip are separate sections with separate jobs, and neither owns
 * the other's state: the academic context itself already lives in `AcademicContextProvider` and is
 * read from there by both. What is missing is only the *choreography* — "the context just resolved,
 * take the reader to the results" and "the reader asked to change it, put the picker back into its
 * question". Passing those through the context provider would put page-level scroll behaviour into
 * a value the catalogue and the profile editor also consume, which is the wrong place for it.
 *
 * So this holds nothing but two DOM references and a counter, is mounted only by the landing page,
 * and is deliberately not a store: no academic data crosses it and no filtering decision is made
 * here.
 */

type LandingJourneyValue = {
  /** Called by the stacked sections so the journey can scroll to them without querying the DOM. */
  registerPersonalization: (node: HTMLElement | null) => void;
  registerCourses: (node: HTMLElement | null) => void;
  /** The context has just resolved: take the reader to the results. */
  scrollToCourses: () => void;
  /**
   * Increments when a downstream surface asks for the picker back.
   *
   * A counter rather than a boolean, because the panel has to react to a *repeated* request. With a
   * flag, asking twice in a row is one state change and the second press does nothing.
   */
  editRequests: number;
  requestEdit: () => void;
};

const LandingJourney = React.createContext<LandingJourneyValue | null>(null);

function prefersReducedMotion(): boolean {
  if (typeof window === "undefined" || !window.matchMedia) return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * Scrolls a stacked section to the header edge.
 *
 * `block: "start"` against the section's own `scroll-margin-top` is what keeps this honest under
 * the sticky stack: the target is a sticky layer, so anything that computed an offset by hand would
 * be measuring a box that is about to move. The browser resolves it against the layout the stack
 * actually produces.
 */
function scrollToSection(node: HTMLElement | null): void {
  if (!node) return;
  node.scrollIntoView({
    behavior: prefersReducedMotion() ? "auto" : "smooth",
    block: "start",
  });
}

export function LandingJourneyProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const personalization = React.useRef<HTMLElement | null>(null);
  const courses = React.useRef<HTMLElement | null>(null);
  const [editRequests, setEditRequests] = React.useState(0);

  const registerPersonalization = React.useCallback((node: HTMLElement | null) => {
    personalization.current = node;
  }, []);
  const registerCourses = React.useCallback((node: HTMLElement | null) => {
    courses.current = node;
  }, []);

  /**
   * Deliberately synchronous, and deliberately not deferred to an animation frame.
   *
   * Both hand-offs have to happen *after* React has committed the state change that caused them —
   * the courses section is being retitled and refiltered by the same update that triggers the
   * scroll — so the timing belongs to an effect at the call site, which is exactly what an effect
   * guarantees. `requestAnimationFrame` looks like the same thing and is not: it does not run in a
   * hidden or non-compositing tab, so the reader could complete the questions and simply never
   * arrive at their results.
   */
  const scrollToCourses = React.useCallback(() => {
    scrollToSection(courses.current);
  }, []);

  const requestEdit = React.useCallback(() => {
    setEditRequests((count) => count + 1);
  }, []);

  // The counter is the request; this is the response to it. Scrolling from inside `requestEdit`
  // would run before the panel has been told to reopen its questions.
  React.useEffect(() => {
    if (editRequests === 0) return;
    scrollToSection(personalization.current);
  }, [editRequests]);

  const value = React.useMemo<LandingJourneyValue>(
    () => ({
      registerPersonalization,
      registerCourses,
      scrollToCourses,
      editRequests,
      requestEdit,
    }),
    [registerPersonalization, registerCourses, scrollToCourses, editRequests, requestEdit],
  );

  return <LandingJourney.Provider value={value}>{children}</LandingJourney.Provider>;
}

/**
 * The journey, where one is mounted.
 *
 * Returns `null` off the landing page rather than throwing. `FeaturedCourses` and the academic
 * panel are both mounted elsewhere in the product, and a section that works on one page and crashes
 * on another is a worse contract than one that simply has no choreography to perform.
 */
export function useLandingJourney(): LandingJourneyValue | null {
  return React.useContext(LandingJourney);
}
