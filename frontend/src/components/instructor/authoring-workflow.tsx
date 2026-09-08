"use client";

import * as React from "react";
import { Check, CircleDashed, Loader2, TriangleAlert } from "lucide-react";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@/components/ui/accordion";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import { cn } from "@/lib/utils";
import {
  sectionAfter,
  type AuthoringPlan,
  type AuthoringSectionKey,
  type AuthoringSectionPlan,
  type AuthoringSectionState,
} from "./authoring-plan";

type AuthoringLabels = Dictionary["instructor"]["authoring"];

/**
 * A request to move on, raised by a section that has just completed a real action.
 *
 * It carries a token rather than being a bare key because the same section may progress twice in a
 * row — save, edit, save again — and the shell has to treat the second one as a new event. The
 * token is the only thing that changes then.
 */
export type AuthoringAdvance = { token: number; from: AuthoringSectionKey };

/**
 * The authoring studio's disclosure workflow.
 *
 * The rules it exists to enforce, in the order they were asked for:
 *
 *  - Sections open and close, and more than one may be open. This is not a wizard. Nothing is
 *    locked, nothing is ordered by force, and a finished section can be reopened by clicking it —
 *    an instructor correcting a title on a course whose curriculum is already built must not have
 *    to walk back through four steps to reach it.
 *
 *  - On arrival, the sections that are already finished are closed and the first unfinished one is
 *    open. That is the whole reason the plan is derived from server state: the studio can only
 *    close a section on the reader's behalf if "finished" means something the reader would agree
 *    with.
 *
 *  - A section closes itself only after an intentional, successful progression — the `advance`
 *    prop, raised by a Save-and-continue that the server accepted, or by an explicit Continue.
 *    Never on blur, never on a field change, never because a background refresh recomputed the
 *    plan. Accordions that move while someone is typing are the reason this is spelled out.
 *
 *  - A closed section still says what is outstanding inside it. Hiding a validation problem behind
 *    a collapsed header is the failure mode this pattern invites, so the header carries the count.
 */
export function AuthoringWorkflow({
  plan,
  labels,
  curriculumLabels,
  children,
  courseID,
}: {
  plan: AuthoringPlan;
  labels: AuthoringLabels;
  curriculumLabels: Dictionary["instructor"]["curriculum"];
  /** One node per section key. A key with no node is not rendered. */
  children: Partial<Record<AuthoringSectionKey, React.ReactNode>>;
  /** Selecting a different course starts the workflow again from that course's own state. */
  courseID: string;
}) {
  const [open, setOpen] = React.useState<AuthoringSectionKey[]>([plan.nextSection]);

  // The arrival state, recomputed only when the course changes. Deliberately not keyed on the plan:
  // the plan changes on every save, and reopening the accordion from it would move the disclosure
  // under the instructor's hands every time the studio re-read the course.
  const arrival = React.useRef<string | null>(null);
  React.useEffect(() => {
    if (arrival.current === courseID) return;
    arrival.current = courseID;
    setOpen([plan.nextSection]);
  }, [courseID, plan.nextSection]);

  const advance = React.useCallback(
    (from: AuthoringSectionKey) => {
      const next = sectionAfter(plan, from);
      setOpen((current) => {
        const withoutSource = current.filter((key) => key !== from);
        return withoutSource.includes(next) ? withoutSource : [...withoutSource, next];
      });
    },
    [plan],
  );

  return (
    <AuthoringAdvanceContext.Provider value={advance}>
      <div className="space-y-4" data-testid="authoring-workflow">
        <AuthoringProgress plan={plan} labels={labels} />
        <Accordion
          type="multiple"
          value={open}
          onValueChange={(value) => setOpen(value as AuthoringSectionKey[])}
          className="space-y-3"
        >
          {plan.sections.map((section) => {
            const node = children[section.key];
            if (!node) return null;
            return (
              <AccordionItem
                key={section.key}
                value={section.key}
                data-testid={`authoring-section-${section.key}`}
                data-state-name={section.state}
              >
                <AccordionTrigger
                  headingLevel="h3"
                  data-testid={`authoring-toggle-${section.key}`}
                >
                  <span className="flex min-w-0 flex-1 flex-col gap-1">
                    <span className="flex min-w-0 items-center gap-2.5">
                      <SectionMarker state={section.state} />
                      <span className="min-w-0 truncate">
                        {labels.section[section.key].title}
                      </span>
                    </span>
                    <SectionSubline
                      section={section}
                      plan={plan}
                      labels={labels}
                      curriculumLabels={curriculumLabels}
                    />
                  </span>
                </AccordionTrigger>
                <AccordionContent className="text-foreground">
                  <p className="mb-4 text-sm leading-6 text-muted-foreground">
                    {labels.section[section.key].lead}
                  </p>
                  {node}
                </AccordionContent>
              </AccordionItem>
            );
          })}
        </Accordion>
      </div>
    </AuthoringAdvanceContext.Provider>
  );
}

const AuthoringAdvanceContext = React.createContext<
  ((from: AuthoringSectionKey) => void) | null
>(null);

/**
 * The progression hook a section calls once its own action has actually succeeded.
 *
 * It is a hook rather than a prop threaded through five components because the call site is always
 * the innermost one — the handler that just heard the server say yes — and passing a callback down
 * to each of them would make it easy to fire from somewhere that had not.
 */
export function useAuthoringAdvance(): (from: AuthoringSectionKey) => void {
  const advance = React.useContext(AuthoringAdvanceContext);
  return React.useCallback(
    (from: AuthoringSectionKey) => advance?.(from),
    [advance],
  );
}

/**
 * The explicit "I am done with this part" control.
 *
 * It saves nothing and validates nothing — the panel above it already owns whatever writing there
 * was to do — so it is deliberately a secondary control. Its only job is to be the intentional act
 * that a collapse is allowed to follow.
 */
export function AuthoringContinue({
  section,
  label,
}: {
  section: AuthoringSectionKey;
  label: string;
}) {
  const advance = useAuthoringAdvance();
  return (
    <button
      type="button"
      onClick={() => advance(section)}
      data-testid={`authoring-continue-${section}`}
      className="inline-flex h-9 items-center rounded-md border border-border px-4 text-sm font-semibold text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
    >
      {label}
    </button>
  );
}

/**
 * Bridges a progression that completes outside this subtree.
 *
 * The basics form's save is issued by the studio, not by the disclosure, so the disclosure cannot
 * watch the promise. The studio increments a token when the server accepts, and this advances once
 * per increment. The initial render is deliberately not an advance: mounting is not progress.
 */
export function AuthoringAdvanceOn({
  section,
  token,
}: {
  section: AuthoringSectionKey;
  token: number;
}) {
  const advance = useAuthoringAdvance();
  const seen = React.useRef(token);
  React.useEffect(() => {
    if (seen.current === token) return;
    seen.current = token;
    advance(section);
  }, [advance, section, token]);
  return null;
}

function SectionMarker({ state }: { state: AuthoringSectionState }) {
  // Never colour alone: each marker is an icon whose shape differs, and the header text beside it
  // says the same thing in words.
  if (state === "COMPLETE") {
    return <Check className="size-4 shrink-0 text-gx-success" aria-hidden />;
  }
  if (state === "ATTENTION") {
    return <TriangleAlert className="size-4 shrink-0 text-destructive" aria-hidden />;
  }
  if (state === "PROCESSING") {
    return <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" aria-hidden />;
  }
  // `OPTIONAL` and `INCOMPLETE` share a marker deliberately: neither is a problem, and the words
  // beside it already say which of the two this is.
  return <CircleDashed className="size-4 shrink-0 text-muted-foreground" aria-hidden />;
}

function SectionSubline({
  section,
  plan,
  labels,
  curriculumLabels,
}: {
  section: AuthoringSectionPlan;
  plan: AuthoringPlan;
  labels: AuthoringLabels;
  curriculumLabels: Dictionary["instructor"]["curriculum"];
}) {
  const parts: string[] = [];

  if (section.key === "CURRICULUM") {
    parts.push(`${curriculumLabels.sectionCount}: ${plan.sectionCount}`);
    parts.push(`${curriculumLabels.lessonCount}: ${plan.lessonCount}`);
  }

  if (section.state === "ATTENTION") {
    parts.push(labels.stateAttention);
  } else if (section.outstanding.length > 0) {
    // The count travels with the closed header on purpose: a collapsed section that is quietly
    // holding two unmet requirements is the one thing this pattern must never do.
    parts.push(`${labels.outstanding}: ${section.outstanding.length}`);
  } else if (section.state === "PROCESSING") {
    // Attached and still being worked on by the server. Said plainly, because it is real, and not
    // counted as something the instructor has to do, because it is not.
    parts.push(labels.stateProcessing);
  } else if (section.state === "OPTIONAL") {
    // Nothing attached and the server asks for nothing. Naming it "not finished" would invent a
    // requirement the product does not have.
    parts.push(labels.stateOptional);
  } else if (section.state === "COMPLETE") {
    parts.push(labels.stateComplete);
  } else {
    parts.push(labels.stateIncomplete);
  }

  return (
    <span
      className={cn(
        "text-xs font-semibold",
        section.state === "ATTENTION" ? "text-destructive" : "text-muted-foreground",
      )}
      data-testid={`authoring-subline-${section.key}`}
    >
      {parts.join(" · ")}
    </span>
  );
}

/**
 * The overview above the disclosures.
 *
 * A count and a bar, not a second navigation. The accordion below is the workflow; duplicating it
 * as a clickable step list would give the same five things two homes and make it ambiguous which
 * one is the real one.
 */
function AuthoringProgress({
  plan,
  labels,
}: {
  plan: AuthoringPlan;
  labels: AuthoringLabels;
}) {
  const percent = plan.totalCount === 0 ? 0 : (plan.completeCount / plan.totalCount) * 100;
  return (
    <div
      className="rounded-lg border border-border bg-card px-5 py-4"
      data-testid="authoring-progress"
      data-complete={plan.completeCount}
      data-total={plan.totalCount}
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <p className="font-display text-base font-bold text-foreground">{labels.progressTitle}</p>
        <p className="text-sm font-semibold text-muted-foreground">
          {plan.completeCount}/{plan.totalCount} {labels.progressComplete}
        </p>
      </div>
      <div
        className="mt-3 h-1.5 w-full overflow-hidden rounded-pill bg-muted"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={plan.totalCount}
        aria-valuenow={plan.completeCount}
        aria-label={labels.progressTitle}
      >
        {/*
          Width, not a physical offset. A block child fills from its container's start edge, which
          under an Arabic page is the right — so the bar grows the correct way in both directions
          with no locale branch and no left/right anywhere.
        */}
        <div
          className="h-full rounded-pill bg-primary transition-[width] duration-base ease-out-brand"
          style={{ width: `${percent}%` }}
        />
      </div>
      <p className="mt-2 text-xs leading-5 text-muted-foreground">{labels.progressLead}</p>
    </div>
  );
}
