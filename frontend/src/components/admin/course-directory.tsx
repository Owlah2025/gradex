"use client";

import Link from "next/link";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { getCourseLifecycleDirectory } from "@/lib/api/catalog";
import { listReviewQueue } from "@/lib/api/review";
import { describeApiError } from "@/lib/api/api-error";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { EmptyState } from "@/components/common/empty-state";
import { Alert } from "@/components/ui/alert";
import {
  DIRECTORY_FILTERS,
  DIRECTORY_PAGE_LIMIT,
  buildDirectory,
  courseStatusView,
  courseTitles,
  filterCounts,
  matchesFilter,
  type AdminCourseRow,
  type DirectoryFilter,
} from "./course-status";

/**
 * Admin Courses — the human-readable discovery surface for the whole catalogue.
 *
 * This screen exists because Admin Course discovery used to be keyed to `PENDING_REVIEW` and
 * nothing else: a Course an Instructor had just created appeared in no Admin list at all, so
 * reaching it meant reading its identifier off another screen and carrying it across by hand.
 * `GET /admin/courses` already returns every Course in every lifecycle state together with the
 * owner's display name — `catalog/lifecycle_directory.go` describes that payload as carrying
 * "enough human-readable identity to take it without handling a UUID". Nothing in the product
 * mounted it. This is that surface.
 *
 * "Needs review" is deliberately **not** `lifecycle === "PENDING_REVIEW"`. It is membership in
 * `GET /admin/review/queue`, which is the only set the server treats as awaiting a decision. The
 * two differ in a real case: an Instructor revising a published Course produces a `PENDING_REVIEW`
 * revision while the Course lifecycle stays `PUBLISHED`. Reading the lifecycle would hide exactly
 * that Course from the Admin who owes it a decision, and would simultaneously pull in DRAFT
 * Courses, which require no Admin action at all.
 *
 * The two reads fail independently. A directory that loads without its queue still renders every
 * Course with its state, and says plainly that the review column may be incomplete — a partially
 * readable directory is more useful to an Admin than a blank page, provided it does not lie about
 * what it knows.
 */
export function CourseDirectory() {
  const { locale, dir, t } = useLocale();
  const copy = t.adminCourses;

  const [searchInput, setSearchInput] = useState("");
  const [appliedSearch, setAppliedSearch] = useState("");
  const [rows, setRows] = useState<AdminCourseRow[] | null>(null);
  // Whether the lifecycle directory returned a full page. Tracked from the directory read itself
  // rather than from the row count, which now also carries Courses found only in the review queue.
  const [directoryCapped, setDirectoryCapped] = useState(false);
  const [filter, setFilter] = useState<DirectoryFilter>("NEEDS_REVIEW");
  const [error, setError] = useState<string | null>(null);
  const [queueUnavailable, setQueueUnavailable] = useState(false);
  const [attempt, setAttempt] = useState(0);

  const load = useCallback(
    async (query: string) => {
      setRows(null);
      setError(null);
      setQueueUnavailable(false);
      // Issued together rather than in sequence: the queue is secondary information and must not
      // add a second serial round trip before any Course can be rendered.
      const [directory, queue] = await Promise.allSettled([
        getCourseLifecycleDirectory(locale, query),
        listReviewQueue(locale),
      ]);
      if (directory.status === "rejected") {
        setError(describeApiError(directory.reason, locale));
        return;
      }
      if (queue.status === "rejected") setQueueUnavailable(true);
      setDirectoryCapped(directory.value.length >= DIRECTORY_PAGE_LIMIT);
      setRows(
        buildDirectory(directory.value, queue.status === "fulfilled" ? queue.value : []),
      );
    },
    [locale],
  );

  useEffect(() => {
    void load(appliedSearch);
  }, [load, appliedSearch, attempt]);

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setAppliedSearch(searchInput.trim());
  }

  const counts = rows ? filterCounts(rows) : null;
  const visible = rows?.filter((row) => matchesFilter(row, filter)) ?? [];

  return (
    <div dir={dir} className="mx-auto max-w-container px-5 py-8 sm:px-6">
      <header className="border-b border-border pb-6">
        <h1 className="font-display text-3xl font-bold text-foreground">{copy.title}</h1>
        <p className="mt-2 max-w-2xl text-muted-foreground">{copy.intro}</p>
      </header>

      <div className="mt-6 flex flex-wrap items-center gap-3">
        <form role="search" onSubmit={submitSearch} className="flex flex-1 gap-2 sm:max-w-md">
          <label className="sr-only" htmlFor="admin-course-search">
            {copy.searchLabel}
          </label>
          <Input
            id="admin-course-search"
            type="search"
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder={copy.searchPlaceholder}
            data-testid="admin-course-search"
          />
          <Button type="submit" data-testid="admin-course-search-submit">
            {copy.searchSubmit}
          </Button>
        </form>
        <Button
          type="button"
          variant="outline"
          onClick={() => setAttempt((value) => value + 1)}
          data-testid="admin-course-refresh"
        >
          {copy.refresh}
        </Button>
      </div>

      {/* A single-select filter group rather than navigation: these change what the current screen
          shows, they do not go anywhere, so the active one is announced with aria-pressed. */}
      <div
        role="group"
        aria-label={copy.filterGroupLabel}
        className="mt-6 flex flex-wrap gap-2"
        data-testid="admin-course-filters"
      >
        {DIRECTORY_FILTERS.map((key) => {
          const active = key === filter;
          return (
            <button
              key={key}
              type="button"
              onClick={() => setFilter(key)}
              aria-pressed={active}
              data-testid={`admin-course-filter-${key}`}
              className={
                active
                  ? "rounded-md bg-primary px-3 py-2 text-sm font-semibold text-primary-foreground"
                  : "rounded-md border border-border px-3 py-2 text-sm font-semibold text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              }
            >
              {copy.filters[key]}
              {counts ? <span className="ms-2 tabular-nums">{counts[key]}</span> : null}
            </button>
          );
        })}
      </div>

      {rows && directoryCapped && (
        <p className="mt-5 text-sm text-muted-foreground" data-testid="admin-course-capped">
          {copy.capped}
        </p>
      )}

      {queueUnavailable && (
        <div className="mt-5" data-testid="admin-course-queue-unavailable">
          <Alert tone="info" title={copy.queueUnavailable} />
        </div>
      )}

      {error && (
        <div className="mt-5" data-testid="admin-course-error">
          <Alert tone="error" title={copy.loadFailed}>
            <p className="mb-3">{error}</p>
            <Button variant="outline" size="sm" onClick={() => setAttempt((value) => value + 1)}>
              {copy.retry}
            </Button>
          </Alert>
        </div>
      )}

      {!rows && !error && (
        <p className="mt-8 text-muted-foreground" aria-live="polite" data-testid="admin-course-loading">
          {copy.loading}
        </p>
      )}

      {rows && visible.length === 0 && (
        <div className="mt-8" data-testid="admin-course-empty">
          <EmptyState
            title={appliedSearch === "" ? copy.empty[filter] : copy.emptySearch}
            action={
              appliedSearch === "" ? undefined : (
                <Button
                  variant="outline"
                  onClick={() => {
                    setSearchInput("");
                    setAppliedSearch("");
                  }}
                >
                  {copy.clearSearch}
                </Button>
              )
            }
          />
        </div>
      )}

      {rows && visible.length > 0 && (
        <>
          <p className="mt-6 text-sm text-muted-foreground" aria-live="polite">
            {visible.length} {copy.resultCount}
          </p>
          {/* One list of rows rather than a wide table: every column an Admin needs here is
              descriptive rather than comparative, and this keeps the same markup usable from 375px
              upward without a separate mobile representation to keep in step. */}
          <ul className="mt-3 space-y-3" data-testid="admin-course-list">
            {visible.map((row) => (
              <CourseRow key={row.id} row={row} locale={locale} />
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

function CourseRow({ row, locale }: { row: AdminCourseRow; locale: "ar" | "en" }) {
  const { t } = useLocale();
  const copy = t.adminCourses;
  const view = courseStatusView(row);
  const { primary, secondary } = courseTitles(row, locale);

  // Only a Course with a pending decision has a review workspace to open. Everything else routes to
  // the lifecycle surface, which is where the remaining Admin commands actually live.
  const href = view.needsReview
    ? `/${locale}/admin/courses/${row.id}/review`
    : `/${locale}/admin/course-lifecycle`;

  return (
    <li
      data-testid="admin-course-row"
      data-course-id={row.id}
      data-from-queue-only={row.fromQueueOnly ? "true" : "false"}
      data-course-state={view.state}
      data-needs-review={view.needsReview ? "true" : "false"}
      className="rounded-lg border border-border bg-card p-4 shadow-sm"
    >
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={view.tone} data-testid="admin-course-status">
              {copy.status[view.state]}
            </Badge>
            {/* Never colour alone: the actor is stated in words next to the state. */}
            <span className="text-xs font-semibold text-muted-foreground" data-testid="admin-course-awaiting">
              {copy.awaiting[view.awaiting]}
            </span>
            {view.accessSuspended && <Badge variant="neutral">{copy.flags.accessSuspended}</Badge>}
            {view.retired && <Badge variant="neutral">{copy.flags.retired}</Badge>}
          </div>

          <h2 className="mt-3 font-display text-lg font-bold leading-snug text-foreground">
            {primary}
          </h2>
          {secondary ? (
            <p className="text-sm text-muted-foreground" dir="auto">
              {secondary}
            </p>
          ) : null}

          <dl className="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-sm text-muted-foreground">
            {/* Owner and last-update are omitted rather than filled in when this Course is known
                only from the review queue, which does not carry them. An empty value says less; a
                placeholder would read as data. */}
            {row.ownerDisplayName ? (
              <div className="flex gap-1.5">
                <dt>{copy.instructor}:</dt>
                <dd className="font-medium text-foreground">{row.ownerDisplayName}</dd>
              </div>
            ) : null}
            {row.updatedAt ? (
              <div className="flex gap-1.5">
                <dt>{copy.lastUpdated}:</dt>
                <dd>
                  <time dateTime={row.updatedAt}>
                    {new Date(row.updatedAt).toLocaleDateString(locale)}
                  </time>
                </dd>
              </div>
            ) : null}
            {row.pendingReview ? (
              <div className="flex gap-1.5">
                <dt>{copy.revision}:</dt>
                <dd dir="ltr">
                  v{row.pendingReview.revision_number} ·{" "}
                  {row.pendingReview.is_first_publish ? copy.firstPublication : copy.updatedRevision}
                </dd>
              </div>
            ) : null}
          </dl>

          <p className="mt-3 max-w-2xl text-sm text-muted-foreground">{copy.explain[view.state]}</p>
        </div>

        <Button asChild variant={view.needsReview ? "default" : "outline"}>
          <Link href={href} data-testid={`admin-course-action-${row.id}`}>
            {copy.actions[view.action]}
          </Link>
        </Button>
      </div>
    </li>
  );
}
