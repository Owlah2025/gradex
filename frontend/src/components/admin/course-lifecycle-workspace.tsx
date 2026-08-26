"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import type { Dictionary } from "@/lib/i18n/dictionaries/en";
import {
  archiveCourse,
  delistCourse,
  getCourseLifecycleDirectory,
  relistCourse,
  restoreCourseAccess,
  retireCourse,
  suspendCourseAccess,
  type CourseLifecycleSummary,
  type SuspensionCause,
} from "@/lib/api/catalog";
import { ProblemError } from "@/lib/api/problem";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import {
  WorkspacePage,
  WorkspacePageHeader,
  WorkspaceSection,
  WorkspaceToolbar,
} from "@/components/layout/workspace-page";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
} from "@/components/ui/table";
import { courseStatusView } from "./course-status";

type LifecycleCopy = Dictionary["adminLifecycle"];

const SUSPENSION_CAUSES: SuspensionCause[] = [
  "LEGAL",
  "SECURITY",
  "MALWARE",
  "SEVERE_MODERATION",
];

/**
 * AD-12 — the Admin Course lifecycle surface.
 *
 * The lifecycle commands already existed as routes and as an API client; what did not exist was a
 * screen that mounts them, so no Admin could reach delist, relist, retire, archive, suspension or
 * restoration through the product. This workspace is that screen, and it is deliberately built on
 * the Admin lifecycle directory rather than the public catalogue: a delisted, retired or archived
 * Course is invisible publicly by design, and relisting it has to stay possible.
 *
 * The screen holds no lifecycle policy of its own. Every button issues the canonical request and
 * then refetches the directory; what it renders afterwards is the server's state, never an
 * optimistic guess. A refused transition renders the server's refusal.
 *
 * It is also where the Courses directory sends an Admin who presses "Manage", which is why its
 * presentation matters: arriving here from a screen built on the design system and landing on six
 * differently-coloured buttons in an unframed page read as two different products. The six commands
 * are now grouped by what they do to the Course and each group says, in words, what its buttons
 * mean — the amber/emerald/slate/rose palette they used to be told apart by said nothing to a
 * reader who cannot separate those hues, and nothing at all to a screen reader.
 *
 * What has deliberately *not* changed is that these commands fire immediately. Archival is terminal
 * and suspension denies every student read, so both want a confirmation step; adding one is a
 * behaviour change with its own tests to write, and it is recorded as outstanding rather than
 * smuggled into a presentation tranche.
 */
export function CourseLifecycleWorkspace() {
  const { locale, t } = useLocale();
  const copy = t.adminLifecycle;
  const courseLabels = t.adminCourses;

  const [search, setSearch] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");
  const [courses, setCourses] = useState<CourseLifecycleSummary[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [cause, setCause] = useState<SuspensionCause>("SECURITY");
  const [reason, setReason] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);

  const load = useCallback(
    async (query: string) => {
      setLoading(true);
      try {
        setCourses(await getCourseLifecycleDirectory(locale, query));
        setLoadError(null);
      } catch (cause) {
        setLoadError(problemMessage(cause, copy.failed));
      } finally {
        setLoading(false);
      }
    },
    [locale, copy.failed],
  );

  useEffect(() => {
    void load(appliedSearch);
  }, [load, appliedSearch]);

  const selected = courses.find((course) => course.id === selectedID) ?? null;

  const invoke = async (completed: string, action: (csrf: string) => Promise<void>) => {
    const csrf = currentCSRFToken();
    if (!csrf) {
      setError(copy.csrfMissing);
      return;
    }
    setBusy(true);
    setMessage(null);
    setError(null);
    try {
      await action(csrf);
      setMessage(completed);
    } catch (actionError) {
      setError(problemMessage(actionError, copy.failed));
    } finally {
      // The directory is reread whether the command succeeded or was refused, so the rendered
      // state is always the server's and a refusal cannot leave a stale screen behind.
      await load(appliedSearch);
      setBusy(false);
    }
  };

  const withReason = (run: () => void) => {
    if (!reason.trim()) {
      setError(copy.reasonRequired);
      return;
    }
    run();
  };

  return (
    <WorkspacePage>
      <WorkspacePageHeader title={copy.title} description={copy.intro} />

      <WorkspaceToolbar>
        <form
          role="search"
          className="flex flex-1 gap-2 sm:max-w-md"
          onSubmit={(event) => {
            event.preventDefault();
            setAppliedSearch(search.trim());
          }}
        >
          <label className="sr-only" htmlFor="lifecycle-course-search">
            {copy.searchLabel}
          </label>
          <Input
            id="lifecycle-course-search"
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={copy.searchPlaceholder}
            data-testid="lifecycle-course-search"
          />
          <Button type="submit" data-testid="lifecycle-course-search-submit">
            {copy.searchSubmit}
          </Button>
        </form>
      </WorkspaceToolbar>

      <WorkspaceSection title={copy.tableCaption} className="mt-8">
        {loadError ? (
          <ErrorState
            testID="lifecycle-course-list-error"
            title={copy.loadFailed}
            detail={loadError}
            retryLabel={copy.retry}
            onRetry={() => void load(appliedSearch)}
          />
        ) : loading ? (
          <LoadingState testID="lifecycle-course-list-loading" label={copy.loading} />
        ) : courses.length === 0 ? (
          <div data-testid="lifecycle-course-list-empty">
            <EmptyState
              density="compact"
              title={copy.emptyTitle}
              description={appliedSearch === "" ? undefined : copy.emptyBody}
              action={
                appliedSearch === "" ? undefined : (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      setSearch("");
                      setAppliedSearch("");
                    }}
                  >
                    {copy.clearSearch}
                  </Button>
                )
              }
            />
          </div>
        ) : (
          <TableContainer>
            <Table data-testid="lifecycle-course-list">
              <TableCaption>{copy.tableCaption}</TableCaption>
              <TableHead>
                <TableRow>
                  <TableHeaderCell scope="col">{copy.course}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.state}</TableHeaderCell>
                  <TableHeaderCell scope="col">{copy.actions}</TableHeaderCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {courses.map((course) => {
                  const title = locale === "ar" ? course.title_ar : course.title_en;
                  return (
                    <TableRow
                      key={course.id}
                      interactive
                      data-testid="lifecycle-course-row"
                      data-lifecycle-state={course.lifecycle}
                    >
                      <TableHeaderCell scope="row" className="min-w-48">
                        <span dir="auto">{title}</span>
                      </TableHeaderCell>
                      <TableCell>
                        <LifecycleState course={course} labels={courseLabels} />
                      </TableCell>
                      <TableCell>
                        <Button
                          type="button"
                          variant={course.id === selectedID ? "default" : "outline"}
                          size="sm"
                          aria-pressed={course.id === selectedID}
                          onClick={() => {
                            setSelectedID(course.id);
                            setMessage(null);
                            setError(null);
                          }}
                        >
                          {copy.manage}
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </WorkspaceSection>

      {selected && (
        <section
          aria-labelledby="lifecycle-selected-title"
          className="mt-8 rounded-lg border border-border bg-card p-5 shadow-sm sm:p-6"
          data-testid="lifecycle-selected-course"
          data-lifecycle-state={selected.lifecycle}
          data-access-suspended={selected.access_suspended_at ? "true" : "false"}
          data-retired={selected.retired_at ? "true" : "false"}
        >
          <p className="font-display text-xs font-bold uppercase tracking-[0.1em] text-muted-foreground">
            {copy.selected}
          </p>
          <h2
            id="lifecycle-selected-title"
            className="mt-1 font-display text-xl font-bold text-foreground"
            data-testid="lifecycle-selected-title"
            dir="auto"
          >
            {locale === "ar" ? selected.title_ar : selected.title_en}
          </h2>
          <div className="mt-3" data-testid="lifecycle-selected-state">
            <LifecycleState course={selected} labels={courseLabels} />
          </div>

          {message && (
            <div className="mt-5" data-testid="lifecycle-message">
              <Alert tone="success" title={message} />
            </div>
          )}
          {error && (
            <ErrorState
              className="mt-5"
              testID="lifecycle-error"
              title={copy.actionFailed}
              detail={error}
            />
          )}

          <CommandGroup title={copy.visibility.title} body={copy.visibility.body}>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              data-testid="lifecycle-delist"
              onClick={() =>
                void invoke(copy.completed.delist, (csrf) =>
                  delistCourse({ courseID: selected.id, locale, csrf }),
                )
              }
            >
              {copy.visibility.delist}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              data-testid="lifecycle-relist"
              onClick={() =>
                void invoke(copy.completed.relist, (csrf) =>
                  relistCourse({ courseID: selected.id, locale, csrf }),
                )
              }
            >
              {copy.visibility.relist}
            </Button>
          </CommandGroup>

          <CommandGroup title={copy.withdrawal.title} body={copy.withdrawal.body}>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              data-testid="lifecycle-retire"
              onClick={() =>
                void invoke(copy.completed.retire, (csrf) =>
                  retireCourse({ courseID: selected.id, locale, csrf }),
                )
              }
            >
              {copy.withdrawal.retire}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={busy}
              data-testid="lifecycle-archive"
              onClick={() =>
                void invoke(copy.completed.archive, (csrf) =>
                  archiveCourse({ courseID: selected.id, locale, csrf }),
                )
              }
            >
              {copy.withdrawal.archive}
            </Button>
          </CommandGroup>

          <CommandGroup title={copy.suspension.title} body={copy.suspension.body}>
            <div className="grid w-full gap-4 sm:grid-cols-2">
              <Field label={copy.suspension.causeLabel} htmlFor="lifecycle-suspension-cause">
                <Select
                  id="lifecycle-suspension-cause"
                  controlSize="sm"
                  value={cause}
                  onChange={(event) => setCause(event.target.value as SuspensionCause)}
                  data-testid="lifecycle-suspension-cause"
                >
                  {SUSPENSION_CAUSES.map((value) => (
                    <option key={value} value={value}>
                      {copy.suspension.causes[value]}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label={copy.suspension.reasonLabel} htmlFor="lifecycle-suspension-reason">
                <Input
                  id="lifecycle-suspension-reason"
                  controlSize="sm"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder={copy.suspension.reasonPlaceholder}
                  data-testid="lifecycle-suspension-reason"
                />
              </Field>
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="destructive"
                size="sm"
                disabled={busy}
                data-testid="lifecycle-suspend"
                onClick={() =>
                  withReason(() =>
                    void invoke(copy.completed.suspend, (csrf) =>
                      suspendCourseAccess({
                        courseID: selected.id,
                        locale,
                        csrf,
                        cause,
                        reason: reason.trim(),
                      }),
                    ),
                  )
                }
              >
                {copy.suspension.suspend}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={busy}
                data-testid="lifecycle-restore"
                onClick={() =>
                  withReason(() =>
                    void invoke(copy.completed.restore, (csrf) =>
                      restoreCourseAccess({
                        courseID: selected.id,
                        locale,
                        csrf,
                        reason: reason.trim(),
                      }),
                    ),
                  )
                }
              >
                {copy.suspension.restore}
              </Button>
            </div>
          </CommandGroup>

          {busy && <LoadingState visuallyHidden label={copy.working} />}
        </section>
      )}
    </WorkspacePage>
  );
}

/**
 * One group of lifecycle commands with the sentence that says what they do.
 *
 * The sentence is the point. Delist, retire and archive are three different kinds of withdrawal
 * with three different consequences for a Student who already holds access, and the screen used to
 * distinguish them with an amber button, a slate one and a rose one.
 */
function CommandGroup({
  title,
  body,
  children,
}: {
  title: string;
  body: string;
  children: ReactNode;
}) {
  return (
    <div className="mt-6 border-t border-border pt-5">
      <h3 className="font-display text-sm font-bold text-foreground">{title}</h3>
      <p className="mt-1 max-w-2xl text-sm leading-6 text-muted-foreground">{body}</p>
      <div className="mt-4 flex flex-wrap items-end gap-3">{children}</div>
    </div>
  );
}

/**
 * The rendered state names every authority currently acting on the Course, not just one.
 *
 * The state word comes from the shared Admin course vocabulary rather than from the raw `lifecycle`
 * enum this screen used to print verbatim, and the qualifiers — retired, access suspended — are
 * listed beside it rather than replacing it, because they are orthogonal to the lifecycle: a
 * suspended Course is still PUBLISHED.
 */
function LifecycleState({
  course,
  labels,
}: {
  course: CourseLifecycleSummary;
  labels: Dictionary["adminCourses"];
}) {
  // This screen reads only the lifecycle directory, so it never knows about a pending review; the
  // Courses directory is the surface that joins the two.
  const view = courseStatusView({
    id: course.id,
    titleAr: course.title_ar,
    titleEn: course.title_en,
    ownerDisplayName: course.owner_display_name,
    lifecycle: course.lifecycle,
    updatedAt: course.updated_at,
    accessSuspendedAt: course.access_suspended_at,
    retiredAt: course.retired_at,
    pendingReview: null,
    fromQueueOnly: false,
  });
  const qualifiers: string[] = [];
  if (view.retired) qualifiers.push(labels.flags.retired);
  if (view.accessSuspended) qualifiers.push(labels.flags.accessSuspended);

  return (
    <StatusBadge
      size="sm"
      tone={view.tone}
      label={labels.status[view.state]}
      detail={qualifiers.length > 0 ? qualifiers.join(" · ") : undefined}
    />
  );
}

function problemMessage(cause: unknown, fallback: string): string {
  if (cause instanceof ProblemError) {
    return cause.problem.detail || cause.problem.title || fallback;
  }
  if (cause instanceof Error && cause.message) {
    return cause.message;
  }
  return fallback;
}
