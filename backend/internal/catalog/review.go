package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

var (
	ErrReasonRequired = errors.New("reason is required for change request")
)

type approvalFailureStage string

const (
	approvalAfterSupersede approvalFailureStage = "after_supersede"
	approvalAfterApprove   approvalFailureStage = "after_approve"
	approvalAfterPointer   approvalFailureStage = "after_pointer"
	approvalAfterAudit     approvalFailureStage = "after_audit"
	approvalAfterOutbox    approvalFailureStage = "after_outbox"
)

type approvalFailureKey struct{}

type approvalHold struct {
	reached chan<- struct{}
	release <-chan struct{}
}

type approvalHoldKey struct{}

func withApprovalFailure(ctx context.Context, stage approvalFailureStage) context.Context {
	return context.WithValue(ctx, approvalFailureKey{}, stage)
}

func withApprovalHold(ctx context.Context, hold approvalHold) context.Context {
	return context.WithValue(ctx, approvalHoldKey{}, hold)
}

func failApprovalAt(ctx context.Context, stage approvalFailureStage) error {
	if requested, _ := ctx.Value(approvalFailureKey{}).(approvalFailureStage); requested == stage {
		return fmt.Errorf("injected approval failure at %s", stage)
	}
	return nil
}

func holdApprovalAfterValidation(ctx context.Context) error {
	hold, ok := ctx.Value(approvalHoldKey{}).(approvalHold)
	if !ok {
		return nil
	}
	if hold.reached != nil {
		hold.reached <- struct{}{}
	}
	if hold.release == nil {
		return nil
	}
	select {
	case <-hold.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sameNullableRevision(left, right *string) bool {
	if left == nil || *left == "" {
		return right == nil || *right == ""
	}
	return right != nil && *right == *left
}

type ReviewQueueItem struct {
	CourseID        string          `json:"course_id"`
	OwnerAccountID  string          `json:"owner_account_id"`
	RevisionID      string          `json:"revision_id"`
	RevisionNumber  int             `json:"revision_number"`
	TitleAr         string          `json:"title_ar"`
	TitleEn         string          `json:"title_en"`
	SubmittedAt     *time.Time      `json:"submitted_at"`
	CourseLifecycle CourseLifecycle `json:"course_lifecycle"`
	IsFirstPublish  bool            `json:"is_first_publish"`
}

type ApproveCourseRequest struct {
	CourseID        string
	RevisionID      string
	AdminAccountID  string
	ActorDescriptor string
}

type RequestChangesRequest struct {
	CourseID        string
	RevisionID      string
	AdminAccountID  string
	Reason          string
	ActorDescriptor string
}

type AdminPreviewRequest struct {
	CourseID        string
	RevisionID      string
	LessonID        string
	AdminAccountID  string
	ActorDescriptor string
}

func (r *Repository) ListReviewQueue(ctx context.Context) ([]ReviewQueueItem, error) {
	query := `
		SELECT c.id, c.owner_account_id, r.id, r.revision_number, r.title_ar, r.title_en,
		       r.submitted_at, c.lifecycle, (c.live_revision_id IS NULL) AS is_first_publish
		FROM course_revisions r
		JOIN courses c ON c.id = r.course_id
		WHERE r.state = 'PENDING_REVIEW'
		ORDER BY r.submitted_at ASC NULLS LAST, r.created_at ASC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying review queue: %w", err)
	}
	defer rows.Close()

	var queue []ReviewQueueItem
	for rows.Next() {
		var item ReviewQueueItem
		if err := rows.Scan(
			&item.CourseID, &item.OwnerAccountID, &item.RevisionID, &item.RevisionNumber,
			&item.TitleAr, &item.TitleEn, &item.SubmittedAt, &item.CourseLifecycle, &item.IsFirstPublish,
		); err != nil {
			return nil, fmt.Errorf("scanning review queue item: %w", err)
		}
		queue = append(queue, item)
	}
	if queue == nil {
		queue = []ReviewQueueItem{}
	}
	return queue, nil
}

func (r *Repository) GetCourseRevisionGraph(ctx context.Context, courseID string, revisionID string) (*Course, error) {
	if courseID == "" || revisionID == "" {
		return nil, ErrCourseNotFound
	}
	query := `
		SELECT id, owner_account_id, lifecycle, live_revision_id,
		       classification_model, institution_id, subject_id,
		       access_suspended_at, access_suspension_reason, retired_at,
		       created_at, updated_at
		FROM courses
		WHERE id = $1::uuid
	`
	var c Course
	err := r.pool.QueryRow(ctx, query, courseID).Scan(
		&c.ID, &c.OwnerAccountID, &c.Lifecycle, &c.LiveRevisionID,
		&c.ClassificationModel, &c.InstitutionID, &c.SubjectID,
		&c.AccessSuspendedAt, &c.AccessSuspensionReason, &c.RetiredAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting review course: %w", err)
	}

	rev, err := r.loadRevisionGraphByID(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("loading candidate revision %s: %w", revisionID, err)
	}
	if rev == nil || rev.CourseID != courseID {
		return nil, ErrCourseNotFound
	}
	c.EditableRevision = rev

	if c.LiveRevisionID != nil && *c.LiveRevisionID != "" {
		liveRevisionID := *c.LiveRevisionID
		liveRev, err := r.loadRevisionGraphByID(ctx, liveRevisionID)
		if err != nil {
			return nil, fmt.Errorf("loading live revision %s: %w", liveRevisionID, err)
		}
		if liveRev == nil {
			return nil, fmt.Errorf("live revision %s is missing", liveRevisionID)
		}
		c.LiveRevision = liveRev
	}
	if err := loadCourseAcademicProjection(ctx, r.pool, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

func (r *Repository) loadRevisionGraphByID(ctx context.Context, revID string) (*CourseRevision, error) {
	query := `
		SELECT cr.id, cr.course_id, cr.based_on_revision_id, cr.state, cr.revision_number,
		       cr.title_ar, cr.title_en, cr.description_ar, cr.description_en,
		       cr.major_term_id, cr.subject_term_id, cr.study_year, cr.preview_asset_version_id,
		       preview.state::text,
		       cr.submitted_at, cr.reviewed_at, cr.reviewed_by_account_id, cr.review_reason,
		       cr.created_at, cr.updated_at
		FROM course_revisions cr
		LEFT JOIN media_asset_versions preview ON preview.id = cr.preview_asset_version_id
		WHERE cr.id = $1::uuid
	`
	var rev CourseRevision
	err := r.pool.QueryRow(ctx, query, revID).Scan(
		&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID, &rev.PreviewAssetState,
		&rev.SubmittedAt, &rev.ReviewedAt, &rev.ReviewedByAccountID, &rev.ReviewReason, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := loadRevisionGraphBatch(ctx, r.pool, &rev); err != nil {
		return nil, fmt.Errorf("loading revision graph: %w", err)
	}
	return &rev, nil
}

type approvalContext struct {
	request   ApproveCourseRequest
	course    *CourseRow
	candidate *CourseRevision
	graph     *CourseRevision
	now       time.Time
}

// ApproveCourse commits the candidate and its evidence through one transaction.
func (r *Repository) ApproveCourse(
	ctx context.Context,
	validator AssetVersionValidator,
	req ApproveCourseRequest,
) (*Course, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.AdminAccountID == "" {
		return nil, errors.New("courseID, revisionID, and adminAccountID are required")
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
	}

	approval := &approvalContext{request: req}
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		if err := r.lockApprovalTarget(ctx, tx, approval); err != nil {
			return err
		}
		if err := r.revalidateApproval(ctx, tx, approval); err != nil {
			return err
		}
		return r.commitApproval(ctx, tx, approval)
	})
	if err != nil {
		return nil, err
	}
	return approvedCourse(approval), nil
}

func (r *Repository) lockApprovalTarget(
	ctx context.Context,
	tx pgx.Tx,
	approval *approvalContext,
) error {
	course, err := r.LockCourse(ctx, tx, approval.request.CourseID)
	if err != nil {
		return err
	}
	candidate, err := lockPendingReviewCandidate(ctx, tx, approval)
	if err != nil {
		return err
	}
	if !sameNullableRevision(course.LiveRevisionID, candidate.BasedOnRevisionID) {
		return &LifecycleConflictError{
			CourseID: approval.request.CourseID,
			Actual:   string(candidate.State),
			Expected: []string{"PENDING_REVIEW_MATCHING_BASE"},
		}
	}
	approval.course = course
	approval.candidate = candidate
	return nil
}

func lockPendingReviewCandidate(
	ctx context.Context,
	tx pgx.Tx,
	approval *approvalContext,
) (*CourseRevision, error) {
	courseID := approval.request.CourseID
	revisionID := approval.request.RevisionID
	var revision CourseRevision
	err := tx.QueryRow(ctx, `
		SELECT id, course_id, based_on_revision_id, state, revision_number,
		       title_ar, title_en, description_ar, description_en,
		       major_term_id, subject_term_id, study_year, preview_asset_version_id,
		       submitted_at, reviewed_at, reviewed_by_account_id, review_reason,
		       created_at, updated_at
		FROM course_revisions
		WHERE id = $1::uuid AND course_id = $2::uuid
		FOR UPDATE
	`, revisionID, courseID).Scan(
		&revision.ID, &revision.CourseID, &revision.BasedOnRevisionID,
		&revision.State, &revision.RevisionNumber,
		&revision.TitleAr, &revision.TitleEn, &revision.DescriptionAr, &revision.DescriptionEn,
		&revision.MajorTermID, &revision.SubjectTermID, &revision.StudyYear,
		&revision.PreviewAssetVersionID, &revision.SubmittedAt, &revision.ReviewedAt,
		&revision.ReviewedByAccountID, &revision.ReviewReason,
		&revision.CreatedAt, &revision.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, &LifecycleConflictError{
			CourseID: courseID, Actual: approval.course.Lifecycle, Expected: []string{"PENDING_REVIEW"},
		}
	}
	if err != nil {
		return nil, fmt.Errorf("locking revision: %w", err)
	}
	if revision.State != RevisionPendingReview {
		return nil, &LifecycleConflictError{
			CourseID: courseID, Actual: string(revision.State),
			Expected: []string{string(RevisionPendingReview)},
		}
	}
	return &revision, nil
}

func (r *Repository) revalidateApproval(
	ctx context.Context,
	tx pgx.Tx,
	approval *approvalContext,
) error {
	if err := lockEligibleOwner(
		ctx, tx, approval.course.OwnerAccountID, approval.request.CourseID,
	); err != nil {
		return err
	}
	graph, err := r.loadRevisionGraphByIDTx(ctx, tx, approval.candidate.ID)
	if err != nil {
		return fmt.Errorf("loading candidate revision graph: %w", err)
	}
	if graph == nil {
		return fmt.Errorf("candidate revision graph %s is missing", approval.candidate.ID)
	}
	if err := lockTaxonomyDependencies(ctx, tx, graph); err != nil {
		return err
	}
	if err := lockAcademicDependencies(ctx, tx, approval.course); err != nil {
		return err
	}
	if err := lockAudienceDependencies(ctx, tx, graph, approval.course); err != nil {
		return err
	}
	if err := lockAssetDependencies(ctx, tx, graph); err != nil {
		return err
	}
	validation, err := validateCourseForSubmission(ctx, submissionValidationRequest{
		tx: tx, validator: newTxAssetVersionValidator(tx),
		courseID: approval.request.CourseID, revision: graph, course: approval.course,
	})
	if err != nil {
		return err
	}
	// Publication, unlike Instructor submission, additionally requires the
	// Admin-owned launch price (BR-019). It is checked here rather than in
	// validateCourseForSubmission because an Instructor can never satisfy it:
	// only Admins set prices, so submission must not demand one.
	priced, err := courseHasLaunchPrice(ctx, tx, approval.request.CourseID)
	if err != nil {
		return err
	}
	if !priced {
		if validation == nil {
			validation = &SubmissionValidationError{}
		}
		validation.Violations = append(validation.Violations, SubmissionViolation{
			Code:   "COURSE_PRICE_REQUIRED",
			Target: "course:" + approval.request.CourseID,
		})
	}
	if validation != nil {
		return validation
	}
	approval.graph = graph
	return holdApprovalAfterValidation(ctx)
}

func lockEligibleOwner(ctx context.Context, tx pgx.Tx, ownerID string, courseID string) error {
	var role, status string
	err := tx.QueryRow(ctx,
		`SELECT role, status FROM accounts WHERE id = $1::uuid FOR SHARE`,
		ownerID,
	).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ownerIneligible(courseID)
	}
	if err != nil {
		return fmt.Errorf("locking owner account: %w", err)
	}
	if role != "INSTRUCTOR" || status != "ACTIVE" {
		return ownerIneligible(courseID)
	}
	return nil
}

func ownerIneligible(courseID string) error {
	return &SubmissionValidationError{Violations: []SubmissionViolation{
		{Code: "OWNER_INELIGIBLE", Target: "course:" + courseID},
	}}
}

func lockTaxonomyDependencies(ctx context.Context, tx pgx.Tx, graph *CourseRevision) error {
	termIDs := compactSortedIDs(graph.MajorTermID, graph.SubjectTermID)
	if len(termIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx,
		`SELECT id FROM taxonomy_terms WHERE id = ANY($1::uuid[]) ORDER BY id FOR SHARE`,
		termIDs,
	)
	if err != nil {
		return fmt.Errorf("locking taxonomy terms: %w", err)
	}
	return drainLockedIDs(rows)
}

// lockAcademicDependencies holds the Course's Subject for the duration of the
// approval transaction, so a concurrent retirement cannot land between
// revalidation and the live-revision swap. It mirrors lockTaxonomyDependencies
// for the Academic model and is a no-op for a legacy Course.
func lockAcademicDependencies(ctx context.Context, tx pgx.Tx, course *CourseRow) error {
	if course == nil || course.SubjectID == nil || *course.SubjectID == "" {
		return nil
	}
	rows, err := tx.Query(ctx,
		`SELECT id FROM subjects WHERE id = $1::uuid FOR SHARE`, *course.SubjectID)
	if err != nil {
		return fmt.Errorf("locking course subject: %w", err)
	}
	return drainLockedIDs(rows)
}

func lockAssetDependencies(ctx context.Context, tx pgx.Tx, graph *CourseRevision) error {
	assetIDs := referencedAssetIDs(graph)
	if len(assetIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx,
		`SELECT id FROM videos WHERE id = ANY($1::uuid[]) ORDER BY id FOR SHARE`,
		assetIDs,
	)
	if err != nil {
		return fmt.Errorf("locking asset versions: %w", err)
	}
	return drainLockedIDs(rows)
}

func compactSortedIDs(ids ...*string) []string {
	unique := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != nil && *id != "" {
			unique[*id] = struct{}{}
		}
	}
	return sortedIDs(unique)
}

func referencedAssetIDs(graph *CourseRevision) []string {
	unique := make(map[string]struct{})
	if graph.PreviewAssetVersionID != nil && *graph.PreviewAssetVersionID != "" {
		unique[*graph.PreviewAssetVersionID] = struct{}{}
	}
	for _, section := range graph.Sections {
		for _, lesson := range section.Lessons {
			if lesson.VideoAssetVersionID != nil && *lesson.VideoAssetVersionID != "" {
				unique[*lesson.VideoAssetVersionID] = struct{}{}
			}
			for _, file := range lesson.Files {
				if file.AssetVersionID != "" {
					unique[file.AssetVersionID] = struct{}{}
				}
			}
		}
	}
	return sortedIDs(unique)
}

func sortedIDs(unique map[string]struct{}) []string {
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func drainLockedIDs(rows pgx.Rows) error {
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Repository) commitApproval(
	ctx context.Context,
	tx pgx.Tx,
	approval *approvalContext,
) error {
	approval.now = time.Now().UTC()
	if err := supersedePreviousRevision(ctx, tx, approval); err != nil {
		return err
	}
	if err := approveCandidateRevision(ctx, tx, approval); err != nil {
		return err
	}
	if err := swapLiveRevision(ctx, tx, approval); err != nil {
		return err
	}
	if err := writeApprovalAudit(ctx, tx, approval); err != nil {
		return err
	}
	return r.writeApprovalNotification(ctx, tx, approval)
}

func supersedePreviousRevision(ctx context.Context, tx pgx.Tx, approval *approvalContext) error {
	previous := approval.course.LiveRevisionID
	if previous == nil || *previous == "" || *previous == approval.candidate.ID {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE course_revisions SET state = 'SUPERSEDED', updated_at = $1
		WHERE id = $2::uuid
	`, approval.now, *previous); err != nil {
		return fmt.Errorf("superseding previous live revision: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterSupersede)
}

func approveCandidateRevision(ctx context.Context, tx pgx.Tx, approval *approvalContext) error {
	if _, err := tx.Exec(ctx, `
		UPDATE course_revisions
		SET state = 'APPROVED', reviewed_at = $1,
		    reviewed_by_account_id = $2::uuid, updated_at = $1
		WHERE id = $3::uuid
	`, approval.now, approval.request.AdminAccountID, approval.candidate.ID); err != nil {
		return fmt.Errorf("approving candidate revision: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterApprove)
}

func swapLiveRevision(ctx context.Context, tx pgx.Tx, approval *approvalContext) error {
	if _, err := tx.Exec(ctx, `
		UPDATE courses
		SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED', updated_at = $2
		WHERE id = $3::uuid
	`, approval.candidate.ID, approval.now, approval.request.CourseID); err != nil {
		return fmt.Errorf("updating course live revision pointer: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterPointer)
}

func writeApprovalAudit(ctx context.Context, tx pgx.Tx, approval *approvalContext) error {
	adminID := approval.request.AdminAccountID
	if err := WriteAuditEvent(ctx, tx, AuditEvent{
		ActorAccountID: &adminID, ActorRole: "ADMIN",
		ActorDescriptor: approval.request.ActorDescriptor,
		Action:          "COURSE_PUBLISHED", TargetType: "COURSE",
		TargetID:       approval.request.CourseID,
		TargetRevision: &approval.candidate.RevisionNumber,
		Reason:         "Course revision approved and published",
		Metadata:       map[string]any{"revision_id": approval.candidate.ID},
	}); err != nil {
		return err
	}
	return failApprovalAt(ctx, approvalAfterAudit)
}

func (r *Repository) writeApprovalNotification(
	ctx context.Context,
	tx pgx.Tx,
	approval *approvalContext,
) error {
	writer, err := NewNotificationIntentWriter(r.outboxWriter)
	if err != nil {
		return fmt.Errorf("constructing notification intent writer: %w", err)
	}
	event := outbox.Event{
		Type: "catalog.course_published", SchemaVersion: 1,
		SourceModule: "CATALOG_AND_AUTHORING", AggregateType: "COURSE",
		AggregateID: approval.request.CourseID, AggregateRevision: 1,
		CorrelationID: approval.request.CourseID,
		SafePayload:   map[string]any{"course_id": approval.request.CourseID},
	}
	protected := map[string]any{
		"course_id":        approval.request.CourseID,
		"owner_account_id": approval.course.OwnerAccountID,
		"revision_id":      approval.candidate.ID,
		"published_at":     approval.now,
	}
	if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
		return fmt.Errorf("writing publication intent: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterOutbox)
}

func approvedCourse(approval *approvalContext) *Course {
	approval.graph.State = RevisionApproved
	return &Course{
		ID:             approval.request.CourseID,
		OwnerAccountID: approval.course.OwnerAccountID,
		Lifecycle:      LifecyclePublished,
		LiveRevisionID: &approval.candidate.ID,
		CreatedAt:      approval.course.CreatedAt,
		UpdatedAt:      approval.now,
		LiveRevision:   approval.graph,
	}
}

// RequestChanges requests changes on or rejects a pending course revision (BR-072, T035).
func (r *Repository) RequestChanges(
	ctx context.Context,
	req RequestChangesRequest,
) (*Course, error) {
	courseID := req.CourseID
	revisionID := req.RevisionID
	adminAccountID := req.AdminAccountID
	reason := req.Reason
	actorDescriptor := req.ActorDescriptor
	if courseID == "" || revisionID == "" || adminAccountID == "" {
		return nil, errors.New("courseID, revisionID, and adminAccountID are required")
	}
	if strings.TrimSpace(reason) == "" {
		return nil, ErrReasonRequired
	}
	if r.outboxWriter == nil {
		return nil, errors.New("outbox writer is required")
	}

	var course Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}

		var rev CourseRevision
		queryRev := `
			SELECT id, course_id, based_on_revision_id, state, revision_number FROM course_revisions
			WHERE id = $1::uuid AND course_id = $2::uuid
			FOR UPDATE
		`
		err = tx.QueryRow(ctx, queryRev, revisionID, courseID).Scan(
			&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   row.Lifecycle,
				Expected: []string{"PENDING_REVIEW"},
			}
		}
		if err != nil {
			return fmt.Errorf("locking revision: %w", err)
		}

		if rev.State != RevisionPendingReview {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   string(rev.State),
				Expected: []string{string(RevisionPendingReview)},
			}
		}
		if !sameNullableRevision(row.LiveRevisionID, rev.BasedOnRevisionID) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   string(rev.State),
				Expected: []string{"PENDING_REVIEW_MATCHING_BASE"},
			}
		}

		now := time.Now().UTC()
		isFirstPublish := (row.LiveRevisionID == nil || *row.LiveRevisionID == "")

		var auditAction string

		if isFirstPublish {
			auditAction = "COURSE_CHANGES_REQUESTED"

			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'CHANGES_REQUESTED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, review_reason = $3, updated_at = $1
				WHERE id = $4::uuid
			`, now, adminAccountID, reason, rev.ID)
			if err != nil {
				return fmt.Errorf("updating revision state: %w", err)
			}
			_, err = tx.Exec(ctx, `
				UPDATE courses
				SET lifecycle = 'CHANGES_REQUESTED', updated_at = $1
				WHERE id = $2::uuid
			`, now, courseID)
			if err != nil {
				return fmt.Errorf("updating course lifecycle: %w", err)
			}
			course.Lifecycle = LifecycleChangesRequested
		} else {
			// Published course revision rejection: state = REJECTED, course lifecycle & pointer UNCHANGED (FR-052)
			auditAction = "COURSE_REVISION_REJECTED"

			_, err = tx.Exec(ctx, `
				UPDATE course_revisions
				SET state = 'REJECTED', reviewed_at = $1, reviewed_by_account_id = $2::uuid, review_reason = $3, updated_at = $1
				WHERE id = $4::uuid
			`, now, adminAccountID, reason, rev.ID)
			if err != nil {
				return fmt.Errorf("updating revision state: %w", err)
			}
			course.Lifecycle = CourseLifecycle(row.Lifecycle)
			course.LiveRevisionID = row.LiveRevisionID
		}

		audit := AuditEvent{
			ActorAccountID:  &adminAccountID,
			ActorRole:       "ADMIN",
			ActorDescriptor: actorDescriptor,
			Action:          auditAction,
			TargetType:      "COURSE",
			TargetID:        courseID,
			TargetRevision:  &rev.RevisionNumber,
			Reason:          reason,
			Metadata:        map[string]any{"revision_id": rev.ID, "is_first_publish": isFirstPublish},
		}
		if err := WriteAuditEvent(ctx, tx, audit); err != nil {
			return err
		}

		writer, err := NewNotificationIntentWriter(r.outboxWriter)
		if err != nil {
			return fmt.Errorf("constructing notification intent writer: %w", err)
		}
		eventType := "catalog.course_changes_requested"
		if !isFirstPublish {
			eventType = "catalog.course_revision_rejected"
		}
		event := outbox.Event{
			Type:              eventType,
			SchemaVersion:     1,
			SourceModule:      "CATALOG_AND_AUTHORING",
			AggregateType:     "COURSE",
			AggregateID:       courseID,
			AggregateRevision: 1,
			CorrelationID:     courseID,
			SafePayload:       map[string]any{"course_id": courseID},
		}
		protected := map[string]any{
			"course_id":        courseID,
			"owner_account_id": row.OwnerAccountID,
			"revision_id":      rev.ID,
			"reason":           reason,
		}
		if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
			return fmt.Errorf("writing rejection intent: %w", err)
		}

		course.ID = courseID
		course.OwnerAccountID = row.OwnerAccountID
		course.CreatedAt = row.CreatedAt
		course.UpdatedAt = now
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &course, nil
}

// PreviewAdminLesson satisfies BR-081 & FR-016: Admin video preview creates NO enrollment and NO entitlement, and is audited on a distinct path.
func (r *Repository) PreviewAdminLesson(
	ctx context.Context,
	req AdminPreviewRequest,
) (string, error) {
	courseID := req.CourseID
	revisionID := req.RevisionID
	lessonID := req.LessonID
	adminAccountID := req.AdminAccountID
	actorDescriptor := req.ActorDescriptor
	if courseID == "" || revisionID == "" || lessonID == "" || adminAccountID == "" {
		return "", errors.New("courseID, revisionID, lessonID, and adminAccountID are required")
	}

	var videoAssetVersionID *string
	err := r.pool.QueryRow(ctx, `
		SELECT l.video_asset_version_id
		FROM course_lessons l
		JOIN course_sections s ON s.id = l.section_id
		WHERE s.revision_id = $1::uuid AND s.course_id = $2::uuid
		  AND l.lesson_identity_id = $3::uuid
	`, revisionID, courseID, lessonID).Scan(&videoAssetVersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrCourseNotFound
	}
	if err != nil {
		return "", fmt.Errorf("querying lesson video: %w", err)
	}

	if videoAssetVersionID == nil || *videoAssetVersionID == "" {
		return "", errors.New("lesson has no attached video asset version")
	}

	err = r.ExecTx(ctx, func(tx pgx.Tx) error {
		audit := AuditEvent{
			ActorAccountID:  &adminAccountID,
			ActorRole:       "ADMIN",
			ActorDescriptor: actorDescriptor,
			Action:          "ADMIN_CONTENT_PREVIEWED",
			TargetType:      "LESSON",
			TargetID:        lessonID,
			Reason:          "Admin content preview",
			Metadata:        map[string]any{"course_id": courseID, "revision_id": revisionID, "video_asset_version_id": *videoAssetVersionID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
	if err != nil {
		return "", err
	}

	return *videoAssetVersionID, nil
}

func (r *Repository) loadRevisionGraphByIDTx(ctx context.Context, tx pgx.Tx, revID string) (*CourseRevision, error) {
	query := `
		SELECT cr.id, cr.course_id, cr.based_on_revision_id, cr.state, cr.revision_number,
		       cr.title_ar, cr.title_en, cr.description_ar, cr.description_en,
		       cr.major_term_id, cr.subject_term_id, cr.study_year, cr.preview_asset_version_id,
		       preview.state::text,
		       cr.submitted_at, cr.reviewed_at, cr.reviewed_by_account_id, cr.review_reason,
		       cr.created_at, cr.updated_at
		FROM course_revisions cr
		LEFT JOIN media_asset_versions preview ON preview.id = cr.preview_asset_version_id
		WHERE cr.id = $1::uuid
	`
	var rev CourseRevision
	err := tx.QueryRow(ctx, query, revID).Scan(
		&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID, &rev.PreviewAssetState,
		&rev.SubmittedAt, &rev.ReviewedAt, &rev.ReviewedByAccountID, &rev.ReviewReason, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := loadRevisionGraphBatch(ctx, tx, &rev); err != nil {
		return nil, fmt.Errorf("loading revision graph: %w", err)
	}
	return &rev, nil
}
