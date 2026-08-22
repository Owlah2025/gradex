package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ClassificationModel records which classification authority owns a Course's
// academic identity (D-093 §1).
//
// It is explicit rather than inferred because no combination of existing state
// distinguishes the two models. A Course carrying no classification data is the
// normal initial state of the pre-T4 create path, and a subject-less Academic
// draft is a legitimate T4 state, so subject_id nullability describes both.
// Nothing else on the Course is both immutable and classification-bearing.
//
// It is also never taken from a client. The server derives it from whether
// academic context was supplied at creation, so no request shape can move a
// Course between models.
type ClassificationModel string

const (
	// ClassificationLegacyTaxonomy is every Course that predates T4 and every
	// Course created through the pre-T4 path. Its academic identity remains the
	// revision-scoped major_term_id / subject_term_id / study_year columns until
	// T5 migrates it.
	ClassificationLegacyTaxonomy ClassificationModel = "LEGACY_TAXONOMY"

	// ClassificationAcademicCatalog is a Course whose identity is the canonical
	// Academic Catalog: a Course-level Institution and Subject.
	ClassificationAcademicCatalog ClassificationModel = "ACADEMIC_CATALOG"
)

func (m ClassificationModel) Valid() bool {
	switch m {
	case ClassificationLegacyTaxonomy, ClassificationAcademicCatalog:
		return true
	default:
		return false
	}
}

var (
	// ErrLegacyTaxonomyOnAcademicCourse refuses the legacy classification
	// vocabulary on a Course whose identity is the Academic Catalog. Populating
	// legacy terms on an Academic Course to satisfy the old submission gate is
	// exactly the defect the redesign exists to remove.
	ErrLegacyTaxonomyOnAcademicCourse = errors.New("academic catalog course cannot use legacy taxonomy classification")

	// ErrAcademicContextRequired guards an academic command against a Course
	// that is not on the Academic Catalog model.
	ErrAcademicContextRequired = errors.New("course does not use the academic catalog classification model")

	// ErrSubjectImmutable is the domain half of the post-publication Subject
	// lock. The database trigger is the other half.
	ErrSubjectImmutable = errors.New("course has published history; its subject is immutable")

	// ErrSubjectLockedForReview refuses a Subject change while the candidate is
	// held by Admin review.
	ErrSubjectLockedForReview = errors.New("course subject cannot change while a revision is pending review")

	// ErrSubjectUnavailable covers a missing, retired, or cross-Institution
	// Subject at assignment time.
	ErrSubjectUnavailable = errors.New("subject is not available for assignment")

	// ErrInstitutionRequired refuses an Academic Course with no Institution.
	ErrInstitutionRequired = errors.New("academic catalog course requires an institution")
)

// AcademicCourseContext is the semantic input that causes the server to create a
// Course on the Academic Catalog model. The classification model itself is
// deliberately not a field: a caller names the academic context it has, and the
// server decides the model. That is the same shape the lifecycle commands
// already use, where a caller names a command and never a target state.
type AcademicCourseContext struct {
	InstitutionID string
	// SubjectID is optional. An Academic Course may draft without a Subject
	// while the Instructor is still searching, or while a Subject request is
	// pending. Submission for review is what requires one.
	SubjectID *string
}

// SetCourseSubjectRequest assigns or changes the canonical Subject of a Course
// that has never been published.
type SetCourseSubjectRequest struct {
	CourseID        string
	OwnerAccountID  string
	SubjectID       string
	ActorDescriptor string
}

// SetCourseSubject applies the D-093 §5 Subject lifecycle.
//
// The Subject may be set or changed while the Course has never been published
// and no candidate is held in review. Once the Course has published history the
// command refuses, and the database trigger refuses independently, so the lock
// does not depend on this path being the only writer.
func (r *Repository) SetCourseSubject(ctx context.Context, req SetCourseSubjectRequest) (*Course, error) {
	if req.CourseID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if strings.TrimSpace(req.SubjectID) == "" {
		return nil, ErrSubjectUnavailable
	}

	var result Course
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		row, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if row.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		if ClassificationModel(row.ClassificationModel) != ClassificationAcademicCatalog {
			return ErrAcademicContextRequired
		}
		if row.InstitutionID == nil {
			return ErrInstitutionRequired
		}

		// Publication history, read from the row already locked in this
		// transaction, so a concurrent approval cannot slip between the check
		// and the write.
		if row.LiveRevisionID != nil && *row.LiveRevisionID != "" {
			return ErrSubjectImmutable
		}

		// A candidate held in review is frozen. If the Admin requests changes,
		// the revision returns to CHANGES_REQUESTED and the Subject becomes
		// editable again, which is the D-093 §5 correction window.
		var pendingID string
		err = tx.QueryRow(ctx, `
			SELECT id::text FROM course_revisions
			WHERE course_id = $1::uuid AND state = 'PENDING_REVIEW'
			LIMIT 1
			FOR UPDATE
		`, req.CourseID).Scan(&pendingID)
		if err == nil {
			return ErrSubjectLockedForReview
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("checking pending review candidate: %w", err)
		}

		// Only an active Subject in the Course's own Institution may be newly
		// assigned. The composite foreign key already makes a cross-Institution
		// Subject unwritable; this check exists so the caller gets a semantic
		// refusal rather than a constraint violation, and so retirement is
		// enforced at assignment time.
		if err := lockAssignableSubject(ctx, tx, req.SubjectID, *row.InstitutionID); err != nil {
			return err
		}

		now := time.Now().UTC()
		previous := ""
		if row.SubjectID != nil {
			previous = *row.SubjectID
		}
		if _, err := tx.Exec(ctx, `
			UPDATE courses SET subject_id = $1::uuid, updated_at = $2 WHERE id = $3::uuid
		`, req.SubjectID, now, req.CourseID); err != nil {
			return fmt.Errorf("assigning course subject: %w", err)
		}

		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: req.ActorDescriptor,
			action: "COURSE_SUBJECT_ASSIGNED", targetType: "COURSE", targetID: req.CourseID,
			reason: "Course academic Subject selected before first publication",
			metadata: map[string]any{
				"institution_id": *row.InstitutionID,
				"changed":        previous != "",
			},
		}); err != nil {
			return err
		}

		result = courseFromRow(row)
		result.SubjectID = &req.SubjectID
		result.UpdatedAt = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// lockAssignableSubject refuses a Subject that does not exist, is retired, or
// belongs to another Institution.
func lockAssignableSubject(ctx context.Context, tx pgx.Tx, subjectID, institutionID string) error {
	var owningInstitution string
	var retired bool
	err := tx.QueryRow(ctx, `
		SELECT institution_id::text, retired_at IS NOT NULL
		FROM subjects
		WHERE id = $1::uuid
		FOR SHARE
	`, subjectID).Scan(&owningInstitution, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSubjectUnavailable
	}
	if err != nil {
		return fmt.Errorf("locking subject for assignment: %w", err)
	}
	if retired || owningInstitution != institutionID {
		return ErrSubjectUnavailable
	}
	return nil
}

// rejectLegacyTaxonomyOnAcademicCourse is the server-side half of the
// coexistence rule (D-093 §6). An Academic Course must not carry the legacy
// classification vocabulary, and that must not be enforced only by hiding a
// panel: the refusal belongs on every write path that can set those columns.
func rejectLegacyTaxonomyOnAcademicCourse(row *CourseRow, majorTermID, subjectTermID *string, studyYear *StudyYear) error {
	if row == nil || ClassificationModel(row.ClassificationModel) != ClassificationAcademicCatalog {
		return nil
	}
	if majorTermID != nil || subjectTermID != nil || studyYear != nil {
		return ErrLegacyTaxonomyOnAcademicCourse
	}
	return nil
}
