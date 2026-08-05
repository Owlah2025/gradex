package entitlement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("entitlement evaluation target not found")

// Reader is intentionally read-only. It represents the one authoritative
// evaluation input boundary; no handler queries entitlement rows itself.
type Reader interface {
	Load(context.Context, string, string) (Snapshot, error)
}

// queryer is the deliberately small read boundary shared by a pool and a
// transaction. It lets the final learning authorization observe and lock the
// same authoritative rows as its following Progress mutation.
type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Repository reads Entitlements and the S2 Course graph. It has no exported
// writer: S6 owns the grant transaction.
type Repository struct {
	query         queryer
	lockAuthority bool
	observe       func(string)
}

func NewRepository(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("entitlement database is required")
	}
	return &Repository{query: pool}, nil
}

// NewRepositoryWithQueryObserver is a deterministic test seam for asserting
// that read-model authority classification remains bulk and bounded.
func NewRepositoryWithQueryObserver(pool *pgxpool.Pool, observe func(string)) (*Repository, error) {
	repository, err := NewRepository(pool)
	if err != nil {
		return nil, err
	}
	repository.observe = observe
	return repository, nil
}

func (r *Repository) observeQuery(name string) {
	if r != nil && r.observe != nil {
		r.observe(name)
	}
}

// readerFor is intentionally package-private. Evaluator uses it to bind its
// unchanged S4 decision logic to the transaction that will write Progress.
// The locking variant makes a concurrent access-state mutation serialize
// before or after that final decision and mutation, never between them.
func (r *Repository) readerFor(tx pgx.Tx) Reader {
	if r == nil || tx == nil {
		return nil
	}
	return &Repository{query: tx, lockAuthority: true, observe: r.observe}
}

func (r *Repository) Load(ctx context.Context, studentID, lessonID string) (Snapshot, error) {
	if studentID == "" || lessonID == "" {
		return Snapshot{}, ErrNotFound
	}
	var snapshot Snapshot
	if r == nil || r.query == nil {
		return Snapshot{}, ErrNotFound
	}
	lockClause := ""
	if r.lockAuthority {
		lockClause = " FOR SHARE OF cl, c, csi, cli, a"
	}
	r.observeQuery("entitlement.read")
	err := r.query.QueryRow(ctx, `
		SELECT cl.lesson_identity_id::text, cl.course_id::text, cl.section_identity_id::text,
		       a.status::text, c.access_suspended_at IS NOT NULL, c.retired_at,
		       csi.retired_at, cli.retired_at
		FROM course_lessons cl
		JOIN courses c ON c.id = cl.course_id
		JOIN course_section_identities csi ON csi.id = cl.section_identity_id AND csi.course_id = cl.course_id
		JOIN course_lesson_identities cli ON cli.id = cl.lesson_identity_id AND cli.course_id = cl.course_id
		JOIN accounts a ON a.id = $1::uuid
		WHERE cl.lesson_identity_id = $2::uuid`+lockClause,
		studentID, lessonID).Scan(
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
	if r.lockAuthority {
		lockClause = " FOR SHARE"
	} else {
		lockClause = ""
	}
	r.observeQuery("entitlement.read")
	rows, err := r.query.Query(ctx, `
		SELECT id::text, student_account_id::text, scope_kind, scope_id::text, course_id::text,
		       grant_source, source_invitation_id::text, original_access_ends_at,
		       access_ends_at, revoked_at, retirement_eligibility_at, state,
		       created_at, updated_at
		FROM entitlements
		WHERE student_account_id = $1::uuid AND course_id = $2::uuid
		ORDER BY created_at, id`+lockClause,
		studentID, snapshot.Lesson.CourseID)
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

// LoadCourseReadSnapshots is the bulk S4-owned authority boundary for the
// Dashboard. It selects one deterministic current live Lesson per enrolled
// Course and all of that Course's Entitlements in one query. The evaluator,
// not S5, applies scope, expiry, suspension, and retirement policy.
func (r *Repository) LoadCourseReadSnapshots(ctx context.Context, studentID string) ([]CourseReadSnapshot, error) {
	if r == nil || r.query == nil || studentID == "" {
		return nil, ErrNotFound
	}
	r.observeQuery("entitlement.dashboard")
	rows, err := r.query.Query(ctx, `
		SELECT e.course_id::text, e.created_at,
		       target.lesson_identity_id::text, target.course_id::text, target.section_identity_id::text,
		       a.status::text, target.course_suspended, target.course_retired_at,
		       target.section_retired_at, target.lesson_retired_at,
		       ent.id::text, ent.student_account_id::text, ent.scope_kind, ent.scope_id::text,
		       ent.course_id::text, ent.grant_source, ent.source_invitation_id::text,
		       ent.original_access_ends_at, ent.access_ends_at, ent.revoked_at,
		       ent.retirement_eligibility_at, ent.state, ent.created_at, ent.updated_at
		FROM enrollments e
		JOIN accounts a ON a.id = e.student_account_id
		JOIN courses c ON c.id = e.course_id
		JOIN LATERAL (
			SELECT cl.lesson_identity_id, cl.course_id, cl.section_identity_id,
			       c.access_suspended_at IS NOT NULL AS course_suspended,
			       c.retired_at AS course_retired_at,
			       csi.retired_at AS section_retired_at,
			       cli.retired_at AS lesson_retired_at
			FROM course_revisions cr
			JOIN course_sections cs ON cs.revision_id = cr.id AND cs.course_id = c.id
			JOIN course_section_identities csi
			  ON csi.id = cs.section_identity_id AND csi.course_id = c.id
			JOIN course_lessons cl
			  ON cl.section_id = cs.id AND cl.course_id = c.id
			JOIN course_lesson_identities cli
			  ON cli.id = cl.lesson_identity_id AND cli.course_id = c.id
			WHERE cr.id = c.live_revision_id AND cr.course_id = c.id AND cr.state = 'APPROVED'
			ORDER BY cs.position ASC, cs.id ASC, cl.position ASC, cl.id ASC
			LIMIT 1
		) target ON TRUE
		LEFT JOIN entitlements ent
		  ON ent.student_account_id = e.student_account_id AND ent.course_id = e.course_id
		WHERE e.student_account_id = $1::uuid
		ORDER BY e.created_at DESC, e.course_id ASC, ent.created_at ASC NULLS LAST, ent.id ASC NULLS LAST
	`, studentID)
	if err != nil {
		return nil, fmt.Errorf("loading course read snapshots: %w", err)
	}
	defer rows.Close()

	snapshots := make([]CourseReadSnapshot, 0)
	for rows.Next() {
		var courseID, lessonID, lessonCourseID, sectionID, accountStatus string
		var enrollmentCreatedAt time.Time
		var courseSuspended bool
		var courseRetiredAt, sectionRetiredAt, lessonRetiredAt *time.Time
		var entitlementID, entitlementStudentID, scopeID, entitlementCourseID, sourceInvitationID *string
		var scopeKind *ScopeKind
		var grantSource *GrantSource
		var originalAccessEndsAt, accessEndsAt, revokedAt, retirementEligibilityAt, entitlementCreatedAt, entitlementUpdatedAt *time.Time
		var state *State
		if err := rows.Scan(
			&courseID, &enrollmentCreatedAt,
			&lessonID, &lessonCourseID, &sectionID, &accountStatus, &courseSuspended, &courseRetiredAt,
			&sectionRetiredAt, &lessonRetiredAt,
			&entitlementID, &entitlementStudentID, &scopeKind, &scopeID, &entitlementCourseID,
			&grantSource, &sourceInvitationID, &originalAccessEndsAt, &accessEndsAt, &revokedAt,
			&retirementEligibilityAt, &state, &entitlementCreatedAt, &entitlementUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning course read snapshot: %w", err)
		}
		if len(snapshots) == 0 || snapshots[len(snapshots)-1].CourseID != courseID {
			snapshots = append(snapshots, CourseReadSnapshot{
				CourseID: courseID, EnrollmentCreatedAt: enrollmentCreatedAt,
				Lesson: Lesson{ID: lessonID, CourseID: lessonCourseID, SectionID: sectionID,
					AccountStatus: accountStatus, CourseSuspended: courseSuspended,
					RetiredAt: courseRetiredAt, SectionRetiredAt: sectionRetiredAt, LessonRetiredAt: lessonRetiredAt},
			})
		}
		if entitlementID == nil {
			continue
		}
		if entitlementStudentID == nil || scopeKind == nil || scopeID == nil || entitlementCourseID == nil ||
			grantSource == nil || originalAccessEndsAt == nil || accessEndsAt == nil || retirementEligibilityAt == nil ||
			state == nil || entitlementCreatedAt == nil || entitlementUpdatedAt == nil {
			return nil, fmt.Errorf("incomplete entitlement authority for course %s", courseID)
		}
		snapshots[len(snapshots)-1].Entitlements = append(snapshots[len(snapshots)-1].Entitlements, Record{
			ID: *entitlementID, StudentAccountID: *entitlementStudentID, ScopeKind: *scopeKind, ScopeID: *scopeID,
			CourseID: *entitlementCourseID, GrantSource: *grantSource, SourceInvitationID: sourceInvitationID,
			OriginalAccessEndsAt: *originalAccessEndsAt, AccessEndsAt: *accessEndsAt, RevokedAt: revokedAt,
			RetirementEligibilityAt: *retirementEligibilityAt, State: *state,
			CreatedAt: *entitlementCreatedAt, UpdatedAt: *entitlementUpdatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating course read snapshots: %w", err)
	}
	return snapshots, nil
}
