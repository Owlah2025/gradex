package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBAssetVersionValidator struct {
	pool *pgxpool.Pool
}

func NewDBAssetVersionValidator(pool *pgxpool.Pool) *DBAssetVersionValidator {
	return &DBAssetVersionValidator{pool: pool}
}

func (v *DBAssetVersionValidator) ValidateAssetVersion(ctx context.Context, assetVersionID string) error {
	if assetVersionID == "" {
		return errors.New("asset version ID is required")
	}
	if v == nil || v.pool == nil {
		// In integration tests / fixtures where video pool is initialized, check DB.
		return nil
	}

	var status string
	err := v.pool.QueryRow(ctx, `SELECT status FROM videos WHERE id = $1::uuid`, assetVersionID).Scan(&status)
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
	OwnerAccountID string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type UpdateSectionRequest struct {
	CourseID       string
	OwnerAccountID string
	SectionID      string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type AddLessonRequest struct {
	CourseID       string
	OwnerAccountID string
	SectionID      string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type UpdateLessonRequest struct {
	CourseID       string
	OwnerAccountID string
	LessonID       string
	TitleAr        string
	TitleEn        string
	Position       *int
}

type LessonFileRequest struct {
	CourseID       string
	OwnerAccountID string
	LessonID       string
	Kind           LessonFileKind
	AssetVersionID string
	DisplayNameAr  string
	DisplayNameEn  string
	Position       *int
}

func (r *Repository) checkOwnerActive(ctx context.Context, tx pgx.Tx, ownerAccountID string) error {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM accounts WHERE id = $1::uuid`, ownerAccountID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("owner account not found")
	}
	if err != nil {
		return fmt.Errorf("checking owner account status: %w", err)
	}
	if status == "SUSPENDED" {
		return ErrAccountSuspended
	}
	return nil
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

func (r *Repository) ListOwnedCourses(ctx context.Context, ownerAccountID string) ([]Course, error) {
	if ownerAccountID == "" {
		return nil, errors.New("owner account ID is required")
	}

	query := `
		SELECT c.id, c.owner_account_id, c.lifecycle, c.live_revision_id,
		       c.access_suspended_at, c.access_suspension_reason, c.retired_at,
		       c.created_at, c.updated_at
		FROM courses c
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
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning course: %w", err)
		}

		// Load latest revision summary
		rev, err := r.getLatestRevision(ctx, c.ID)
		if err == nil {
			c.EditableRevision = rev
		}
		result = append(result, c)
	}
	if result == nil {
		result = []Course{}
	}
	return result, nil
}

func (r *Repository) getLatestRevision(ctx context.Context, courseID string) (*CourseRevision, error) {
	query := `
		SELECT id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en,
		       major_term_id, subject_term_id, study_year, preview_asset_version_id,
		       submitted_at, reviewed_at, reviewed_by_account_id, review_reason, created_at, updated_at
		FROM course_revisions
		WHERE course_id = $1::uuid
		ORDER BY revision_number DESC
		LIMIT 1
	`
	var rev CourseRevision
	err := r.pool.QueryRow(ctx, query, courseID).Scan(
		&rev.ID, &rev.CourseID, &rev.State, &rev.RevisionNumber, &rev.TitleAr, &rev.TitleEn, &rev.DescriptionAr, &rev.DescriptionEn,
		&rev.MajorTermID, &rev.SubjectTermID, &rev.StudyYear, &rev.PreviewAssetVersionID,
		&rev.SubmittedAt, &rev.ReviewedAt, &rev.ReviewedByAccountID, &rev.ReviewReason, &rev.CreatedAt, &rev.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rev, nil
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

	rev, err := r.loadFullRevisionGraph(ctx, courseID)
	if err != nil {
		return nil, fmt.Errorf("loading revision graph: %w", err)
	}
	c.EditableRevision = rev
	return &c, nil
}

func (r *Repository) loadFullRevisionGraph(ctx context.Context, courseID string) (*CourseRevision, error) {
	rev, err := r.getLatestRevision(ctx, courseID)
	if err != nil || rev == nil {
		return rev, err
	}

	// Load sections
	secQuery := `
		SELECT id, revision_id, title_ar, title_en, position, price_minor_units, created_at, updated_at
		FROM course_sections
		WHERE revision_id = $1::uuid
		ORDER BY position ASC
	`
	rows, err := r.pool.Query(ctx, secQuery, rev.ID)
	if err != nil {
		return nil, fmt.Errorf("querying sections: %w", err)
	}
	defer rows.Close()

	var sections []Section
	for rows.Next() {
		var s Section
		if err := rows.Scan(&s.ID, &s.RevisionID, &s.TitleAr, &s.TitleEn, &s.Position, &s.PriceMinorUnits, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}

		// Load lessons for section
		lesQuery := `
			SELECT id, section_id, title_ar, title_en, position, video_asset_version_id, created_at, updated_at
			FROM course_lessons
			WHERE section_id = $1::uuid
			ORDER BY position ASC
		`
		lRows, err := r.pool.Query(ctx, lesQuery, s.ID)
		if err != nil {
			return nil, fmt.Errorf("querying lessons: %w", err)
		}

		var lessons []Lesson
		for lRows.Next() {
			var l Lesson
			if err := lRows.Scan(&l.ID, &l.SectionID, &l.TitleAr, &l.TitleEn, &l.Position, &l.VideoAssetVersionID, &l.CreatedAt, &l.UpdatedAt); err != nil {
				lRows.Close()
				return nil, err
			}

			// Load files for lesson
			fQuery := `
				SELECT id, lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position, created_at, updated_at
				FROM lesson_files
				WHERE lesson_id = $1::uuid
				ORDER BY position ASC
			`
			fRows, err := r.pool.Query(ctx, fQuery, l.ID)
			if err != nil {
				lRows.Close()
				return nil, fmt.Errorf("querying lesson files: %w", err)
			}
			var files []LessonFile
			for fRows.Next() {
				var f LessonFile
				if err := fRows.Scan(&f.ID, &f.LessonID, &f.Kind, &f.AssetVersionID, &f.DisplayNameAr, &f.DisplayNameEn, &f.Position, &f.CreatedAt, &f.UpdatedAt); err != nil {
					fRows.Close()
					lRows.Close()
					return nil, err
				}
				files = append(files, f)
			}
			fRows.Close()
			if files == nil {
				files = []LessonFile{}
			}
			l.Files = files
			lessons = append(lessons, l)
		}
		lRows.Close()
		if lessons == nil {
			lessons = []Lesson{}
		}
		s.Lessons = lessons
		sections = append(sections, s)
	}
	if sections == nil {
		sections = []Section{}
	}
	rev.Sections = sections
	return rev, nil
}

func (r *Repository) UpdateCourseRevision(
	ctx context.Context,
	validator AssetVersionValidator,
	req UpdateRevisionRequest,
	actorDescriptor string,
) (*CourseRevision, error) {
	if req.CourseID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}

	if req.PreviewAssetVersionID != nil && *req.PreviewAssetVersionID != "" && validator != nil {
		if err := validator.ValidateAssetVersion(ctx, *req.PreviewAssetVersionID); err != nil {
			return nil, err
		}
	}

	var updatedRev CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: req.CourseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.getLatestRevision(ctx, req.CourseID)
		if err != nil || rev == nil {
			return fmt.Errorf("finding editable revision: %w", err)
		}

		query := `
			UPDATE course_revisions
			SET title_ar = COALESCE(NULLIF($1, ''), title_ar),
			    title_en = COALESCE(NULLIF($2, ''), title_en),
			    description_ar = COALESCE($3, description_ar),
			    description_en = COALESCE($4, description_en),
			    major_term_id = COALESCE($5::uuid, major_term_id),
			    subject_term_id = COALESCE($6::uuid, subject_term_id),
			    study_year = COALESCE($7::study_year, study_year),
			    preview_asset_version_id = COALESCE($8::uuid, preview_asset_version_id),
			    updated_at = now()
			WHERE id = $9::uuid
			RETURNING id, course_id, state, revision_number, title_ar, title_en, description_ar, description_en,
			          major_term_id, subject_term_id, study_year, preview_asset_version_id, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query,
			req.TitleAr, req.TitleEn, req.DescriptionAr, req.DescriptionEn,
			req.MajorTermID, req.SubjectTermID, req.StudyYear, req.PreviewAssetVersionID, rev.ID,
		).Scan(
			&updatedRev.ID, &updatedRev.CourseID, &updatedRev.State, &updatedRev.RevisionNumber,
			&updatedRev.TitleAr, &updatedRev.TitleEn, &updatedRev.DescriptionAr, &updatedRev.DescriptionEn,
			&updatedRev.MajorTermID, &updatedRev.SubjectTermID, &updatedRev.StudyYear, &updatedRev.PreviewAssetVersionID,
			&updatedRev.CreatedAt, &updatedRev.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("updating revision: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "COURSE_REVISION_UPDATED",
			TargetType:      "COURSE_REVISION",
			TargetID:        updatedRev.ID,
			Reason:          "Course metadata updated",
			Metadata:        map[string]any{"course_id": req.CourseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})

	if err != nil {
		return nil, err
	}
	return &updatedRev, nil
}

func (r *Repository) AddSection(ctx context.Context, req AddSectionRequest, actorDescriptor string) (*Section, error) {
	if req.CourseID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if len(req.TitleAr) == 0 || len(req.TitleEn) == 0 {
		return nil, errors.New("title_ar and title_en are required")
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
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: req.CourseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		rev, err := r.getLatestRevision(ctx, req.CourseID)
		if err != nil || rev == nil {
			return errors.New("no editable revision found")
		}

		pos := 1
		if req.Position != nil && *req.Position >= 0 {
			pos = *req.Position
		} else {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM course_sections WHERE revision_id = $1::uuid`, rev.ID).Scan(&pos)
		}

		query := `
			INSERT INTO course_sections (revision_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2, $3, $4)
			RETURNING id, revision_id, title_ar, title_en, position, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, rev.ID, req.TitleAr, req.TitleEn, pos).Scan(
			&sec.ID, &sec.RevisionID, &sec.TitleAr, &sec.TitleEn, &sec.Position, &sec.CreatedAt, &sec.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting section: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "SECTION_CREATED",
			TargetType:      "SECTION",
			TargetID:        sec.ID,
			Reason:          "Section created",
			Metadata:        map[string]any{"course_id": req.CourseID, "position": pos},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})

	if err != nil {
		return nil, err
	}
	sec.Lessons = []Lesson{}
	return &sec, nil
}

func (r *Repository) UpdateSection(ctx context.Context, req UpdateSectionRequest, actorDescriptor string) (*Section, error) {
	if req.CourseID == "" || req.SectionID == "" || req.OwnerAccountID == "" {
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
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: req.CourseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		query := `
			UPDATE course_sections
			SET title_ar = COALESCE(NULLIF($1, ''), title_ar),
			    title_en = COALESCE(NULLIF($2, ''), title_en),
			    position = COALESCE($3, position),
			    updated_at = now()
			WHERE id = $4::uuid
			RETURNING id, revision_id, title_ar, title_en, position, price_minor_units, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, req.TitleAr, req.TitleEn, req.Position, req.SectionID).Scan(
			&sec.ID, &sec.RevisionID, &sec.TitleAr, &sec.TitleEn, &sec.Position, &sec.PriceMinorUnits, &sec.CreatedAt, &sec.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("updating section: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "SECTION_UPDATED",
			TargetType:      "SECTION",
			TargetID:        sec.ID,
			Reason:          "Section updated",
			Metadata:        map[string]any{"course_id": req.CourseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})

	if err != nil {
		return nil, err
	}
	return &sec, nil
}

func (r *Repository) DeleteSection(ctx context.Context, courseID, sectionID, ownerAccountID, actorDescriptor string) error {
	if courseID == "" || sectionID == "" || ownerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `DELETE FROM course_sections WHERE id = $1::uuid`, sectionID)
		if err != nil {
			return fmt.Errorf("deleting section: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrCourseNotFound
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "SECTION_DELETED",
			TargetType:      "SECTION",
			TargetID:        sectionID,
			Reason:          "Section deleted",
			Metadata:        map[string]any{"course_id": courseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
}

func (r *Repository) AddLesson(ctx context.Context, req AddLessonRequest, actorDescriptor string) (*Lesson, error) {
	if req.CourseID == "" || req.SectionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if len(req.TitleAr) == 0 || len(req.TitleEn) == 0 {
		return nil, errors.New("title_ar and title_en are required")
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
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: req.CourseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		pos := 1
		if req.Position != nil && *req.Position >= 0 {
			pos = *req.Position
		} else {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM course_lessons WHERE section_id = $1::uuid`, req.SectionID).Scan(&pos)
		}

		query := `
			INSERT INTO course_lessons (section_id, title_ar, title_en, position)
			VALUES ($1::uuid, $2, $3, $4)
			RETURNING id, section_id, title_ar, title_en, position, video_asset_version_id, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, req.SectionID, req.TitleAr, req.TitleEn, pos).Scan(
			&les.ID, &les.SectionID, &les.TitleAr, &les.TitleEn, &les.Position, &les.VideoAssetVersionID, &les.CreatedAt, &les.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting lesson: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "LESSON_CREATED",
			TargetType:      "LESSON",
			TargetID:        les.ID,
			Reason:          "Lesson created",
			Metadata:        map[string]any{"course_id": req.CourseID, "section_id": req.SectionID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})

	if err != nil {
		return nil, err
	}
	les.Files = []LessonFile{}
	return &les, nil
}

func (r *Repository) UpdateLesson(ctx context.Context, req UpdateLessonRequest, actorDescriptor string) (*Lesson, error) {
	if req.CourseID == "" || req.LessonID == "" || req.OwnerAccountID == "" {
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
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: req.CourseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		query := `
			UPDATE course_lessons
			SET title_ar = COALESCE(NULLIF($1, ''), title_ar),
			    title_en = COALESCE(NULLIF($2, ''), title_en),
			    position = COALESCE($3, position),
			    updated_at = now()
			WHERE id = $4::uuid
			RETURNING id, section_id, title_ar, title_en, position, video_asset_version_id, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, req.TitleAr, req.TitleEn, req.Position, req.LessonID).Scan(
			&les.ID, &les.SectionID, &les.TitleAr, &les.TitleEn, &les.Position, &les.VideoAssetVersionID, &les.CreatedAt, &les.UpdatedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCourseNotFound
		}
		if err != nil {
			return fmt.Errorf("updating lesson: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "LESSON_UPDATED",
			TargetType:      "LESSON",
			TargetID:        les.ID,
			Reason:          "Lesson updated",
			Metadata:        map[string]any{"course_id": req.CourseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})

	if err != nil {
		return nil, err
	}
	return &les, nil
}

func (r *Repository) DeleteLesson(ctx context.Context, courseID, lessonID, ownerAccountID, actorDescriptor string) error {
	if courseID == "" || lessonID == "" || ownerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `DELETE FROM course_lessons WHERE id = $1::uuid`, lessonID)
		if err != nil {
			return fmt.Errorf("deleting lesson: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrCourseNotFound
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "LESSON_DELETED",
			TargetType:      "LESSON",
			TargetID:        lessonID,
			Reason:          "Lesson deleted",
			Metadata:        map[string]any{"course_id": courseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
}

func (r *Repository) SetLessonVideo(
	ctx context.Context,
	validator AssetVersionValidator,
	courseID, lessonID, videoAssetVersionID, ownerAccountID, actorDescriptor string,
) error {
	if courseID == "" || lessonID == "" || videoAssetVersionID == "" || ownerAccountID == "" {
		return ErrCourseNotFound
	}

	if validator != nil {
		if err := validator.ValidateAssetVersion(ctx, videoAssetVersionID); err != nil {
			return err
		}
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE course_lessons
			SET video_asset_version_id = $1::uuid, updated_at = now()
			WHERE id = $2::uuid
		`, videoAssetVersionID, lessonID)
		if err != nil {
			return fmt.Errorf("updating lesson video: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrCourseNotFound
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "LESSON_VIDEO_ATTACHED",
			TargetType:      "LESSON",
			TargetID:        lessonID,
			Reason:          "Lesson video asset version attached",
			Metadata:        map[string]any{"course_id": courseID, "video_asset_version_id": videoAssetVersionID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
}

func (r *Repository) AddOrUpdateLessonFile(
	ctx context.Context,
	validator AssetVersionValidator,
	req LessonFileRequest,
	actorDescriptor string,
) (*LessonFile, error) {
	if req.CourseID == "" || req.LessonID == "" || req.OwnerAccountID == "" || req.AssetVersionID == "" {
		return nil, ErrCourseNotFound
	}
	if !req.Kind.Valid() {
		return nil, fmt.Errorf("invalid lesson file kind: %s", req.Kind)
	}

	if validator != nil {
		if err := validator.ValidateAssetVersion(ctx, req.AssetVersionID); err != nil {
			return nil, err
		}
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
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: req.CourseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}

		pos := 1
		if req.Position != nil && *req.Position >= 0 {
			pos = *req.Position
		} else {
			_ = tx.QueryRow(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM lesson_files WHERE lesson_id = $1::uuid AND kind = $2::lesson_file_kind`, req.LessonID, string(req.Kind)).Scan(&pos)
		}

		query := `
			INSERT INTO lesson_files (lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position)
			VALUES ($1::uuid, $2::lesson_file_kind, $3::uuid, $4, $5, $6)
			RETURNING id, lesson_id, kind, asset_version_id, display_name_ar, display_name_en, position, created_at, updated_at
		`
		err = tx.QueryRow(ctx, query, req.LessonID, string(req.Kind), req.AssetVersionID, req.DisplayNameAr, req.DisplayNameEn, pos).Scan(
			&lf.ID, &lf.LessonID, &lf.Kind, &lf.AssetVersionID, &lf.DisplayNameAr, &lf.DisplayNameEn, &lf.Position, &lf.CreatedAt, &lf.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting lesson file: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &req.OwnerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "LESSON_FILE_ATTACHED",
			TargetType:      "LESSON_FILE",
			TargetID:        lf.ID,
			Reason:          "Lesson file attached",
			Metadata:        map[string]any{"course_id": req.CourseID, "kind": string(req.Kind)},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})

	if err != nil {
		return nil, err
	}
	return &lf, nil
}

func (r *Repository) DeleteLessonFile(ctx context.Context, courseID, lessonID, fileID, ownerAccountID, actorDescriptor string) error {
	if courseID == "" || lessonID == "" || fileID == "" || ownerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `DELETE FROM lesson_files WHERE id = $1::uuid AND lesson_id = $2::uuid`, fileID, lessonID)
		if err != nil {
			return fmt.Errorf("deleting lesson file: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrCourseNotFound
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "LESSON_FILE_DELETED",
			TargetType:      "LESSON_FILE",
			TargetID:        fileID,
			Reason:          "Lesson file deleted",
			Metadata:        map[string]any{"course_id": courseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
}

func (r *Repository) SetPreviewAsset(
	ctx context.Context,
	validator AssetVersionValidator,
	courseID, previewAssetVersionID, ownerAccountID, actorDescriptor string,
) error {
	if courseID == "" || previewAssetVersionID == "" || ownerAccountID == "" {
		return ErrCourseNotFound
	}

	if validator != nil {
		if err := validator.ValidateAssetVersion(ctx, previewAssetVersionID); err != nil {
			return err
		}
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		rev, err := r.getLatestRevision(ctx, courseID)
		if err != nil || rev == nil {
			return errors.New("no editable revision found")
		}

		_, err = tx.Exec(ctx, `UPDATE course_revisions SET preview_asset_version_id = $1::uuid, updated_at = now() WHERE id = $2::uuid`, previewAssetVersionID, rev.ID)
		if err != nil {
			return fmt.Errorf("setting preview asset: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "PREVIEW_ASSET_SET",
			TargetType:      "COURSE_REVISION",
			TargetID:        rev.ID,
			Reason:          "Preview asset version nominated",
			Metadata:        map[string]any{"course_id": courseID, "preview_asset_version_id": previewAssetVersionID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
}

func (r *Repository) ClearPreviewAsset(ctx context.Context, courseID, ownerAccountID, actorDescriptor string) error {
	if courseID == "" || ownerAccountID == "" {
		return ErrCourseNotFound
	}

	return r.ExecTx(ctx, func(tx pgx.Tx) error {
		courseRow, err := r.LockCourse(ctx, tx, courseID)
		if err != nil {
			return err
		}
		if courseRow.OwnerAccountID != ownerAccountID {
			return ErrCourseNotFound
		}
		if courseRow.Lifecycle == string(LifecyclePendingReview) {
			return &LifecycleConflictError{
				CourseID: courseID,
				Actual:   courseRow.Lifecycle,
				Expected: []string{string(LifecycleDraft), string(LifecycleChangesRequested)},
			}
		}
		if err := r.checkOwnerActive(ctx, tx, ownerAccountID); err != nil {
			return err
		}

		rev, err := r.getLatestRevision(ctx, courseID)
		if err != nil || rev == nil {
			return errors.New("no editable revision found")
		}

		_, err = tx.Exec(ctx, `UPDATE course_revisions SET preview_asset_version_id = NULL, updated_at = now() WHERE id = $1::uuid`, rev.ID)
		if err != nil {
			return fmt.Errorf("clearing preview asset: %w", err)
		}

		audit := AuditEvent{
			ActorAccountID:  &ownerAccountID,
			ActorRole:       "INSTRUCTOR",
			ActorDescriptor: actorDescriptor,
			Action:          "PREVIEW_ASSET_CLEARED",
			TargetType:      "COURSE_REVISION",
			TargetID:        rev.ID,
			Reason:          "Preview asset cleared",
			Metadata:        map[string]any{"course_id": courseID},
		}
		return WriteAuditEvent(ctx, tx, audit)
	})
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
	query += ` ORDER BY label_en ASC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying taxonomy terms: %w", err)
	}
	defer rows.Close()

	var result []TaxonomyTerm
	for rows.Next() {
		var t TaxonomyTerm
		if err := rows.Scan(&t.ID, &t.Kind, &t.LabelAr, &t.LabelEn, &t.AcademicCode, &t.RetiredAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	if result == nil {
		result = []TaxonomyTerm{}
	}
	return result, nil
}
