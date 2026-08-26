"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useLocale } from "@/lib/i18n/locale-provider";
import {
  listReviewQueue,
  type ReviewQueueItem,
} from "@/lib/api/review";
import { describeApiError } from "@/lib/api/api-error";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import {
  WorkspacePage,
  WorkspacePageHeader,
  WorkspaceSection,
} from "@/components/layout/workspace-page";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableContainer,
  TableHead,
  TableHeaderCell,
  TableRow,
  TableSkeletonRows,
} from "@/components/ui/table";
import { TaxonomyVocabularyPanel } from "./taxonomy-vocabulary-panel";

export type { ReviewQueueItem } from "@/lib/api/review";

const COLUMN_COUNT = 5;

/**
 * Admin Catalog review surface.
 *
 * The queue rendered here is the server's set of `PENDING_REVIEW` revisions, read from
 * `/admin/review/queue`. There is no local fixture and no fallback content: an empty response
 * renders an empty queue, because a Course the founder never submitted must never appear as if it
 * were waiting.
 *
 * Selecting one queue row opens the review workspace for that Course at its own address
 * (`/<locale>/admin/courses/<id>/review`), which is where inspection, taxonomy override, pricing,
 * preview and the decision live. The workspace used to be component state here, so a review could
 * not be linked, reloaded or returned to with Back; the route is what makes it addressable, and
 * what lets the Courses directory send an Admin straight to the right Course.
 *
 * This screen holds none of the queue's meaning — the set, the ordering and the revision facts are
 * all the server's. What changed here is only how they are said: the copy moved out of inline
 * `isAr` ternaries into the dictionary, the frame and the states came from the shared workspace
 * primitives, and the two publish-type pills stopped being a blue chip against a purple one, which
 * distinguished a first publication from an update by hue and by nothing else.
 */
export function ReviewQueue() {
  const { locale, t } = useLocale();
  const copy = t.adminReviewQueue;

  const [items, setItems] = useState<ReviewQueueItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [queueError, setQueueError] = useState<string | null>(null);

  const loadQueue = useCallback(async () => {
    setQueueError(null);
    try {
      setItems(await listReviewQueue(locale));
    } catch (cause) {
      setItems([]);
      setQueueError(describeApiError(cause, locale));
    }
  }, [locale]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    loadQueue().finally(() => {
      if (!cancelled) setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [loadQueue]);

  return (
    <WorkspacePage>
      <WorkspacePageHeader
        title={copy.title}
        description={copy.intro}
        status={
          !loading && !queueError ? (
            <StatusBadge
              tone={items.length > 0 ? "accent" : "neutral"}
              label={String(items.length)}
              detail={copy.pendingCount}
              labelTestID="review-queue-count"
            />
          ) : null
        }
        actions={
          <Button
            type="button"
            variant="outline"
            onClick={() => void loadQueue()}
            data-testid="refresh-review-queue"
          >
            {copy.refresh}
          </Button>
        }
      />

      <WorkspaceSection title={copy.tableCaption} className="mt-8">
        {queueError ? (
          <ErrorState
            testID="review-queue-error"
            title={copy.loadFailed}
            detail={queueError}
            retryLabel={copy.retry}
            onRetry={() => void loadQueue()}
          />
        ) : loading ? (
          <>
            {/* The header row is rendered against placeholder rows so the columns are already
                measured when the response lands and nothing resizes underneath the reader. */}
            <LoadingState
              visuallyHidden
              testID="review-queue-loading"
              label={copy.loading}
            />
            <TableContainer>
              <Table>
                <TableCaption>{copy.tableCaption}</TableCaption>
                <QueueTableHead copy={copy} />
                <TableSkeletonRows columns={COLUMN_COUNT} />
              </Table>
            </TableContainer>
          </>
        ) : items.length === 0 ? (
          <div data-testid="review-queue-empty">
            <EmptyState
              density="compact"
              title={copy.emptyTitle}
              description={copy.emptyBody}
            />
          </div>
        ) : (
          <TableContainer>
            <Table data-testid="review-queue-table">
              <TableCaption>{copy.tableCaption}</TableCaption>
              <QueueTableHead copy={copy} />
              <TableBody>
                {items.map((item) => (
                  <QueueRow key={item.revision_id} item={item} locale={locale} copy={copy} />
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </WorkspaceSection>

      <WorkspaceSection>
        <TaxonomyVocabularyPanel />
      </WorkspaceSection>
    </WorkspacePage>
  );
}

type QueueCopy = ReturnType<typeof useLocale>["t"]["adminReviewQueue"];

function QueueTableHead({ copy }: { copy: QueueCopy }) {
  return (
    <TableHead>
      <TableRow>
        <TableHeaderCell scope="col">{copy.course}</TableHeaderCell>
        <TableHeaderCell scope="col">{copy.revision}</TableHeaderCell>
        <TableHeaderCell scope="col">{copy.publishType}</TableHeaderCell>
        <TableHeaderCell scope="col">{copy.submitted}</TableHeaderCell>
        <TableHeaderCell scope="col">{copy.actions}</TableHeaderCell>
      </TableRow>
    </TableHead>
  );
}

function QueueRow({
  item,
  locale,
  copy,
}: {
  item: ReviewQueueItem;
  locale: "ar" | "en";
  copy: QueueCopy;
}) {
  const isAr = locale === "ar";
  const primary = isAr ? item.title_ar : item.title_en;
  const secondary = isAr ? item.title_en : item.title_ar;

  return (
    <TableRow interactive data-testid={`review-item-${item.course_id}`}>
      {/* The Course title is the row's own header, so a screen reader announces which Course each
          following cell belongs to instead of reading five unattached values. */}
      <TableHeaderCell scope="row" className="min-w-48">
        {/* `<bdi>` rather than `dir="auto"`: on a block element `dir="auto"` resolves the block's
            own direction, which pushed an Arabic title to the far edge of an otherwise
            left-aligned cell. This isolates the run without moving the box. */}
        <span className="block text-foreground">
          <bdi>{primary}</bdi>
        </span>
        {secondary ? (
          <span className="mt-0.5 block text-xs font-normal text-muted-foreground">
            <bdi>{secondary}</bdi>
          </span>
        ) : null}
      </TableHeaderCell>
      <TableCell className="tabular-nums" dir="ltr">
        v{item.revision_number}
      </TableCell>
      <TableCell>
        {/* Words, not two hues. "First publication" and "update to a published course" are a real
            difference to an Admin, and a blue chip against a purple one stated it to nobody. */}
        <StatusBadge
          size="sm"
          tone={item.is_first_publish ? "default" : "neutral"}
          label={item.is_first_publish ? copy.firstPublication : copy.pendingRevision}
        />
      </TableCell>
      <TableCell className="whitespace-nowrap text-muted-foreground">
        {item.submitted_at ? (
          <time dateTime={item.submitted_at}>
            {new Date(item.submitted_at).toLocaleDateString(locale)}
          </time>
        ) : (
          copy.unknownDate
        )}
      </TableCell>
      <TableCell>
        {/* A link, not in-page state: the review workspace has its own address, so a decision can
            be reloaded, shared and returned to with Back. */}
        <Button asChild size="sm">
          <Link
            href={`/${locale}/admin/courses/${item.course_id}/review`}
            data-testid={`inspect-review-item-${item.course_id}`}
            aria-label={`${copy.openFor} ${primary}`}
          >
            {copy.open}
          </Link>
        </Button>
      </TableCell>
    </TableRow>
  );
}
