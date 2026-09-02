package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ClaimLessonVideoUploadRequest struct {
	CourseID            string
	RevisionID          string
	LessonID            string
	VideoAssetVersionID string
	OwnerAccountID      string
}

type LessonVideoUploadClaim struct {
	Selected bool   `json:"selected"`
	Lesson   Lesson `json:"lesson"`
}

type lessonVideoUploadIntent struct {
	assetVersionID string
	createdAt      time.Time
	state          string
}

type lessonVideoSelection struct {
	request         ClaimLessonVideoUploadRequest
	lesson          Lesson
	incoming        lessonVideoUploadIntent
	actorDescriptor string
}

// ClaimLessonVideoUpload selects a completed Lesson-video upload while it may
// still be processing. Submission, approval, and delivery continue to use the
// READY-only media validator; this command only records the editable draft's
// durable replacement intent.
func (r *Repository) ClaimLessonVideoUpload(
	ctx context.Context,
	req ClaimLessonVideoUploadRequest,
	actorDescriptor string,
) (*LessonVideoUploadClaim, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.LessonID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(req.VideoAssetVersionID); err != nil {
		return nil, ErrAssetVersionInvalid
	}

	var claim *LessonVideoUploadClaim
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		var err error
		claim, err = r.claimLessonVideoUpload(ctx, tx, req, actorDescriptor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

func (r *Repository) claimLessonVideoUpload(
	ctx context.Context,
	tx pgx.Tx,
	req ClaimLessonVideoUploadRequest,
	actorDescriptor string,
) (*LessonVideoUploadClaim, error) {
	if err := r.authorizeLessonVideoUploadClaim(ctx, tx, req); err != nil {
		return nil, err
	}
	lesson, err := lockClaimedLesson(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	incoming, err := validateCompletedLessonVideo(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	return r.selectLessonVideoUpload(ctx, tx, lessonVideoSelection{
		request: req, lesson: lesson, incoming: incoming, actorDescriptor: actorDescriptor,
	})
}

func (r *Repository) authorizeLessonVideoUploadClaim(
	ctx context.Context,
	tx pgx.Tx,
	req ClaimLessonVideoUploadRequest,
) error {
	course, err := r.LockCourse(ctx, tx, req.CourseID)
	if err != nil {
		return err
	}
	if course.OwnerAccountID != req.OwnerAccountID {
		return ErrCourseNotFound
	}
	if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
		return err
	}
	_, err = r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
	return err
}

func (r *Repository) selectLessonVideoUpload(
	ctx context.Context,
	tx pgx.Tx,
	selection lessonVideoSelection,
) (*LessonVideoUploadClaim, error) {
	if selection.lesson.VideoAssetVersionID != nil && *selection.lesson.VideoAssetVersionID == selection.incoming.assetVersionID {
		selection.lesson.VideoAssetState = &selection.incoming.state
		return &LessonVideoUploadClaim{Selected: true, Lesson: selection.lesson}, nil
	}
	newerSelected, err := selectedUploadIsNewer(ctx, tx, selection.lesson.VideoAssetVersionID, selection.incoming)
	if err != nil {
		return nil, err
	}
	if newerSelected {
		state, err := selectedVideoState(ctx, tx, *selection.lesson.VideoAssetVersionID)
		if err != nil {
			return nil, err
		}
		selection.lesson.VideoAssetState = &state
		return &LessonVideoUploadClaim{Lesson: selection.lesson}, nil
	}
	return r.updateLessonVideoSelection(ctx, tx, selection)
}

func (r *Repository) updateLessonVideoSelection(
	ctx context.Context,
	tx pgx.Tx,
	selection lessonVideoSelection,
) (*LessonVideoUploadClaim, error) {
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE course_lessons
		SET video_asset_version_id = $1::uuid, updated_at = $2
		WHERE id = $3::uuid
	`, selection.incoming.assetVersionID, now, selection.lesson.ID); err != nil {
		return nil, fmt.Errorf("selecting completed Lesson video upload: %w", err)
	}
	selection.lesson.VideoAssetVersionID = &selection.incoming.assetVersionID
	selection.lesson.VideoAssetState = &selection.incoming.state
	selection.lesson.UpdatedAt = now
	if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
		accountID: selection.request.OwnerAccountID, actorDescriptor: selection.actorDescriptor,
		action: "LESSON_VIDEO_UPLOAD_SELECTED", targetType: "LESSON", targetID: selection.lesson.LessonIdentityID,
		reason: "Completed Lesson video upload selected for processing", metadata: map[string]any{
			"course_id": selection.request.CourseID, "video_asset_version_id": selection.incoming.assetVersionID,
		},
	}); err != nil {
		return nil, err
	}
	return &LessonVideoUploadClaim{Selected: true, Lesson: selection.lesson}, nil
}

func lockClaimedLesson(ctx context.Context, tx pgx.Tx, req ClaimLessonVideoUploadRequest) (Lesson, error) {
	var lesson Lesson
	err := tx.QueryRow(ctx, `
		SELECT cl.id, cl.section_id, cl.course_id, cl.section_identity_id, cl.lesson_identity_id,
		       cl.title_ar, cl.title_en, cl.position, cl.video_asset_version_id, cl.created_at, cl.updated_at
		FROM course_lessons cl
		JOIN course_sections cs ON cs.id = cl.section_id
		WHERE cl.course_id = $1::uuid AND cs.revision_id = $2::uuid
		  AND cl.lesson_identity_id = $3::uuid
		FOR UPDATE OF cl
	`, req.CourseID, req.RevisionID, req.LessonID).Scan(
		&lesson.ID, &lesson.SectionID, &lesson.CourseID, &lesson.SectionIdentityID,
		&lesson.LessonIdentityID, &lesson.TitleAr, &lesson.TitleEn, &lesson.Position,
		&lesson.VideoAssetVersionID, &lesson.CreatedAt, &lesson.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Lesson{}, ErrCourseNotFound
	}
	if err != nil {
		return Lesson{}, fmt.Errorf("locking Lesson video upload target: %w", err)
	}
	lesson.Files = []LessonFile{}
	return lesson, nil
}

func validateCompletedLessonVideo(
	ctx context.Context,
	tx pgx.Tx,
	req ClaimLessonVideoUploadRequest,
) (lessonVideoUploadIntent, error) {
	var createdAt time.Time
	var state string
	err := tx.QueryRow(ctx, `
		SELECT ui.created_at, mav.state::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN upload_intents ui ON ui.asset_version_id = mav.id
		WHERE mav.id = $1::uuid
		  AND mav.kind = 'VIDEO' AND mav.state <> 'UPLOADED'
		  AND ma.kind = 'VIDEO' AND ma.owner_account_id = $2::uuid
		  AND ma.course_id = $3::uuid AND ma.lesson_id = $4::uuid
		  AND ma.retired_at IS NULL AND ui.completed_at IS NOT NULL
		FOR SHARE OF mav, ma, ui
	`, req.VideoAssetVersionID, req.OwnerAccountID, req.CourseID, req.LessonID).Scan(&createdAt, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return lessonVideoUploadIntent{}, ErrAssetVersionInvalid
	}
	if err != nil {
		return lessonVideoUploadIntent{}, fmt.Errorf("validating completed Lesson video upload: %w", err)
	}
	return lessonVideoUploadIntent{
		assetVersionID: req.VideoAssetVersionID,
		createdAt:      createdAt,
		state:          state,
	}, nil
}

func selectedUploadIsNewer(
	ctx context.Context,
	tx pgx.Tx,
	selectedID *string,
	incoming lessonVideoUploadIntent,
) (bool, error) {
	if selectedID == nil || *selectedID == "" {
		return false, nil
	}
	var newer bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM upload_intents ui
			WHERE ui.asset_version_id = $1::uuid AND ui.completed_at IS NOT NULL
			  AND (ui.created_at, ui.asset_version_id) > ($2, $3::uuid)
		)
	`, *selectedID, incoming.createdAt, incoming.assetVersionID).Scan(&newer); err != nil {
		return false, fmt.Errorf("comparing Lesson video upload intents: %w", err)
	}
	return newer, nil
}

func selectedVideoState(ctx context.Context, tx pgx.Tx, assetVersionID string) (string, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state::text FROM media_asset_versions WHERE id = $1::uuid`, assetVersionID).Scan(&state)
	if err != nil {
		return "", fmt.Errorf("loading selected Lesson video state: %w", err)
	}
	return state, nil
}
