"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { withReturnTo } from "@/lib/identity/return-to";
import type { SubjectDemandAudience } from "@/lib/identity/subject-demand-authority";
import { currentCSRFToken } from "@/lib/identity/session";
import {
  alreadyRequested,
  raiseSubjectDemand,
  subjectInvalid,
  subjectMissing,
  withdrawSubjectDemand,
} from "@/lib/api/subject-catalogue";
import type { SubjectCopy } from "./subject-copy";

/**
 * "Request this course", and everything that follows from pressing it.
 *
 * Modelled on PurchaseAction, and for the same reason: an anonymous visitor who
 * presses a call to action must not lose it to the sign-in journey. The intent
 * travels in the URL, so registering, verifying, and signing in all return to
 * this Subject with the request panel already open.
 *
 * # WHAT THIS DOES NOT DO
 *
 * It records that a Student wants a Subject taught. It does not buy anything,
 * reserve anything, grant anything, or create a Course. The copy says so
 * plainly, because a control that looks like a purchase and is not is worse
 * than no control.
 *
 * # IDENTIFIERS
 *
 * Demand is raised with `subjectId` — the canonical identifier discovery
 * returned — never the official code. The code is what the URL carries and what
 * the Student reads; the server refuses it here.
 */

/**
 * The query flag that says "this visitor came here to request".
 *
 * Not a secret and grants nothing: it selects which state this panel opens in.
 * Putting it in the URL is what lets the intent survive sign in, registration,
 * verification, a reload, and the back button, without any of those steps
 * carrying state they should not hold.
 */
export const demandIntentParameter = "request";

type DemandState = "idle" | "requested";

export function SubjectDemandAction({
  subjectId,
  copy,
  locale,
  audience,
  initiallyRequested,
  onChange,
  className,
  compact = false,
}: {
  subjectId: string;
  copy: SubjectCopy;
  locale: "ar" | "en";
  /**
   * What this visitor may do, derived from the session authority model rather
   * than from "is there a session". See subject-demand-authority.
   */
  audience: SubjectDemandAudience;
  /** Whether this Student already holds a live signal for the Subject. */
  initiallyRequested: boolean;
  /** Lets a parent list keep its own record of what is requested. */
  onChange?: (subjectId: string, requested: boolean) => void;
  className?: string;
  /** Card rendering: the button only, no explanatory panel or note field. */
  compact?: boolean;
}) {
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const [state, setState] = React.useState<DemandState>(
    initiallyRequested ? "requested" : "idle",
  );
  const [note, setNote] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);
  // `busy` is state and does not close the window between two presses
  // dispatched in the same render pass. This does.
  const inFlight = React.useRef(false);

  React.useEffect(() => {
    setState(initiallyRequested ? "requested" : "idle");
  }, [initiallyRequested, subjectId]);

  const intended = searchParams.get(demandIntentParameter) === "1";
  const [open, setOpen] = React.useState(intended && !compact);
  React.useEffect(() => {
    if (intended && !compact) setOpen(true);
  }, [intended, compact]);

  /** Where the auth journey must return to: this Subject, still requesting. */
  const destination = React.useMemo(() => {
    const params = new URLSearchParams(searchParams.toString());
    params.set(demandIntentParameter, "1");
    return `${pathname ?? ""}?${params.toString()}`;
  }, [pathname, searchParams]);

  function describe(caught: unknown): string {
    // A conflict is not a failure. The Student's intent is already recorded, so
    // the honest response is to show the requested state rather than an error.
    if (alreadyRequested(caught)) return "";
    if (subjectMissing(caught)) return copy.subjectGone;
    // Unreachable from this surface, because subjectId came from discovery. If
    // it ever happens it is our bug, and the Student can do nothing about it.
    if (subjectInvalid(caught)) return copy.unexpected;
    return copy.requestFailed;
  }

  async function request() {
    if (inFlight.current) return;
    if (note.length > 500) {
      setError(copy.noteTooLong);
      return;
    }
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      await raiseSubjectDemand({
        locale,
        csrf: currentCSRFToken() ?? "",
        subjectId,
        note: note.trim(),
      });
      setState("requested");
      onChange?.(subjectId, true);
    } catch (caught) {
      const message = describe(caught);
      if (message === "") {
        // Already recorded: converge on the truth rather than reporting it.
        setState("requested");
        onChange?.(subjectId, true);
      } else {
        setError(message);
      }
    } finally {
      setBusy(false);
      inFlight.current = false;
    }
  }

  async function withdraw() {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      await withdrawSubjectDemand({
        locale,
        csrf: currentCSRFToken() ?? "",
        subjectId,
      });
      setState("idle");
      setNote("");
      onChange?.(subjectId, false);
    } catch (caught) {
      // A 404 means there is nothing live to withdraw, which is the state the
      // Student is asking for. Converge instead of reporting a failure.
      if (subjectMissing(caught)) {
        setState("idle");
        onChange?.(subjectId, false);
      } else {
        setError(copy.withdrawFailed);
      }
    } finally {
      setBusy(false);
      inFlight.current = false;
    }
  }

  // UNRESOLVED renders the same shape as the actionable state but inert, so the
  // card does not resize and does not flash "Sign in to request" at a Student
  // who is already signed in. It is never a usable control.
  if (audience === "UNRESOLVED") {
    return (
      <div className={compact ? actionRegionClassName(className) : undefined}>
        <Button
          type="button"
          className={compact ? "w-full" : (className ?? "mt-6")}
          disabled
          aria-hidden
          data-testid="subject-demand-pending"
        >
          {copy.requestCourse}
        </Button>
      </div>
    );
  }

  // Signed in and the server would refuse: an Instructor, an Admin, a
  // restricted principal, or a browser that has not completed device trust.
  // Stating why beats rendering a button that 403s.
  if (audience === "INELIGIBLE") {
    return (
      <div
        className={compact ? actionRegionClassName(className) : (className ?? "mt-6")}
        data-testid="subject-demand-ineligible"
      >
        <p className="flex min-h-11 items-center text-sm leading-6 text-muted-foreground">
          {copy.ineligible}
        </p>
      </div>
    );
  }

  if (audience === "ANONYMOUS") {
    const signInHref = withReturnTo("/login", destination);
    if (compact) {
      return (
        <Button asChild variant="outline" className={className} data-testid="subject-demand-sign-in">
          <Link href={signInHref}>{copy.requestCourse}</Link>
        </Button>
      );
    }
    return (
      <section
        className={panelClassName(className)}
        aria-labelledby="subject-demand-sign-in-heading"
        data-testid="subject-demand-sign-in-required"
      >
        <h2
          id="subject-demand-sign-in-heading"
          className="font-display text-lg font-bold text-foreground"
        >
          {copy.signInToRequest}
        </h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">{copy.requestIntro}</p>
        <div className="mt-4">
          <Button asChild data-testid="subject-demand-sign-in">
            <Link href={signInHref}>{copy.signIn}</Link>
          </Button>
        </div>
      </section>
    );
  }

  if (state === "requested") {
    return (
      <section
        className={compact ? actionRegionClassName(className) : panelClassName(className)}
        data-testid="subject-demand-requested"
      >
        {compact ? null : (
          <p className="text-sm leading-6 text-muted-foreground">{copy.requestedIntro}</p>
        )}
        {/* One row of exactly the same height the Request button occupies, so a
            card does not grow or shrink when the Student requests or withdraws.
            The confirmation and the withdraw control share that row rather than
            stacking below it. */}
        <div
          className={
            compact
              ? "flex min-h-11 items-center justify-between gap-2 rounded-md border border-border px-3"
              : "mt-4 flex flex-wrap items-center gap-3"
          }
        >
          <span
            className={
              compact
                ? "text-sm font-bold text-foreground"
                : "inline-flex min-h-11 items-center rounded-md bg-secondary px-3 text-sm font-bold text-secondary-foreground"
            }
            data-testid="subject-demand-state"
          >
            {copy.requested}
          </span>
          <Button
            type="button"
            variant={compact ? "ghost" : "outline"}
            size={compact ? "sm" : undefined}
            onClick={withdraw}
            disabled={busy}
            data-testid="subject-demand-withdraw"
          >
            {busy ? copy.withdrawing : copy.withdraw}
          </Button>
        </div>
        {error ? (
          <div className="mt-3">
            <Alert tone="error" title={error} />
          </div>
        ) : null}
      </section>
    );
  }

  if (compact || !open) {
    return (
      <div className={compact ? actionRegionClassName(className) : undefined}>
        <Button
          type="button"
          className={compact ? "min-h-11 w-full" : (className ?? "mt-6")}
          onClick={compact ? request : () => setOpen(true)}
          disabled={busy}
          data-testid="subject-demand-request"
        >
          {busy ? copy.requesting : copy.requestCourse}
        </Button>
        {error ? (
          <div className="mt-3">
            <Alert tone="error" title={error} />
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <section
      className={panelClassName(className)}
      aria-labelledby="subject-demand-heading"
      data-testid="subject-demand-panel"
    >
      <h2
        id="subject-demand-heading"
        className="font-display text-lg font-bold text-foreground"
      >
        {copy.requestCourse}
      </h2>
      <p className="mt-2 text-sm leading-6 text-muted-foreground">{copy.requestIntro}</p>

      <label
        htmlFor="subject-demand-note"
        className="mt-4 block text-sm font-semibold text-foreground"
      >
        {copy.noteLabel}
      </label>
      <Textarea
        id="subject-demand-note"
        className="mt-2"
        rows={3}
        maxLength={500}
        value={note}
        placeholder={copy.notePlaceholder}
        onChange={(event) => setNote(event.target.value)}
        data-testid="subject-demand-note"
      />

      {error ? (
        <div className="mt-3">
          <Alert tone="error" title={error} />
        </div>
      ) : null}

      <div className="mt-4">
        <Button
          type="button"
          onClick={request}
          disabled={busy}
          data-testid="subject-demand-submit"
        >
          {busy ? copy.requesting : copy.requestCourse}
        </Button>
      </div>
    </section>
  );
}

/**
 * The card's action region.
 *
 * Every compact state renders into a region of the same height, so a Subject
 * card does not jump when the Student requests or withdraws and a grid of cards
 * does not reflow around one of them.
 */
function actionRegionClassName(className?: string): string {
  return ["min-h-11", className ?? ""].join(" ").trim();
}

function panelClassName(className?: string): string {
  return [
    "rounded-xl border border-border bg-card p-5 text-card-foreground shadow-sm",
    className ?? "mt-6",
  ].join(" ");
}
