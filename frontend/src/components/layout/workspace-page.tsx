"use client";

import { useId, type ReactNode } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import { cn } from "@/lib/utils";

/**
 * The page frame every role workspace screen sits in.
 *
 * Admin and Instructor screens each invented their own outer container: `max-w-6xl mx-auto p-6`,
 * `max-w-7xl mx-auto px-4 py-8`, `space-y-4 p-4`, and — on the Courses directory — the form this
 * module now owns. Four different content widths and four different gutters meant that moving
 * between two Admin screens visibly shifted the page under the reader, and only some of them set
 * `dir` at all, so an RTL session inherited the document direction inconsistently.
 *
 * This is a frame, not a layout engine. It decides width, gutters and direction; what goes inside
 * remains each screen's own business. Public and Student Learning screens are deliberately not
 * expected to adopt it — an operational workspace and a reading surface want different measures.
 */
export function WorkspacePage({
  children,
  className,
  testID,
}: {
  children: ReactNode;
  className?: string;
  testID?: string;
}) {
  const { dir } = useLocale();
  return (
    <div
      dir={dir}
      data-testid={testID}
      className={cn("mx-auto max-w-container px-5 py-8 sm:px-6", className)}
    >
      {children}
    </div>
  );
}

/**
 * The heading block of a workspace screen: what this screen is, what it is for, and the actions
 * that belong to the screen as a whole rather than to any one row.
 *
 * The heading steps down to `text-2xl` below the `sm` breakpoint. An operational screen is read for
 * its rows, and a 30px title on a 390px viewport pushed the first row below the fold for no gain.
 *
 * `actions` sits on the same line on wide viewports and wraps beneath the title on narrow ones, so
 * the primary action stays reachable without a separate mobile arrangement to keep in step.
 */
export function WorkspacePageHeader({
  title,
  description,
  breadcrumb,
  status,
  actions,
  className,
}: {
  title: string;
  description?: string;
  /** Back link or trail, rendered above the title. */
  breadcrumb?: ReactNode;
  /** Badges or counts describing the screen's current contents, rendered under the description. */
  status?: ReactNode;
  /** Screen-level controls: refresh, create, and similar. */
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <header className={cn("border-b border-border pb-6", className)}>
      {breadcrumb ? <div className="mb-3">{breadcrumb}</div> : null}
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-4">
        <div className="min-w-0 flex-1">
          <h1 className="font-display text-2xl font-bold text-foreground sm:text-3xl">{title}</h1>
          {description ? (
            <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground sm:text-base">
              {description}
            </p>
          ) : null}
          {status ? <div className="mt-3 flex flex-wrap items-center gap-2">{status}</div> : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </header>
  );
}

/**
 * The row that carries a screen's search, filters and refresh.
 *
 * Layout only. What belongs in a toolbar differs enough between screens — the Courses directory
 * searches and filters, the review queue only refreshes — that a configurable filter component
 * would have one real consumer and a lot of speculative surface.
 */
export function WorkspaceToolbar({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("mt-6 flex flex-wrap items-center gap-3", className)}>{children}</div>
  );
}

/**
 * A titled region within a workspace screen.
 *
 * The heading is wired to the region with `aria-labelledby` rather than left as a loose heading, so
 * the section is announced by name when a screen reader user moves between landmarks.
 *
 * `headingLevel` exists because sections nest. A section directly under the page heading is an
 * `h2`; one inside another section's content — the Instructor roster inside a selected Course
 * panel — is an `h3`. Hardcoding `h2` would have produced two headings claiming the same level for
 * a region and the region containing it, which is precisely the structure a screen reader user
 * navigates by.
 */
export function WorkspaceSection({
  title,
  description,
  actions,
  headingLevel: Heading = "h2",
  children,
  className,
  testID,
}: {
  title?: string;
  description?: string;
  actions?: ReactNode;
  headingLevel?: "h2" | "h3";
  children: ReactNode;
  className?: string;
  testID?: string;
}) {
  const headingID = useId();
  return (
    <section
      aria-labelledby={title ? headingID : undefined}
      data-testid={testID}
      className={cn("mt-8", className)}
    >
      {title ? (
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="min-w-0">
            <Heading id={headingID} className="font-display text-lg font-bold text-foreground">
              {title}
            </Heading>
            {description ? (
              <p className="mt-1 text-sm text-muted-foreground">{description}</p>
            ) : null}
          </div>
          {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
        </div>
      ) : null}
      <div className={title ? "mt-4" : undefined}>{children}</div>
    </section>
  );
}
