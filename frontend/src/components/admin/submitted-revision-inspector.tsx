"use client";

import { useCallback, useEffect, useState } from "react";
import { getTaxonomyTerms, isAcademicCourse, type CourseRevisionWire, type TaxonomyKind, type TaxonomyTerm } from "@/lib/api/catalog";
import { describeApiError } from "@/lib/api/api-error";
import { ProblemError } from "@/lib/api/problem";
import { getMediaAssetStatus } from "@/lib/api/media-upload";
import {
  approveCourseRevision,
  getReviewCourseRevision,
  previewAdminLesson,
  requestCourseRevisionChanges,
  type AdminLessonPreview,
  type ReviewQueueItem,
  type ReviewedCourse,
} from "@/lib/api/review";
import { currentCSRFToken } from "@/lib/identity/session";
import { useLocale } from "@/lib/i18n/locale-provider";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { EmptyState } from "@/components/common/empty-state";
import { ErrorState } from "@/components/common/error-state";
import { Field } from "@/components/ui/field";
import { LoadingState } from "@/components/common/loading-state";
import { StatusBadge } from "@/components/common/status-badge";
import { Textarea } from "@/components/ui/textarea";
import { WorkspaceSection } from "@/components/layout/workspace-page";
import { ReviewLessonPreview } from "./review-lesson-preview";
import { PricingPanel } from "./pricing-panel";
import { TaxonomyOverrideForm } from "./taxonomy-override-form";
import { AcademicReviewContext } from "./academic-review-context";

type SubmittedRevisionInspectorProps = {
  item: ReviewQueueItem;
  onClose: () => void;
  onReviewed: (notice: string) => Promise<void>;
};

type LoadedRevision = {
  course: ReviewedCourse;
  revision: CourseRevisionWire;
  terms: TaxonomyTerm[];
};

function videoIDs(revision: CourseRevisionWire): string[] {
  return revision.sections.flatMap((section) =>
    (section.lessons ?? []).flatMap((lesson) => (lesson.video_asset_version_id ? [lesson.video_asset_version_id] : [])),
  );
}

/**
 * A catalogue term by its label. `locale` selects between two data fields the server returned, which
 * is data selection rather than UI copy; the two "no term" answers are copy and come from the
 * dictionary.
 */
function taxonomyLabel(
  termID: string | undefined,
  kind: TaxonomyKind,
  terms: TaxonomyTerm[],
  copy: ReturnType<typeof useLocale>["t"]["adminReview"]["inspector"],
  locale: "ar" | "en",
): string {
  if (!termID) return copy.notSpecified;
  const term = terms.find((candidate) => candidate.id === termID && candidate.kind === kind);
  if (!term) return copy.unavailableTerm;
  return locale === "ar" ? term.label_ar : term.label_en;
}

/**
 * The server refuses publication of a Course with no Admin-set launch price
 * (`COURSE_PRICE_REQUIRED`). The backend stays authoritative; this only names
 * the remedy, because the raw violation code is not an instruction.
 */
function isCoursePriceRequired(cause: unknown): boolean {
  if (!(cause instanceof ProblemError)) return false;
  const problem = cause.problem as typeof cause.problem & {
    violations?: Array<{ code?: string }>;
  };
  return (problem.violations ?? []).some((violation) => violation.code === "COURSE_PRICE_REQUIRED");
}

/**
 * Reads and renders the graph stored in the submitted revision. It deliberately
 * never reads the Instructor's current draft; actions remain unavailable until
 * the fetched Course and revision prove they match the queue row that opened it.
 */
export function SubmittedRevisionInspector({ item, onClose, onReviewed }: SubmittedRevisionInspectorProps) {
  const { locale, dir, t } = useLocale();
  const isAr = locale === "ar";
  const [loaded, setLoaded] = useState<LoadedRevision | null>(null);
  const [loadError, setLoadError] = useState("");
  const [taxonomyError, setTaxonomyError] = useState("");
  const [loading, setLoading] = useState(true);
  const [mediaStates, setMediaStates] = useState<Record<string, string>>({});
  const [preview, setPreview] = useState<AdminLessonPreview | null>(null);
  const [previewError, setPreviewError] = useState("");
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const [actionSuccess, setActionSuccess] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const [requestReason, setRequestReason] = useState("");
  // Which decision is awaiting confirmation, or none. One piece of state for both, because an Admin
  // is answering one question at a time.
  const [pendingDecision, setPendingDecision] = useState<"approve" | "changes" | null>(null);
  // Read from the price history the pricing panel already loads. `null` means "not known yet",
  // which must not be rendered as "no price" — an unread history is not a missing price.
  const [launchPriced, setLaunchPriced] = useState<boolean | null>(null);
  const noteLaunchPrice = useCallback((known: boolean | null) => setLaunchPriced(known), []);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError("");
    setTaxonomyError("");
    setLoaded(null);
    setMediaStates({});
    setPreview(null);
    setPreviewError("");
    setReviewed(false);
    try {
      const course = await getReviewCourseRevision(item.course_id, item.revision_id, locale);
      const revision = course.editable_revision;
      if (
        course.id !== item.course_id ||
        !revision ||
        revision.id !== item.revision_id ||
        revision.course_id !== item.course_id
      ) {
        throw new Error(t.adminReview.inspector.mismatch);
      }
      let terms: TaxonomyTerm[] = [];
      if (!isAcademicCourse(course)) {
        try {
          terms = await getTaxonomyTerms(locale);
        } catch (cause) {
          setTaxonomyError(describeApiError(cause, locale));
        }
      }
      setLoaded({ course, revision, terms });
      const assetVersionIDs = videoIDs(revision);
      if (assetVersionIDs.length > 0) {
        const states = await Promise.all(
          assetVersionIDs.map(async (assetVersionID) => {
            try {
              const status = await getMediaAssetStatus(assetVersionID, locale);
              return [assetVersionID, status.state] as const;
            } catch {
              return [assetVersionID, "UNAVAILABLE"] as const;
            }
          }),
        );
        setMediaStates(Object.fromEntries(states));
      }
    } catch (cause) {
      setLoadError(describeApiError(cause, locale));
    } finally {
      setLoading(false);
    }
  }, [t, item.course_id, item.revision_id, locale]);

  useEffect(() => {
    void load();
  }, [load]);

  const copy = t.adminReview.inspector;
  const canReview = loaded !== null && !loading && !loadError;
  const canAct = canReview && !reviewed;
  const revision = loaded?.revision;
  const course = loaded?.course;
  const terms = loaded?.terms ?? [];

  const csrf = (): string | null => {
    const token = currentCSRFToken();
    if (!token) setActionError(copy.csrfMissing);
    return token || null;
  };

  /**
   * Carries out whichever decision is awaiting confirmation.
   *
   * One entry point for both, so both obey the same rule: the API is called exactly once, the
   * dialog closes whatever the server says, and a refusal is reported as a refusal rather than
   * leaving a success notice standing beside it.
   */
  const decide = async () => {
    if (!pendingDecision || busy) return;
    const isApproval = pendingDecision === "approve";
    await completeReview(
      (token) =>
        isApproval
          ? approveCourseRevision({
              courseID: item.course_id,
              revisionID: item.revision_id,
              locale,
              csrf: token,
            }).then(() => undefined)
          : requestCourseRevisionChanges({
              courseID: item.course_id,
              revisionID: item.revision_id,
              reason: requestReason,
              locale,
              csrf: token,
            }).then(() => {
              setRequestReason("");
            }),
      isApproval ? copy.approved : copy.requested,
    );
    setPendingDecision(null);
  };

  const completeReview = async (operation: (token: string) => Promise<void>, success: string) => {
    if (!canAct || busy) return;
    const token = csrf();
    if (!token) return;
    setBusy(true);
    setActionError("");
    setActionSuccess("");
    try {
      await operation(token);
      setReviewed(true);
      setActionSuccess(success);
      await onReviewed(success);
    } catch (cause) {
      const message = describeApiError(cause, locale);
      setActionError(
        isCoursePriceRequired(cause)
          ? `${copy.priceFirst} ${message}`
          : message,
      );
    } finally {
      setBusy(false);
    }
  };

  const startPreview = async (lessonID: string, assetVersionID: string | undefined) => {
    if (!canReview || !assetVersionID || mediaStates[assetVersionID] !== "READY") return;
    const token = csrf();
    if (!token) return;
    setPreviewError("");
    setPreview(null);
    try {
      const issued = await previewAdminLesson({
        courseID: item.course_id,
        revisionID: item.revision_id,
        lessonID,
        locale,
        csrf: token,
      });
      if (
        issued.course_id !== item.course_id ||
        issued.revision_id !== item.revision_id ||
        issued.lesson_id !== lessonID ||
        issued.video_asset_version_id !== assetVersionID
      ) {
        throw new Error(copy.previewMismatch);
      }
      setPreview(issued);
    } catch (cause) {
      setPreviewError(describeApiError(cause, locale));
    }
  };


  const mediaLabel = (state: string): string =>
    copy.mediaState[state as keyof typeof copy.mediaState] ?? copy.mediaState.UNAVAILABLE;

  /** A field in the "what was submitted" grid. */
  const Detail = ({
    label,
    value,
    testID,
    valueDir,
  }: {
    label: string;
    value: string;
    testID?: string;
    valueDir?: "rtl" | "ltr";
  }) => (
    <div>
      <dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {label}
      </dt>
      <dd
        dir={valueDir}
        data-testid={testID}
        className="mt-1 whitespace-pre-wrap text-sm text-foreground"
      >
        {value}
      </dd>
    </div>
  );

  return (
    <section dir={dir} data-testid="submitted-revision-inspector" className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-border pb-4">
        <div className="min-w-0">
          <h2 className="font-display text-lg font-bold text-foreground">{copy.title}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{copy.lead}</p>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onClose}>
          {copy.close}
        </Button>
      </div>

      {loading ? <LoadingState label={copy.loading} testID="submitted-revision-loading" /> : null}
      {loadError ? (
        <ErrorState title={copy.loadFailed} detail={loadError} testID="submitted-revision-error" />
      ) : null}

      {revision && canReview ? (
        <div className="space-y-6">
          <WorkspaceSection title={copy.details} headingLevel="h3">
            <dl className="grid gap-x-6 gap-y-4 rounded-lg border border-border bg-card p-5 md:grid-cols-2">
              <Detail
                label={copy.titleAr}
                value={revision.title_ar}
                testID="submitted-title-ar"
                valueDir="rtl"
              />
              <Detail
                label={copy.titleEn}
                value={revision.title_en}
                testID="submitted-title-en"
                valueDir="ltr"
              />
              <Detail
                label={copy.descriptionAr}
                value={revision.description_ar || "—"}
                testID="submitted-description-ar"
                valueDir="rtl"
              />
              <Detail
                label={copy.descriptionEn}
                value={revision.description_en || "—"}
                testID="submitted-description-en"
                valueDir="ltr"
              />
              {/* The review state, as a state rather than as the enum that carries it. This field
                  used to render `revision.state` directly, so the one thing the Admin read here in
                  a screen full of prose was PENDING_REVIEW. */}
              <div>
                <dt className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                  {copy.state}
                </dt>
                <dd className="mt-1" data-testid="submitted-revision-state">
                  <StatusBadge
                    tone="default"
                    label={revisionStateLabel(revision.state, t)}
                    size="sm"
                  />
                </dd>
              </div>
              {course && isAcademicCourse(course) ? (
                <AcademicReviewContext course={course} revision={revision} locale={locale} />
              ) : (
                <>
                  <Detail
                    label={copy.studyYear}
                    value={revision.study_year || "—"}
                    testID="submitted-study-year"
                  />
                  <Detail
                    label={copy.major}
                    value={taxonomyLabel(revision.major_term_id, "MAJOR", terms, copy, locale)}
                    testID="submitted-major"
                  />
                  <Detail
                    label={copy.subject}
                    value={taxonomyLabel(revision.subject_term_id, "SUBJECT", terms, copy, locale)}
                    testID="submitted-subject"
                  />
                </>
              )}
              <Detail
                label={copy.preview}
                value={
                  revision.preview_asset_version_id ? copy.previewPresent : copy.previewAbsent
                }
                testID="submitted-public-preview"
              />
            </dl>
          </WorkspaceSection>

          <WorkspaceSection title={copy.outline} headingLevel="h3">
            {revision.sections.length === 0 ? (
              <EmptyState
                density="compact"
                title={copy.outlineEmpty}
                testID="submitted-sections-empty"
              />
            ) : (
              <ul className="space-y-3">
                {revision.sections.map((section) => (
                  <li
                    key={section.id}
                    data-testid={`submitted-section-${section.id}`}
                    className="rounded-lg border border-border bg-card p-4"
                  >
                    <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                      {copy.section} {section.position}
                    </p>
                    <p dir="rtl" className="mt-1 font-display font-bold text-foreground">
                      {section.title_ar}
                    </p>
                    <p dir="ltr" className="text-sm text-muted-foreground">
                      {section.title_en}
                    </p>
                    <ul className="mt-3 space-y-3">
                      {(section.lessons ?? []).map((lesson) => {
                        const mediaState = lesson.video_asset_version_id
                          ? (mediaStates[lesson.video_asset_version_id] ?? "LOADING")
                          : "NO_VIDEO";
                        const canPreview = mediaState === "READY";
                        return (
                          <li
                            key={lesson.id}
                            data-testid={`submitted-lesson-${lesson.id}`}
                            className="rounded-md border border-border p-3"
                          >
                            <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                              {copy.lesson} {lesson.position}
                            </p>
                            <p dir="rtl" className="mt-1 text-foreground">
                              {lesson.title_ar}
                            </p>
                            <p dir="ltr" className="text-sm text-muted-foreground">
                              {lesson.title_en}
                            </p>
                            <p
                              data-testid={`submitted-lesson-media-state-${lesson.id}`}
                              className="mt-2 text-sm text-muted-foreground"
                            >
                              {copy.media}: {mediaLabel(mediaState)}
                            </p>
                            {lesson.files && lesson.files.length > 0 ? (
                              <ul
                                data-testid={`submitted-lesson-materials-${lesson.id}`}
                                className="mt-2 space-y-1 text-sm text-muted-foreground"
                              >
                                {lesson.files.map((file) => (
                                  <li key={file.id}>
                                    {file.kind === "RESOURCE" ? copy.resource : copy.labMaterial}:{" "}
                                    <bdi>{isAr ? file.display_name_ar : file.display_name_en}</bdi>
                                  </li>
                                ))}
                              </ul>
                            ) : null}
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              className="mt-3"
                              disabled={!canPreview || busy || reviewed}
                              onClick={() =>
                                void startPreview(lesson.id, lesson.video_asset_version_id)
                              }
                              data-testid={`preview-submitted-lesson-${lesson.id}`}
                              // The disabled reason is the media state, which the line above already
                              // states; the control names the action rather than repeating it.
                              aria-label={`${copy.previewLesson} — ${lesson.title_en}`}
                            >
                              {copy.previewLesson}
                            </Button>
                          </li>
                        );
                      })}
                    </ul>
                  </li>
                ))}
              </ul>
            )}
          </WorkspaceSection>

          {previewError ? (
            <ErrorState title={copy.previewFailed} detail={previewError} testID="review-preview-error" />
          ) : null}
          {preview ? (
            <WorkspaceSection title={copy.previewHeading} headingLevel="h3" testID="review-preview-player">
              <div className="rounded-lg border border-border bg-card p-4">
                <ReviewLessonPreview playbackURL={preview.playback_url} locale={locale} />
              </div>
            </WorkspaceSection>
          ) : null}

          <div className="grid gap-5 lg:grid-cols-2">
            {course && isAcademicCourse(course) ? null : taxonomyError ? (
              <ErrorState
                title={copy.taxonomyFailed}
                detail={taxonomyError}
                testID="review-taxonomy-error"
              />
            ) : (
              <TaxonomyOverrideForm courseID={item.course_id} revisionID={item.revision_id} terms={terms} />
            )}
            <PricingPanel
              courseID={item.course_id}
              sections={revision.sections}
              onLaunchPriceKnown={noteLaunchPrice}
            />
          </div>

          {actionSuccess ? (
            <div data-testid="review-action-success">
              <Alert tone="success" title={actionSuccess} />
            </div>
          ) : null}
          {actionError ? (
            <div data-testid="review-action-error">
              <Alert tone="error" title={actionError} />
            </div>
          ) : null}

          {/* The approval blocker the Admin owns, named before Approve is pressed rather than only
              as a refusal afterwards. The server remains authoritative: this reports what the price
              history says and never decides whether publication is permitted. */}
          {launchPriced === false ? (
            <div data-testid="review-launch-price-required">
              <Alert tone="info" title={t.adminReview.priceRequired} />
            </div>
          ) : null}

          <WorkspaceSection title={copy.decision} headingLevel="h3">
            <div className="flex flex-wrap gap-3">
              <Button
                type="button"
                disabled={busy || reviewed}
                onClick={() => setPendingDecision("approve")}
                data-testid="approve-inspected-revision"
              >
                {copy.approve}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={busy || reviewed}
                onClick={() => setPendingDecision("changes")}
                data-testid="request-changes-inspected-revision"
              >
                {copy.requestChanges}
              </Button>
            </div>
          </WorkspaceSection>

          {/*
            Both decisions are confirmed, and both confirmations state their effect.

            Approve had none at all: one click published a Course into the public catalogue and
            closed the Instructor's ability to edit the version, from a button sitting beside the
            one that sends it back. Request-changes had a hand-rolled `role="dialog"` that trapped
            no focus, closed on no key, and returned focus nowhere.
          */}
          {pendingDecision ? (
            <ConfirmDialog
              open
              onOpenChange={(next) => {
                if (!next && !busy) setPendingDecision(null);
              }}
              title={pendingDecision === "approve" ? copy.approveTitle : copy.requestChangesTitle}
              body={pendingDecision === "approve" ? copy.approveBody : copy.requestChangesBody}
              confirmLabel={
                pendingDecision === "approve" ? copy.approveConfirm : copy.requestChangesConfirm
              }
              cancelLabel={copy.cancel}
              tone={pendingDecision === "approve" ? "default" : "destructive"}
              busy={busy}
              confirmDisabled={pendingDecision === "changes" && requestReason.trim() === ""}
              onConfirm={() => void decide()}
              testID="review-decision-confirm"
            >
              {pendingDecision === "changes" ? (
                <Field htmlFor="request-changes-reason" label={copy.reason} hint={copy.reasonHint}>
                  <Textarea
                    id="request-changes-reason"
                    data-testid="request-changes-reason"
                    rows={4}
                    value={requestReason}
                    onChange={(event) => setRequestReason(event.target.value)}
                    disabled={busy}
                  />
                </Field>
              ) : null}
            </ConfirmDialog>
          ) : null}
        </div>
      ) : null}

      {!loading && !revision && !loadError ? (
        <EmptyState density="compact" title={copy.unavailable} />
      ) : null}
    </section>
  );
}

/**
 * The submitted revision's state, in words.
 *
 * In practice a revision reaches this screen only from the review queue, so the value is always
 * `PENDING_REVIEW`. The map is still a map rather than a constant, because the field is on the
 * contract and a value this screen does not recognise must degrade to something readable rather
 * than to the enum itself.
 */
function revisionStateLabel(
  state: string | undefined,
  dictionary: ReturnType<typeof useLocale>["t"],
): string {
  const labels = dictionary.adminCourses.status as Record<string, string>;
  return (state && labels[state]) || dictionary.adminReview.inspector.state;
}
