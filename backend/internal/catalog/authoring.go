package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

type DBAssetVersionValidator struct {
	queryer interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func NewDBAssetVersionValidator(pool *pgxpool.Pool) *DBAssetVersionValidator {
	return &DBAssetVersionValidator{queryer: pool}
}

func newTxAssetVersionValidator(tx pgx.Tx) *DBAssetVersionValidator {
	return &DBAssetVersionValidator{queryer: tx}
}

type candidateMutationHold struct {
	reached chan<- struct{}
	release <-chan struct{}
}

type candidateMutationHoldKey struct{}

func withCandidateMutationHold(ctx context.Context, hold candidateMutationHold) context.Context {
	return context.WithValue(ctx, candidateMutationHoldKey{}, hold)
}

func holdCandidateMutationAfterCourseLock(ctx context.Context) error {
	hold, ok := ctx.Value(candidateMutationHoldKey{}).(candidateMutationHold)
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

func (v *DBAssetVersionValidator) ValidateAssetVersion(ctx context.Context, assetVersionID string) error {
	if assetVersionID == "" {
		return ErrAssetVersionInvalid
	}
	if v == nil || v.queryer == nil {
		return errors.New("asset version validator database queryer is required")
	}

	var status string
	err := v.queryer.QueryRow(ctx, `SELECT status FROM videos WHERE id = $1::uuid`, assetVersionID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetVersionInvalid
	}
	if err != nil {
		return fmt.Errorf("checking asset version: %w", err)
	}

	if status != "READY" && status != "PUBLISHED" {
		return ErrAssetVersionNotReady
	}
	return nil
}

type CreateCourseRequest struct {
	OwnerAccountID string
	TitleAr        string
	TitleEn        string
	DescriptionAr  string
	DescriptionEn  string
}

type UpdateRevisionRequest struct {
	CourseID              string
	RevisionID            string
	OwnerAccountID        string
	TitleAr               string
	TitleEn               string
	DescriptionAr         string
	DescriptionEn         string
	MajorTermID           *string
	SubjectTermID         *string
	StudyYear             *StudyYear
	PreviewAssetVersionID *string
}

type AddSectionRequest struct {
	CourseID       string
	RevisionID     string
	OwnerAccountID string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type UpdateSectionRequest struct {
	CourseID       string
	RevisionID     string
	SectionID      string
	OwnerAccountID string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type DeleteSectionRequest struct {
	CourseID       string
	RevisionID     string
	SectionID      string
	OwnerAccountID string
}

type AddLessonRequest struct {
	CourseID       string
	RevisionID     string
	SectionID      string
	OwnerAccountID string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type UpdateLessonRequest struct {
	CourseID       string
	RevisionID     string
	LessonID       string
	OwnerAccountID string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type DeleteLessonRequest struct {
	CourseID       string
	RevisionID     string
	LessonID       string
	OwnerAccountID string
}

type SetVideoRequest struct {
	CourseID            string
	RevisionID          string
	LessonID            string
	VideoAssetVersionID string
	OwnerAccountID      string
}

type LessonFileRequest struct {
	CourseID       string
	RevisionID     string
	LessonID       string
	Kind           LessonFileKind
	AssetVersionID string
	DisplayNameAr  string
	DisplayNameEn  string
	Position       *int
	OwnerAccountID string
}

type DeleteLessonFileRequest struct {
	CourseID       string
	RevisionID     string
	LessonID       string
	FileID         string
	OwnerAccountID string
}

type PreviewAssetRequest struct {
	CourseID              string
	RevisionID            string
	PreviewAssetVersionID string
	OwnerAccountID        string
}

type ClearPreviewAssetRequest struct {
	CourseID       string
	RevisionID     string
	OwnerAccountID string
}

type SubmitCourseRequest struct {
	CourseID        string
	RevisionID      string
	OwnerAccountID  string
	ActorDescriptor string
}

type instructorAuditRequest struct {
	accountID       string
	actorDescriptor string
	action          string
	targetType      string
	targetID        string
	reason          string
	metadata        map[string]any
}

func writeInstructorAudit(ctx context.Context, tx pgx.Tx, req instructorAuditRequest) error {
	return WriteAuditEvent(ctx, tx, AuditEvent{
		ActorAccountID:  &req.accountID,
		ActorRole:       "INSTRUCTOR",
		ActorDescriptor: req.actorDescriptor,
		Action:          req.action,
		TargetType:      req.targetType,
		TargetID:        req.targetID,
		Reason:          req.reason,
		Metadata:        req.metadata,
	})
}

func (r *Repository) checkOwnerActive(ctx context.Context, tx pgx.Tx, ownerAccountID string) error {
	var role, status string
	err := tx.QueryRow(ctx, `SELECT role, status FROM accounts WHERE id = $1::uuid`, ownerAccountID).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("owner account not found")
	}
	if err != nil {
		return fmt.Errorf("checking owner account status: %w", err)
	}
	if status == "SUSPENDED" {
		return ErrAccountSuspended
	}
	if role != "INSTRUCTOR" || status != "ACTIVE" {
		return ErrOwnerIneligible
	}
	return nil
}

func (r *Repository) LockCandidate(
	ctx context.Context,
	tx pgx.Tx,
	courseID string,
	revisionID string,
) (*CourseRevision, error) {
	if tx == nil {
		return nil, errors.New("transaction is required")
	}
	if courseID == "" || revisionID == "" {
		return nil, ErrCourseNotFound
	}

	var rev CourseRevision
	query := `
		SELECT id, course_id, based_on_revision_id, state, revision_number,
		       title_ar, title_en, description_ar, description_en,
		       major_term_id, subject_term_id, study_year, preview_asset_version_id,
		       submitted_at, reviewed_at, reviewed_by_account_id, review_reason, created_at, updated_at
		FROM course_revisions
		WHERE id = $1::uuid AND course_id = $2::uuid
		FOR UPDATE
	`
	err := tx.QueryRow(ctx, query, revisionID, courseID).Scan(
		&rev.ID, &rev.CourseID, &rev.BasedOnRevisionID, &rev.State, &rev.RevisionNumber,
		&rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID,
		&rev.SubmittedAt, &rev.ReviewedAt, &rev.ReviewedByAccountID, &rev.ReviewReason,
		&rev.CreatedAt, &rev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("locking revision %s: %w", revisionID, err)
	}

	if rev.State != RevisionDraft && rev.State != RevisionChangesRequested {
		return nil, &LifecycleConflictError{
			CourseID: courseID,
			Actual:   string(rev.State),
			Expected: []string{string(RevisionDraft), string(RevisionChangesRequested)},
		}
	}

	return &rev, nil
}

func (r *Repository) CreateCourse(ctx context.Context, req CreateCourseRequest, actorDescriptor string) (*Course, error) {
	if req.OwnerAccountID == "" {
		return nil, errors.New("owner account ID is required")
	}
	if len(req.TitleAr) == 0 || len(req.TitleEn) == 0 {
		return nil, errors.New("title_ar and title_en are required")
	}

	var course Course
	var revision CourseRevision

	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		queryCourse := `
			INSERT INTO courses (owner_account_id, lifecycle)
			VALUES ($1::uuid, 'DRAFT')
			RETURNING id, owner_account_id, lifecycle, created_at, updated_at
		`
		err := tx.QueryRow(ctx, queryCourse, req.OwnerAccountID).Scan(
			&course.ID, &course.OwnerAccountID, &course.Lifecycle,
			&course.CreatedAt, &course.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting course: %w", err)
		}

		queryRev := `
			INSERT INTO course_revisions (
				course_id, state, revision_number, title_ar, title_en, description_ar, description_en
			) VALUES (
				$1::uuid, 'DRAFT', 1, $2, $3, $4, $5
			)
			RETURNING id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en, created_at, updated_at
		`
		err = tx.QueryRow(ctx, queryRev, course.ID, req.TitleAr, req.TitleEn, req.DescriptionAr, req.DescriptionEn).Scan(
			&revision.ID, &revision.CourseID, &revision.State, &revision.RevisionNumber,
			&revision.TitleAr, &revision.TitleEn, &revision.DescriptionAr, &revision.DescriptionEn,
			&revision.CreatedAt, &revision.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting revision: %w", err)
		}

		course.EditableRevision = &revision

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "COURSE_CREATED",
			TargetType:      "COURSE",
			TargetID:        course.ID,
			Reason:          "Course created in DRAFT",
			Metadata:        map[string]any{"title_en": req.TitleEn},
		}
		if err := WriteAuditEvent(ctx, tx, audit); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}
	return &course, nil
}

func (r *Repository) CreateCandidate(
	ctx context.Context,
	courseID string,
	ownerAccountID string,
	actorDescriptor string,
) (*CourseRevision, error) {
	if courseID == "" || ownerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var candidate *CourseRevision

	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		// Check if active candidate already exists
		var activeRevID string
		err = tx.QueryRow(ctx, `
			SELECT id FROM course_revisions
			WHERE course_id = $1::uuid AND state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
			LIMIT 1
			FOR UPDATE
		`, courseID).Scan(&activeRevID)

		if err == nil {
			rev, err := r.loadRevisionGraphByIDTx(ctx, tx, activeRevID)
			if err != nil {
				return err
			}
			candidate = rev
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("checking active candidate: %w", err)
		}

		// If course is not published and has no active candidate (edge case), return error
		if courseRow.LiveRevisionID == nil || *courseRow.LiveRevisionID == "" {
			return fmt.Errorf("course has no live revision to clone")
		}

		liveRevID := *courseRow.LiveRevisionID
		liveRev, err := r.loadRevisionGraphByIDTx(ctx, tx, liveRevID)
		if err != nil || liveRev == nil {
			return fmt.Errorf("loading live revision %s: %w", liveRevID, err)
		}

		var maxRevNum int
		err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(revision_number), 0) FROM course_revisions WHERE course_id = $1::uuid`, courseID).Scan(&maxRevNum)
		if err != nil {
			return fmt.Errorf("querying max revision number: %w", err)
		}
		nextRevNum := maxRevNum + 1

		var newRevID string
		now := time.Now().UTC()
		queryRev := `
			INSERT INTO course_revisions (
				course_id, based_on_revision_id, state, revision_number,
				title_ar, title_en, description_ar, description_en,
				major_term_id, subject_term_id, study_year, preview_asset_version_id,
				created_at, updated_at
			) VALUES (
				$1::uuid, $2::uuid, 'DRAFT', $3,
				$4, $5, $6, $7,
				$8, $9, $10, $11,
				$12, $12
			)
			RETURNING id
		`
		err = tx.QueryRow(ctx, queryRev,
			courseID, liveRevID, nextRevNum,
			liveRev.TitleAr, liveRev.TitleEn, liveRev.DescriptionAr, liveRev.DescriptionEn,
			liveRev.MajorTermID, liveRev.SubjectTermID, liveRev.StudyYear, liveRev.PreviewAssetVersionID,
			now,
		).Scan(&newRevID)
		if err != nil {
			return fmt.Errorf("inserting candidate revision: %w", err)
		}

		for _, sec := range liveRev.Sections {
			var newSecID string
			err = tx.QueryRow(ctx, `
				INSERT INTO course_sections (
					revision_id, course_id, section_identity_id,
					title_ar, title_en, position, price_minor_units, created_at, updated_at
				) VALUES (
					$1::uuid, $2::uuid, $3::uuid,
					$4, $5, $6, $7, $8, $8
				) RETURNING id
			`, newRevID, courseID, sec.SectionIdentityID, sec.TitleAr, sec.TitleEn, sec.Position, sec.PriceMinorUnits, now).Scan(&newSecID)
			if err != nil {
				return fmt.Errorf("cloning section %s: %w", sec.ID, err)
			}

			for _, les := range sec.Lessons {
				var newLesID string
				err = tx.QueryRow(ctx, `
					INSERT INTO course_lessons (
						section_id, course_id, section_identity_id, lesson_identity_id,
						title_ar, title_en, position, video_asset_version_id, created_at, updated_at
					) VALUES (
						$1::uuid, $2::uuid, $3::uuid, $4::uuid,
						$5, $6, $7, $8, $9, $9
					) RETURNING id
				`, newSecID, courseID, sec.SectionIdentityID, les.LessonIdentityID, les.TitleAr, les.TitleEn, les.Position, les.VideoAssetVersionID, now).Scan(&newLesID)
				if err != nil {
					return fmt.Errorf("cloning lesson %s: %w", les.ID, err)
				}

				for _, f := range les.Files {
					_, err = tx.Exec(ctx, `
						INSERT INTO lesson_files (
							lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position, created_at, updated_at
						) VALUES (
							$1::uuid, $2, $3::uuid, $4, $5, $6, $7, $7
						)
					`, newLesID, f.Kind, f.AssetVersionID, f.DisplayNameAr, f.DisplayNameEn, f.Position, now)
					if err != nil {
						return fmt.Errorf("cloning lesson file %s: %w", f.ID, err)
					}
				}
			}
		}

		rev, err := r.loadRevisionGraphByIDTx(ctx, tx, newRevID)
		if err != nil {
			return err
		}
		candidate = rev
		return nil
	})

	if err != nil {
		return nil, err
	}
	return candidate, nil
}

func (r *Repository) ListOwnedCourses(ctx context.Context, ownerAccountID string) ([]Course, error) {
	if ownerAccountID == "" {
		return nil, errors.New("owner account ID is required")
	}

	query := `
		SELECT c.id, c.owner_account_id, c.lifecycle, c.live_revision_id,
		       c.access_suspended_at, c.access_suspension_reason, c.retired_at,
		       cpc.new_value_minor_units AS price_minor_units,
		       c.created_at, c.updated_at
		FROM courses c
		LEFT JOIN LATERAL (
			SELECT new_value_minor_units
			FROM course_price_changes
			WHERE course_id = c.id AND section_id IS NULL
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		) cpc ON TRUE
		WHERE c.owner_account_id = $1::uuid
		ORDER BY c.updated_at DESC
	`

	rows, err := r.pool.Query(ctx, query, ownerAccountID)
	if err != nil {
		return nil, fmt.Errorf("querying owned courses: %w", err)
	}
	defer rows.Close()

	var result []Course
	for rows.Next() {
		var c Course
		if err := rows.Scan(
			&c.ID, &c.OwnerAccountID, &c.Lifecycle, &c.LiveRevisionID,
			&c.AccessSuspendedAt, &c.AccessSuspensionReason, &c.RetiredAt,
			&c.PriceMinorUnits,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning course: %w", err)
		}

		var activeRevID string
		err = r.pool.QueryRow(ctx, `
			SELECT id FROM course_revisions
			WHERE course_id = $1::uuid AND state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
			LIMIT 1
		`, c.ID).Scan(&activeRevID)
		if err == nil {
			rev, err := r.loadRevisionGraphByID(ctx, activeRevID)
			if err != nil {
				return nil, fmt.Errorf("loading editable revision %s: %w", activeRevID, err)
			}
			if rev == nil {
				return nil, fmt.Errorf("editable revision %s is missing", activeRevID)
			}
			c.EditableRevision = rev
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("querying active revision for course %s: %w", c.ID, err)
		}

		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading owned courses: %w", err)
	}
	if result == nil {
		result = []Course{}
	}
	return result, nil
}

func (r *Repository) GetLiveCourseGraph(ctx context.Context, courseID string) (*Course, error) {
	if courseID == "" {
		return nil, ErrCourseNotFound
	}

	query := `
		SELECT id, owner_account_id, lifecycle, live_revision_id,
		       access_suspended_at, access_suspension_reason, retired_at,
		       created_at, updated_at
		FROM courses
		WHERE id = $1::uuid
	`
	var c Course
	err := r.pool.QueryRow(ctx, query, courseID).Scan(
		&c.ID, &c.OwnerAccountID, &c.Lifecycle, &c.LiveRevisionID,
		&c.AccessSuspendedAt, &c.AccessSuspensionReason, &c.RetiredAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting course: %w", err)
	}

	if c.LiveRevisionID != nil && *c.LiveRevisionID != "" {
		liveRevID := *c.LiveRevisionID
		liveRev, err := r.loadRevisionGraphByID(ctx, liveRevID)
		if err != nil {
			return nil, fmt.Errorf("loading live revision %s: %w", liveRevID, err)
		}
		if liveRev == nil {
			return nil, fmt.Errorf("live revision %s is missing", liveRevID)
		}
		c.LiveRevision = liveRev
	}

	return &c, nil
}

func (r *Repository) GetOwnedCourse(ctx context.Context, courseID, ownerAccountID string) (*Course, error) {
	if courseID == "" || ownerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	query := `
		SELECT id, owner_account_id, lifecycle, live_revision_id,
		       access_suspended_at, access_suspension_reason, retired_at,
		       created_at, updated_at
		FROM courses
		WHERE id = $1::uuid AND owner_account_id = $2::uuid
	`
	var c Course
	err := r.pool.QueryRow(ctx, query, courseID, ownerAccountID).Scan(
		&c.ID, &c.OwnerAccountID, &c.Lifecycle, &c.LiveRevisionID,
		&c.AccessSuspendedAt, &c.AccessSuspensionReason, &c.RetiredAt,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCourseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting course: %w", err)
	}

	var coursePrice *int64
	err = r.pool.QueryRow(ctx, `
		SELECT new_value_minor_units
		FROM course_price_changes
		WHERE course_id = $1::uuid AND section_id IS NULL
		ORDER BY changed_at DESC, id DESC
		LIMIT 1
	`, courseID).Scan(&coursePrice)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("getting course price: %w", err)
	}
	c.PriceMinorUnits = coursePrice

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

	var activeRevID string
	err = r.pool.QueryRow(ctx, `
		SELECT id FROM course_revisions
		WHERE course_id = $1::uuid AND state IN ('DRAFT', 'CHANGES_REQUESTED', 'PENDING_REVIEW')
		LIMIT 1
	`, courseID).Scan(&activeRevID)
	if err == nil {
		candidate, err := r.loadRevisionGraphByID(ctx, activeRevID)
		if err != nil {
			return nil, fmt.Errorf("loading editable revision %s: %w", activeRevID, err)
		}
		if candidate == nil {
			return nil, fmt.Errorf("editable revision %s is missing", activeRevID)
		}
		c.EditableRevision = candidate
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("querying active revision for course %s: %w", courseID, err)
	}

	return &c, nil
}

type queryExecer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func loadRevisionGraphBatch(ctx context.Context, q queryExecer, rev *CourseRevision) error {
	if rev == nil {
		return nil
	}

	secQuery := `
		SELECT cs.id, cs.revision_id, cs.course_id, cs.section_identity_id, cs.title_ar, cs.title_en, cs.position,
		       cpc.new_value_minor_units AS price_minor_units, cs.created_at, cs.updated_at
		FROM course_sections cs
		LEFT JOIN LATERAL (
			SELECT new_value_minor_units
			FROM course_price_changes
			WHERE course_id = cs.course_id AND section_id = cs.section_identity_id
			ORDER BY changed_at DESC, id DESC
			LIMIT 1
		) cpc ON TRUE
		WHERE cs.revision_id = $1::uuid
		ORDER BY cs.position ASC
	`
	rows, err := q.Query(ctx, secQuery, rev.ID)
	if err != nil {
		return fmt.Errorf("querying sections: %w", err)
	}

	var sections []Section
	var sectionIDs []string
	for rows.Next() {
		var s Section
		if err := rows.Scan(&s.ID, &s.RevisionID, &s.CourseID, &s.SectionIdentityID, &s.TitleAr, &s.TitleEn, &s.Position, &s.PriceMinorUnits, &s.CreatedAt, &s.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		s.Lessons = []Lesson{}
		sections = append(sections, s)
		sectionIDs = append(sectionIDs, s.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading sections: %w", err)
	}

	if len(sections) == 0 {
		rev.Sections = []Section{}
		return nil
	}

	lesQuery := `
		SELECT id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id, created_at, updated_at
		FROM course_lessons
		WHERE section_id = ANY($1::uuid[])
		ORDER BY position ASC
	`
	lRows, err := q.Query(ctx, lesQuery, sectionIDs)
	if err != nil {
		return fmt.Errorf("querying lessons: %w", err)
	}

	var lessonIDs []string
	lessonsBySectionID := make(map[string][]Lesson)
	for lRows.Next() {
		var l Lesson
		if err := lRows.Scan(&l.ID, &l.SectionID, &l.CourseID, &l.SectionIdentityID, &l.LessonIdentityID, &l.TitleAr, &l.TitleEn, &l.Position, &l.VideoAssetVersionID, &l.CreatedAt, &l.UpdatedAt); err != nil {
			lRows.Close()
			return err
		}
		l.Files = []LessonFile{}
		lessonsBySectionID[l.SectionID] = append(lessonsBySectionID[l.SectionID], l)
		lessonIDs = append(lessonIDs, l.ID)
	}
	lRows.Close()
	if err := lRows.Err(); err != nil {
		return fmt.Errorf("reading lessons: %w", err)
	}

	filesByLessonID := make(map[string][]LessonFile)
	if len(lessonIDs) > 0 {
		fQuery := `
			SELECT id, lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position, created_at, updated_at
			FROM lesson_files
			WHERE lesson_id = ANY($1::uuid[])
			ORDER BY position ASC
		`
		fRows, err := q.Query(ctx, fQuery, lessonIDs)
		if err != nil {
			return fmt.Errorf("querying lesson files: %w", err)
		}
		for fRows.Next() {
			var f LessonFile
			if err := fRows.Scan(&f.ID, &f.LessonID, &f.Kind, &f.AssetVersionID, &f.DisplayNameAr, &f.DisplayNameEn, &f.Position, &f.CreatedAt, &f.UpdatedAt); err != nil {
				fRows.Close()
				return err
			}
			filesByLessonID[f.LessonID] = append(filesByLessonID[f.LessonID], f)
		}
		fRows.Close()
		if err := fRows.Err(); err != nil {
			return fmt.Errorf("reading lesson files: %w", err)
		}
	}

	for secIdx, sec := range sections {
		secLessons := lessonsBySectionID[sec.ID]
		if secLessons == nil {
			secLessons = []Lesson{}
		}
		for lesIdx, les := range secLessons {
			files := filesByLessonID[les.ID]
			if files == nil {
				files = []LessonFile{}
			}
			secLessons[lesIdx].Files = files
		}
		sections[secIdx].Lessons = secLessons
	}

	rev.Sections = sections
	return nil
}

func (r *Repository) UpdateCourseRevision(
	ctx context.Context,
	validator AssetVersionValidator,
	req UpdateRevisionRequest,
	actorDescriptor string,
) (*CourseRevision, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
	}

	if req.PreviewAssetVersionID != nil && *req.PreviewAssetVersionID != "" {
		if err := validator.ValidateAssetVersion(ctx, *req.PreviewAssetVersionID); err != nil {
			return nil, err
		}
	}

	var updatedRev *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		if err := holdCandidateMutationAfterCourseLock(ctx); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		titleAr := rev.TitleAr
		if req.TitleAr != "" {
			titleAr = req.TitleAr
		}
		titleEn := rev.TitleEn
		if req.TitleEn != "" {
			titleEn = req.TitleEn
		}
		descAr := rev.DescriptionAr
		if req.DescriptionAr != "" {
			descAr = req.DescriptionAr
		}
		descEn := rev.DescriptionEn
		if req.DescriptionEn != "" {
			descEn = req.DescriptionEn
		}

		majorTermID := rev.MajorTermID
		if req.MajorTermID != nil {
			majorTermID = req.MajorTermID
		}
		subjectTermID := rev.SubjectTermID
		if req.SubjectTermID != nil {
			subjectTermID = req.SubjectTermID
		}
		studyYear := rev.StudyYear
		if req.StudyYear != nil {
			studyYear = req.StudyYear
		}
		previewAssetVersionID := rev.PreviewAssetVersionID
		if req.PreviewAssetVersionID != nil {
			previewAssetVersionID = req.PreviewAssetVersionID
		}

		now := time.Now().UTC()
		query := `
			UPDATE course_revisions
			SET title_ar = $1, title_en = $2, description_ar = $3, description_en = $4,
			    major_term_id = $5::uuid, subject_term_id = $6::uuid, study_year = $7,
			    preview_asset_version_id = $8::uuid, updated_at = $9
			WHERE id = $10::uuid AND course_id = $11::uuid
		`
		_, err = tx.Exec(ctx, query,
			titleAr, titleEn, descAr, descEn,
			majorTermID, subjectTermID, studyYear, previewAssetVersionID,
			now, rev.ID, req.CourseID,
		)
		if err != nil {
			return fmt.Errorf("updating course revision: %w", err)
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "COURSE_REVISION_UPDATED", targetType: "COURSE_REVISION", targetID: rev.ID,
			reason: "Course metadata updated", metadata: map[string]any{"course_id": req.CourseID},
		}); err != nil {
			return err
		}

		res, err := r.loadRevisionGraphByIDTx(ctx, tx, rev.ID)
		if err != nil {
			return err
		}
		updatedRev = res
		return nil
	})

	if err != nil {
		return nil, err
	}
	return updatedRev, nil
}

func (r *Repository) AddSection(
	ctx context.Context,
	req AddSectionRequest,
	actorDescriptor string,
) (*Section, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var sec Section
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		var pos int
		if req.Position != nil {
			pos = *req.Position
		} else {
			err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM course_sections WHERE revision_id = $1::uuid`, rev.ID).Scan(&pos)
			if err != nil {
				return fmt.Errorf("querying max section position: %w", err)
			}
		}

		// Create new stable section identity
		var secIdentityID string
		err = tx.QueryRow(ctx, `INSERT INTO course_section_identities (course_id) VALUES ($1::uuid) RETURNING id`, req.CourseID).Scan(&secIdentityID)
		if err != nil {
			return fmt.Errorf("creating section identity: %w", err)
		}

		now := time.Now().UTC()
		query := `
			INSERT INTO course_sections (revision_id, course_id, section_identity_id, title_ar, title_en, position, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $7)
			RETURNING id, revision_id, course_id, section_identity_id, title_ar, title_en, position, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, rev.ID, req.CourseID, secIdentityID, req.TitleAr, req.TitleEn, pos, now).Scan(
			&sec.ID, &sec.RevisionID, &sec.CourseID, &sec.SectionIdentityID, &sec.TitleAr, &sec.TitleEn, &sec.Position, &sec.CreatedAt, &sec.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting section: %w", err)
		}
		sec.Lessons = []Lesson{}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "SECTION_CREATED", targetType: "SECTION", targetID: sec.SectionIdentityID,
			reason:   "Section created",
			metadata: map[string]any{"course_id": req.CourseID, "position": pos},
		})
	})

	if err != nil {
		return nil, err
	}
	return &sec, nil
}

func (r *Repository) UpdateSection(
	ctx context.Context,
	req UpdateSectionRequest,
	actorDescriptor string,
) (*Section, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.SectionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var sec Section
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		query := `
			UPDATE course_sections
			SET title_ar = COALESCE(NULLIF($1, ''), title_ar),
			    title_en = COALESCE(NULLIF($2, ''), title_en),
			    position = COALESCE($3, position),
			    updated_at = $4
			WHERE revision_id = $5::uuid AND section_identity_id = $6::uuid
			RETURNING id, revision_id, course_id, section_identity_id, title_ar, title_en, position, price_minor_units, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, req.TitleAr, req.TitleEn, req.Position, now, rev.ID, req.SectionID).Scan(
			&sec.ID, &sec.RevisionID, &sec.CourseID, &sec.SectionIdentityID, &sec.TitleAr, &sec.TitleEn, &sec.Position, &sec.PriceMinorUnits, &sec.CreatedAt, &sec.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("updating section: %w", err)
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "SECTION_UPDATED", targetType: "SECTION", targetID: sec.SectionIdentityID,
			reason: "Section updated", metadata: map[string]any{"course_id": req.CourseID},
		})
	})

	if err != nil {
		return nil, err
	}
	return &sec, nil
}

func (r *Repository) DeleteSection(
	ctx context.Context,
	req DeleteSectionRequest,
	actorDescriptor string,
) error {
	if req.CourseID == "" || req.RevisionID == "" || req.SectionID == "" || req.OwnerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		res, err := tx.Exec(ctx, `
			DELETE FROM course_sections
			WHERE revision_id = $1::uuid AND section_identity_id = $2::uuid
		`, rev.ID, req.SectionID)
		if err != nil {
			return fmt.Errorf("deleting section: %w", err)
		}
		if res.RowsAffected() == 0 {
			return ErrCourseNotFound
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "SECTION_DELETED", targetType: "SECTION", targetID: req.SectionID,
			reason: "Section deleted", metadata: map[string]any{"course_id": req.CourseID},
		})
	})
}

func (r *Repository) AddLesson(
	ctx context.Context,
	req AddLessonRequest,
	actorDescriptor string,
) (*Lesson, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.SectionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var les Lesson
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		var secID string
		var secIdentityID string
		err = tx.QueryRow(ctx, `
			SELECT id, section_identity_id FROM course_sections
			WHERE revision_id = $1::uuid AND section_identity_id = $2::uuid
		`, rev.ID, req.SectionID).Scan(&secID, &secIdentityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("querying section: %w", err)
		}

		var pos int
		if req.Position != nil {
			pos = *req.Position
		} else {
			err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM course_lessons WHERE section_id = $1::uuid`, secID).Scan(&pos)
			if err != nil {
				return fmt.Errorf("querying max lesson position: %w", err)
			}
		}

		// Create new stable lesson identity
		var lesIdentityID string
		err = tx.QueryRow(ctx, `
			INSERT INTO course_lesson_identities (course_id, section_identity_id)
			VALUES ($1::uuid, $2::uuid)
			RETURNING id
		`, req.CourseID, secIdentityID).Scan(&lesIdentityID)
		if err != nil {
			return fmt.Errorf("creating lesson identity: %w", err)
		}

		now := time.Now().UTC()
		query := `
			INSERT INTO course_lessons (section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, created_at, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $8)
			RETURNING id, section_id, course_id, section_identity_id, lesson_identity_id, title_ar, title_en, position, video_asset_version_id, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, secID, req.CourseID, secIdentityID, lesIdentityID, req.TitleAr, req.TitleEn, pos, now).Scan(
			&les.ID, &les.SectionID, &les.CourseID, &les.SectionIdentityID, &les.LessonIdentityID, &les.TitleAr, &les.TitleEn, &les.Position, &les.VideoAssetVersionID, &les.CreatedAt, &les.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting lesson: %w", err)
		}
		les.Files = []LessonFile{}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSON_CREATED", targetType: "LESSON", targetID: les.LessonIdentityID,
			reason: "Lesson created", metadata: map[string]any{
				"course_id": req.CourseID, "section_id": req.SectionID,
			},
		})
	})

	if err != nil {
		return nil, err
	}
	return &les, nil
}

func (r *Repository) UpdateLesson(
	ctx context.Context,
	req UpdateLessonRequest,
	actorDescriptor string,
) (*Lesson, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.LessonID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var les Lesson
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		query := `
			UPDATE course_lessons cl
			SET title_ar = COALESCE(NULLIF($1, ''), cl.title_ar),
			    title_en = COALESCE(NULLIF($2, ''), cl.title_en),
			    position = COALESCE($3, cl.position),
			    updated_at = $4
			FROM course_sections cs
			WHERE cl.section_id = cs.id AND cs.revision_id = $5::uuid
			  AND cl.lesson_identity_id = $6::uuid
			RETURNING cl.id, cl.section_id, cl.course_id, cl.section_identity_id, cl.lesson_identity_id, cl.title_ar, cl.title_en, cl.position, cl.video_asset_version_id, cl.created_at, cl.updated_at
		`
		err = tx.QueryRow(ctx, query, req.TitleAr, req.TitleEn, req.Position, now, rev.ID, req.LessonID).Scan(
			&les.ID, &les.SectionID, &les.CourseID, &les.SectionIdentityID, &les.LessonIdentityID, &les.TitleAr, &les.TitleEn, &les.Position, &les.VideoAssetVersionID, &les.CreatedAt, &les.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("updating lesson: %w", err)
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSON_UPDATED", targetType: "LESSON", targetID: les.LessonIdentityID,
			reason: "Lesson updated", metadata: map[string]any{"course_id": req.CourseID},
		})
	})

	if err != nil {
		return nil, err
	}
	return &les, nil
}

func (r *Repository) DeleteLesson(
	ctx context.Context,
	req DeleteLessonRequest,
	actorDescriptor string,
) error {
	if req.CourseID == "" || req.RevisionID == "" || req.LessonID == "" || req.OwnerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		res, err := tx.Exec(ctx, `
			DELETE FROM course_lessons cl
			USING course_sections cs
			WHERE cl.section_id = cs.id AND cs.revision_id = $1::uuid
			  AND cl.lesson_identity_id = $2::uuid
		`, rev.ID, req.LessonID)
		if err != nil {
			return fmt.Errorf("deleting lesson: %w", err)
		}
		if res.RowsAffected() == 0 {
			return ErrCourseNotFound
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSON_DELETED", targetType: "LESSON", targetID: req.LessonID,
			reason: "Lesson deleted", metadata: map[string]any{"course_id": req.CourseID},
		})
	})
}

func (r *Repository) SetLessonVideo(
	ctx context.Context,
	validator AssetVersionValidator,
	req SetVideoRequest,
	actorDescriptor string,
) (*Lesson, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.LessonID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
	}

	if err := validator.ValidateAssetVersion(ctx, req.VideoAssetVersionID); err != nil {
		return nil, err
	}

	var les Lesson
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		query := `
			UPDATE course_lessons cl
			SET video_asset_version_id = $1::uuid, updated_at = $2
			FROM course_sections cs
			WHERE cl.section_id = cs.id AND cs.revision_id = $3::uuid
			  AND cl.lesson_identity_id = $4::uuid
			RETURNING cl.id, cl.section_id, cl.course_id, cl.section_identity_id, cl.lesson_identity_id, cl.title_ar, cl.title_en, cl.position, cl.video_asset_version_id, cl.created_at, cl.updated_at
		`
		err = tx.QueryRow(ctx, query, req.VideoAssetVersionID, now, rev.ID, req.LessonID).Scan(
			&les.ID, &les.SectionID, &les.CourseID, &les.SectionIdentityID, &les.LessonIdentityID, &les.TitleAr, &les.TitleEn, &les.Position, &les.VideoAssetVersionID, &les.CreatedAt, &les.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("setting lesson video: %w", err)
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSON_VIDEO_ATTACHED", targetType: "LESSON", targetID: les.LessonIdentityID,
			reason: "Lesson video asset version attached", metadata: map[string]any{
				"course_id": req.CourseID, "video_asset_version_id": req.VideoAssetVersionID,
			},
		})
	})

	if err != nil {
		return nil, err
	}
	return &les, nil
}

func (r *Repository) AddLessonFile(
	ctx context.Context,
	validator AssetVersionValidator,
	req LessonFileRequest,
	actorDescriptor string,
) (*LessonFile, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.LessonID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if !req.Kind.Valid() {
		return nil, fmt.Errorf("invalid lesson file kind: %s", req.Kind)
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
	}

	if err := validator.ValidateAssetVersion(ctx, req.AssetVersionID); err != nil {
		return nil, err
	}

	var lf LessonFile
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		var versionLessonID string
		err = tx.QueryRow(ctx, `
			SELECT cl.id FROM course_lessons cl
			JOIN course_sections cs ON cs.id = cl.section_id
			WHERE cs.revision_id = $1::uuid AND cl.lesson_identity_id = $2::uuid
		`, rev.ID, req.LessonID).Scan(&versionLessonID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("querying lesson: %w", err)
		}

		var pos int
		if req.Position != nil {
			pos = *req.Position
		} else {
			err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM lesson_files WHERE lesson_id = $1::uuid AND kind = $2`, versionLessonID, req.Kind).Scan(&pos)
			if err != nil {
				return fmt.Errorf("querying max lesson file position: %w", err)
			}
		}

		now := time.Now().UTC()
		query := `
			INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position, created_at, updated_at)
			VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $7)
			RETURNING id, lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, versionLessonID, req.Kind, req.AssetVersionID, req.DisplayNameAr, req.DisplayNameEn, pos, now).Scan(
			&lf.ID, &lf.LessonID, &lf.Kind, &lf.AssetVersionID, &lf.DisplayNameAr, &lf.DisplayNameEn, &lf.Position, &lf.CreatedAt, &lf.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting lesson file: %w", err)
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSON_FILE_ATTACHED", targetType: "LESSON_FILE", targetID: lf.ID,
			reason: "Lesson file attached", metadata: map[string]any{
				"course_id": req.CourseID, "kind": string(req.Kind),
			},
		})
	})

	if err != nil {
		return nil, err
	}
	return &lf, nil
}

func (r *Repository) DeleteLessonFile(
	ctx context.Context,
	req DeleteLessonFileRequest,
	actorDescriptor string,
) error {
	if req.CourseID == "" || req.RevisionID == "" || req.LessonID == "" || req.FileID == "" || req.OwnerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		res, err := tx.Exec(ctx, `
			DELETE FROM lesson_files lf
			USING course_lessons cl, course_sections cs
			WHERE lf.lesson_id = cl.id AND cl.section_id = cs.id
			  AND cs.revision_id = $1::uuid
			  AND cl.lesson_identity_id = $2::uuid
			  AND lf.id = $3::uuid
		`, rev.ID, req.LessonID, req.FileID)
		if err != nil {
			return fmt.Errorf("deleting lesson file: %w", err)
		}
		if res.RowsAffected() == 0 {
			return ErrCourseNotFound
		}
		return writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "LESSON_FILE_DELETED", targetType: "LESSON_FILE", targetID: req.FileID,
			reason: "Lesson file deleted", metadata: map[string]any{"course_id": req.CourseID},
		})
	})
}

func (r *Repository) SetPreviewAsset(
	ctx context.Context,
	validator AssetVersionValidator,
	req PreviewAssetRequest,
	actorDescriptor string,
) (*CourseRevision, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
	}

	if err := validator.ValidateAssetVersion(ctx, req.PreviewAssetVersionID); err != nil {
		return nil, err
	}

	var rev *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		candidate, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			UPDATE course_revisions
			SET preview_asset_version_id = $1::uuid, updated_at = $2
			WHERE id = $3::uuid
		`, req.PreviewAssetVersionID, now, candidate.ID)
		if err != nil {
			return fmt.Errorf("setting preview asset: %w", err)
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "PREVIEW_ASSET_SET", targetType: "COURSE_REVISION", targetID: candidate.ID,
			reason: "Preview asset version nominated", metadata: map[string]any{
				"course_id": req.CourseID, "preview_asset_version_id": req.PreviewAssetVersionID,
			},
		}); err != nil {
			return err
		}

		res, err := r.loadRevisionGraphByIDTx(ctx, tx, candidate.ID)
		if err != nil {
			return err
		}
		rev = res
		return nil
	})

	if err != nil {
		return nil, err
	}
	return rev, nil
}

func (r *Repository) ClearPreviewAsset(
	ctx context.Context,
	req ClearPreviewAssetRequest,
	actorDescriptor string,
) (*CourseRevision, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	var rev *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		candidate, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			UPDATE course_revisions
			SET preview_asset_version_id = NULL, updated_at = $1
			WHERE id = $2::uuid
		`, now, candidate.ID)
		if err != nil {
			return fmt.Errorf("clearing preview asset: %w", err)
		}
		if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
			accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
			action: "PREVIEW_ASSET_CLEARED", targetType: "COURSE_REVISION", targetID: candidate.ID,
			reason: "Preview asset cleared", metadata: map[string]any{"course_id": req.CourseID},
		}); err != nil {
			return err
		}

		res, err := r.loadRevisionGraphByIDTx(ctx, tx, candidate.ID)
		if err != nil {
			return err
		}
		rev = res
		return nil
	})

	if err != nil {
		return nil, err
	}
	return rev, nil
}

func (r *Repository) SubmitCourse(
	ctx context.Context,
	validator AssetVersionValidator,
	req SubmitCourseRequest,
) (*Course, error) {
	courseID := req.CourseID
	revisionID := req.RevisionID
	ownerAccountID := req.OwnerAccountID
	actorDescriptor := req.ActorDescriptor
	if courseID == "" || revisionID == "" || ownerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if validator == nil {
		return nil, errors.New("asset version validator is required")
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
		if row.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		rev, err := r.LockCandidate(ctx, tx, courseID, revisionID)
		if err != nil {
			return err
		}

		revGraph, err := r.loadRevisionGraphByIDTx(ctx, tx, rev.ID)
		if err != nil || revGraph == nil {
			return fmt.Errorf("loading candidate revision graph: %w", err)
		}

		valErr, err := validateCourseForSubmission(ctx, submissionValidationRequest{
			tx: tx, validator: newTxAssetVersionValidator(tx),
			courseID: courseID, revision: revGraph,
		})
		if err != nil {
			return err
		}
		if valErr != nil {
			return valErr
		}

		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			UPDATE course_revisions
			SET state = 'PENDING_REVIEW', submitted_at = $1, updated_at = $1
			WHERE id = $2::uuid
		`, now, rev.ID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return &LifecycleConflictError{
					CourseID: courseID,
					Actual:   "PENDING_REVIEW",
					Expected: []string{"DRAFT", "CHANGES_REQUESTED"},
				}
			}
			return fmt.Errorf("updating revision for submit: %w", err)
		}

		if row.LiveRevisionID == nil || *row.LiveRevisionID == "" {
			_, err = tx.Exec(ctx, `
				UPDATE courses
				SET lifecycle = 'PENDING_REVIEW', updated_at = $1
				WHERE id = $2::uuid
			`, now, courseID)
			if err != nil {
				return fmt.Errorf("updating course lifecycle for submit: %w", err)
			}
			course.Lifecycle = LifecyclePendingReview
		} else {
			course.Lifecycle = CourseLifecycle(row.Lifecycle)
			course.LiveRevisionID = row.LiveRevisionID
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "COURSE_SUBMITTED",
			TargetType:      "COURSE",
			TargetID:        courseID,
			TargetRevision:  &rev.RevisionNumber,
			Reason:          "Course submitted for review",
			Metadata:        map[string]any{"revision_id": rev.ID},
		}
		if err := WriteAuditEvent(ctx, tx, audit); err != nil {
			return err
		}

		writer, err := NewNotificationIntentWriter(r.outboxWriter)
		if err != nil {
			return fmt.Errorf("constructing notification intent writer: %w", err)
		}
		event := outbox.Event{
			Type:              "catalog.course_submitted",
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
			"owner_account_id": ownerAccountID,
			"revision_id":      rev.ID,
			"submitted_at":     now,
		}
		if _, err := writer.WriteIntent(ctx, tx, event, protected); err != nil {
			return fmt.Errorf("writing submission intent: %w", err)
		}

		course.ID = courseID
		course.OwnerAccountID = ownerAccountID
		course.CreatedAt = row.CreatedAt
		course.UpdatedAt = now
		course.EditableRevision = revGraph
		course.EditableRevision.State = RevisionPendingReview
		return nil
	})

	if err != nil {
		return nil, err
	}
	return &course, nil
}

func (r *Repository) ListTaxonomyTerms(ctx context.Context, kind *TaxonomyKind) ([]TaxonomyTerm, error) {
	query := `
		SELECT id, kind, label_ar, label_en, academic_code, retired_at, created_at, updated_at
		FROM taxonomy_terms
		WHERE retired_at IS NULL
	`
	var args []any
	if kind != nil && kind.Valid() {
		query += ` AND kind = $1::taxonomy_kind`
		args = append(args, string(*kind))
	}
	query += ` ORDER BY kind ASC, label_en ASC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying taxonomy terms: %w", err)
	}
	defer rows.Close()

	var terms []TaxonomyTerm
	for rows.Next() {
		var t TaxonomyTerm
		if err := rows.Scan(
			&t.ID, &t.Kind, &t.LabelAr, &t.LabelEn, &t.AcademicCode,
			&t.RetiredAt, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning taxonomy term: %w", err)
		}
		terms = append(terms, t)
	}
	if terms == nil {
		terms = []TaxonomyTerm{}
	}
	return terms, nil
}
