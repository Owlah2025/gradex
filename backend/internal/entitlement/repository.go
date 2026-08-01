package entitlement

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("entitlement evaluation target not found")

// Reader is intentionally read-only. It represents the one authoritative
// evaluation input boundary; no handler queries entitlement rows itself.
type Reader interface {
	Load(context.Context, string, string) (Snapshot, error)
}

// Repository reads Entitlements and the S2 Course graph. It has no exported
// writer: S6 owns the grant transaction.
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("entitlement database is required")
	}
	return &Repository{pool: pool}, nil
}

func (r *Repository) Load(ctx context.Context, studentID, lessonID string) (Snapshot, error) {
	if studentID == "" || lessonID == "" {
		return Snapshot{}, ErrNotFound
	}
	var snapshot Snapshot
	err := r.pool.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, cl.course_id::text, cl.section_identity_id::text,
		       a.status::text, c.access_suspended_at IS NOT NULL, c.retired_at,
		       csi.retired_at, cli.retired_at
		FROM course_lessons cl
		JOIN courses c ON c.id = cl.course_id
		JOIN course_section_identities csi ON csi.id = cl.section_identity_id AND csi.course_id = cl.course_id
		JOIN course_lesson_identities cli ON cli.id = cl.lesson_identity_id AND cli.course_id = cl.course_id
		JOIN accounts a ON a.id = $1::uuid
		WHERE cl.lesson_identity_id = $2::uuid
	`, studentID, lessonID).Scan(
		&snapshot.Lesson.ID, &snapshot.Lesson.CourseID, &snapshot.Lesson.SectionID,
		&snapshot.Lesson.AccountStatus, &snapshot.Lesson.CourseSuspended, &snapshot.Lesson.RetiredAt,
		&snapshot.Lesson.SectionRetiredAt, &snapshot.Lesson.LessonRetiredAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("loading entitlement evaluation graph: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
		       grant_source, source_invitation_id::text, original_access_ends_at,
		       access_ends_at, revoked_at, retirement_eligibility_at, state,
		       created_at, updated_at
		FROM entitlements
		WHERE student_account_id = $1::uuid AND course_id = $2::uuid
		ORDER BY created_at, id
	`, studentID, snapshot.Lesson.CourseID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("loading entitlement records: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record Record
		if err := rows.Scan(
			&record.ID, &record.StudentAccountID, &record.ScopeKind, &record.ScopeID, &record.CourseID,
			&record.GrantSource, &record.SourceInvitationID, &record.OriginalAccessEndsAt,
			&record.AccessEndsAt, &record.RevokedAt, &record.RetirementEligibilityAt, &record.State,
			&record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return Snapshot{}, fmt.Errorf("scanning entitlement record: %w", err)
		}
		snapshot.Entitlements = append(snapshot.Entitlements, record)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("iterating entitlement records: %w", err)
	}
	return snapshot, nil
}
