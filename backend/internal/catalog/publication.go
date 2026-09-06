package catalog

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

// Publication actors.
//
// A Course reaches the public catalogue exactly once through the Admin review
// gate — its first publication. Every later revision of that same Course is
// published by its own Instructor (D-097). Both paths promote one exact
// revision to live through the identical atomic primitive below; only the
// authorization and eligibility gates in front of it differ, so there is no
// second, subtly different pointer swap to keep in step.
type publicationActor string

const (
	publicationByAdmin      publicationActor = "ADMIN"
	publicationByInstructor publicationActor = "INSTRUCTOR"
)

var (
	// ErrFirstPublicationRequiresReview refuses an Instructor self-publication
	// of a Course that has never passed Admin review. It is the server-side
	// half of the first-publication boundary; hiding the control is not.
	ErrFirstPublicationRequiresReview = errors.New("first publication requires admin approval")

	// ErrAlreadyPublished refuses a submission for review of a Course that has
	// already been published once. Such a revision is the Instructor's to
	// publish, and must never re-enter the Admin review queue.
	ErrAlreadyPublished = errors.New("course has already been published; publish the revision directly")
)

// PublishRevisionRequest is the complete input for an Instructor publishing
// their own exact candidate revision of an already-published Course.
type PublishRevisionRequest struct {
	CourseID        string
	RevisionID      string
	OwnerAccountID  string
	ActorDescriptor string
}

type publicationContext struct {
	courseID   string
	revisionID string
	actor      publicationActor
	actorID    string
	descriptor string

	course    *CourseRow
	candidate *CourseRevision
	graph     *CourseRevision
	now       time.Time
	// firstPublication is read from committed state inside the transaction, not
	// from the caller, and decides both the audit narrative and whether the
	// Course is entering the catalogue for the first time.
	firstPublication bool
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
	return r.publishRevision(ctx, &publicationContext{
		courseID: req.CourseID, revisionID: req.RevisionID,
		actor: publicationByAdmin, actorID: req.AdminAccountID, descriptor: req.ActorDescriptor,
	})
}

// PublishRevision promotes one exact Instructor-owned candidate revision of an
// already-published Course to live, with no Admin decision in the path.
//
// It is deliberately the same transaction as Admin approval: lock, revalidate
// against committed state, supersede, approve, swap the pointer, audit, emit.
// The only additions are the ownership and first-publication gates, and both
// are enforced here rather than at the edge.
func (r *Repository) PublishRevision(
	ctx context.Context,
	validator AssetVersionValidator,
	req PublishRevisionRequest,
) (*Course, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
	}
	return r.publishRevision(ctx, &publicationContext{
		courseID: req.CourseID, revisionID: req.RevisionID,
		actor: publicationByInstructor, actorID: req.OwnerAccountID, descriptor: req.ActorDescriptor,
	})
}

func (r *Repository) publishRevision(ctx context.Context, pub *publicationContext) (*Course, error) {
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		if err := r.lockPublicationTarget(ctx, tx, pub); err != nil {
			return err
		}
		if err := r.revalidatePublication(ctx, tx, pub); err != nil {
			return err
		}
		return r.commitPublication(ctx, tx, pub)
	})
	if err != nil {
		return nil, err
	}
	return publishedCourse(pub), nil
}

func (r *Repository) lockPublicationTarget(
	ctx context.Context,
	tx pgx.Tx,
	pub *publicationContext,
) error {
	course, err := r.LockCourse(ctx, tx, pub.courseID)
	if err != nil {
		return err
	}
	pub.course = course
	pub.firstPublication = course.LiveRevisionID == nil || *course.LiveRevisionID == ""

	if err := authorizePublication(pub); err != nil {
		return err
	}

	candidate, err := lockPublishableCandidate(ctx, tx, pub)
	if err != nil {
		return err
	}
	// The candidate must descend from exactly the revision that is live now.
	// A stale candidate — one based on a revision that has since been replaced
	// — can never overwrite the newer live revision.
	if !sameNullableRevision(course.LiveRevisionID, candidate.BasedOnRevisionID) {
		return &LifecycleConflictError{
			CourseID: pub.courseID,
			Actual:   string(candidate.State),
			Expected: []string{"CANDIDATE_MATCHING_LIVE_BASE"},
		}
	}
	pub.candidate = candidate
	return nil
}

// authorizePublication is the actor-dependent gate, applied to the locked
// Course row rather than to anything the caller supplied.
func authorizePublication(pub *publicationContext) error {
	if pub.actor == publicationByAdmin {
		return nil
	}
	if pub.course.OwnerAccountID != pub.actorID {
		return ErrCourseNotFound
	}
	// An Instructor may never carry a Course through its first publication.
	if pub.firstPublication {
		return ErrFirstPublicationRequiresReview
	}
	// Admin-owned lifecycle remains Admin-owned. A delisted, archived,
	// access-suspended, or retired Course is not republished by its Instructor
	// as a side effect of shipping an edit.
	if CourseLifecycle(pub.course.Lifecycle) != LifecyclePublished {
		return &LifecycleConflictError{
			CourseID: pub.courseID, Actual: pub.course.Lifecycle,
			Expected: []string{string(LifecyclePublished)},
		}
	}
	if pub.course.AccessSuspendedAt != nil || pub.course.RetiredAt != nil {
		return &LifecycleConflictError{
			CourseID: pub.courseID, Actual: "SUSPENDED_OR_RETIRED",
			Expected: []string{string(LifecyclePublished)},
		}
	}
	return nil
}

// publishableCandidateStates names, per actor, the revision states that may be
// promoted. Admin publishes only what was submitted to it; an Instructor
// publishes only their own open editable candidate.
func publishableCandidateStates(actor publicationActor) []RevisionState {
	if actor == publicationByAdmin {
		return []RevisionState{RevisionPendingReview}
	}
	return []RevisionState{RevisionDraft, RevisionChangesRequested}
}

func lockPublishableCandidate(
	ctx context.Context,
	tx pgx.Tx,
	pub *publicationContext,
) (*CourseRevision, error) {
	allowed := publishableCandidateStates(pub.actor)
	expected := make([]string, 0, len(allowed))
	for _, state := range allowed {
		expected = append(expected, string(state))
	}

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
	`, pub.revisionID, pub.courseID).Scan(
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
			CourseID: pub.courseID, Actual: pub.course.Lifecycle, Expected: expected,
		}
	}
	if err != nil {
		return nil, fmt.Errorf("locking revision: %w", err)
	}
	for _, state := range allowed {
		if revision.State == state {
			return &revision, nil
		}
	}
	return nil, &LifecycleConflictError{
		CourseID: pub.courseID, Actual: string(revision.State), Expected: expected,
	}
}

func (r *Repository) revalidatePublication(
	ctx context.Context,
	tx pgx.Tx,
	pub *publicationContext,
) error {
	if err := lockEligibleOwner(
		ctx, tx, pub.course.OwnerAccountID, pub.courseID,
	); err != nil {
		return err
	}
	graph, err := r.loadRevisionGraphByIDTx(ctx, tx, pub.candidate.ID)
	if err != nil {
		return fmt.Errorf("loading candidate revision graph: %w", err)
	}
	if graph == nil {
		return fmt.Errorf("candidate revision graph %s is missing", pub.candidate.ID)
	}
	if err := lockTaxonomyDependencies(ctx, tx, graph); err != nil {
		return err
	}
	if err := lockAcademicDependencies(ctx, tx, pub.course); err != nil {
		return err
	}
	if err := lockAudienceDependencies(ctx, tx, graph, pub.course); err != nil {
		return err
	}
	if err := lockAssetDependencies(ctx, tx, graph); err != nil {
		return err
	}
	// The same completeness and media-readiness gate the Instructor faced at
	// submission, re-run against committed state. A Lesson video that is still
	// processing fails here whichever actor is publishing.
	validation, err := validateCourseForSubmission(ctx, submissionValidationRequest{
		tx: tx, validator: newTxAssetVersionValidator(tx),
		courseID: pub.courseID, revision: graph, course: pub.course,
	})
	if err != nil {
		return err
	}
	// Publication, unlike Instructor submission, additionally requires the
	// Admin-owned launch price (BR-019). It is checked here rather than in
	// validateCourseForSubmission because an Instructor can never satisfy it:
	// only Admins set prices, so submission must not demand one. A Course that
	// has already published necessarily has one, so the Instructor path is not
	// blocked by a rule it cannot act on.
	priced, err := courseHasLaunchPrice(ctx, tx, pub.courseID)
	if err != nil {
		return err
	}
	if !priced {
		if validation == nil {
			validation = &SubmissionValidationError{}
		}
		validation.Violations = append(validation.Violations, SubmissionViolation{
			Code:   "COURSE_PRICE_REQUIRED",
			Target: "course:" + pub.courseID,
		})
	}
	if validation != nil {
		return validation
	}
	pub.graph = graph
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
// publication transaction, so a concurrent retirement cannot land between
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

func (r *Repository) commitPublication(
	ctx context.Context,
	tx pgx.Tx,
	pub *publicationContext,
) error {
	pub.now = time.Now().UTC()
	if err := supersedePreviousRevision(ctx, tx, pub); err != nil {
		return err
	}
	if err := approveCandidateRevision(ctx, tx, pub); err != nil {
		return err
	}
	if err := swapLiveRevision(ctx, tx, pub); err != nil {
		return err
	}
	if err := writePublicationAudit(ctx, tx, pub); err != nil {
		return err
	}
	return r.writePublicationNotification(ctx, tx, pub)
}

func supersedePreviousRevision(ctx context.Context, tx pgx.Tx, pub *publicationContext) error {
	previous := pub.course.LiveRevisionID
	if previous == nil || *previous == "" || *previous == pub.candidate.ID {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE course_revisions SET state = 'SUPERSEDED', updated_at = $1
		WHERE id = $2::uuid
	`, pub.now, *previous); err != nil {
		return fmt.Errorf("superseding previous live revision: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterSupersede)
}

// approveCandidateRevision marks the promoted revision APPROVED. The reviewer
// column records who made it live: the Admin on a first publication, the
// Instructor on their own subsequent publication. Nothing claims a review
// happened that did not.
func approveCandidateRevision(ctx context.Context, tx pgx.Tx, pub *publicationContext) error {
	if _, err := tx.Exec(ctx, `
		UPDATE course_revisions
		SET state = 'APPROVED', reviewed_at = $1,
		    reviewed_by_account_id = $2::uuid, updated_at = $1
		WHERE id = $3::uuid
	`, pub.now, pub.actorID, pub.candidate.ID); err != nil {
		return fmt.Errorf("approving candidate revision: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterApprove)
}

func swapLiveRevision(ctx context.Context, tx pgx.Tx, pub *publicationContext) error {
	if _, err := tx.Exec(ctx, `
		UPDATE courses
		SET live_revision_id = $1::uuid, lifecycle = 'PUBLISHED', updated_at = $2
		WHERE id = $3::uuid
	`, pub.candidate.ID, pub.now, pub.courseID); err != nil {
		return fmt.Errorf("updating course live revision pointer: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterPointer)
}

func writePublicationAudit(ctx context.Context, tx pgx.Tx, pub *publicationContext) error {
	actorID := pub.actorID
	action := "COURSE_PUBLISHED"
	reason := "Course revision approved and published"
	if pub.actor == publicationByInstructor {
		action = "COURSE_REVISION_PUBLISHED"
		reason = "Course revision published by its instructor"
	}
	if err := WriteAuditEvent(ctx, tx, AuditEvent{
		ActorAccountID: &actorID, ActorRole: string(pub.actor),
		ActorDescriptor: pub.descriptor,
		Action:          action, TargetType: "COURSE",
		TargetID:       pub.courseID,
		TargetRevision: &pub.candidate.RevisionNumber,
		Reason:         reason,
		Metadata: map[string]any{
			"revision_id":       pub.candidate.ID,
			"first_publication": pub.firstPublication,
		},
	}); err != nil {
		return err
	}
	return failApprovalAt(ctx, approvalAfterAudit)
}

func (r *Repository) writePublicationNotification(
	ctx context.Context,
	tx pgx.Tx,
	pub *publicationContext,
) error {
	writer, err := NewNotificationIntentWriter(r.outboxWriter)
	if err != nil {
		return fmt.Errorf("constructing notification intent writer: %w", err)
	}
	event := outbox.Event{
		Type: "catalog.course_published", SchemaVersion: 1,
		SourceModule: "CATALOG_AND_AUTHORING", AggregateType: "COURSE",
		AggregateID: pub.courseID, AggregateRevision: 1,
		CorrelationID: pub.courseID,
		SafePayload:   map[string]any{"course_id": pub.courseID},
	}
	protected := map[string]any{
		"course_id":         pub.courseID,
		"owner_account_id":  pub.course.OwnerAccountID,
		"revision_id":       pub.candidate.ID,
		"published_at":      pub.now,
		"published_by_role": string(pub.actor),
		"first_publication": pub.firstPublication,
	}
	if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
		return fmt.Errorf("writing publication intent: %w", err)
	}
	return failApprovalAt(ctx, approvalAfterOutbox)
}

func publishedCourse(pub *publicationContext) *Course {
	pub.graph.State = RevisionApproved
	return &Course{
		ID:             pub.courseID,
		OwnerAccountID: pub.course.OwnerAccountID,
		Lifecycle:      LifecyclePublished,
		LiveRevisionID: &pub.candidate.ID,
		CreatedAt:      pub.course.CreatedAt,
		UpdatedAt:      pub.now,
		LiveRevision:   pub.graph,
	}
}
