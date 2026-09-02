package catalog

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ClaimPublicPreviewUploadRequest struct {
	CourseID              string
	RevisionID            string
	PreviewAssetVersionID string
	OwnerAccountID        string
}

type PublicPreviewUploadClaim struct {
	Selected bool            `json:"selected"`
	Revision *CourseRevision `json:"revision"`
}

type publicPreviewUploadIntent struct {
	assetVersionID string
	createdAt      time.Time
	state          string
}

// ClaimPublicPreviewUpload selects a completed public-preview upload while it
// may still be processing, exactly as ClaimLessonVideoUpload does for Lesson
// video. It is the durable half of the completion operation: once it commits,
// the editable revision points at these bytes and the browser is free to close.
//
// D-096 made a trusted public preview take the full FFmpeg path, so the window
// between "the upload finished" and "the asset is READY" is now long enough
// that a browser cannot be asked to hold it open. Selecting early is what keeps
// a successfully processed preview from being orphaned.
//
// Selecting early is not publishing early. Submission, approval, and public
// delivery all still go through validatePreviewAsset and the READY-only
// delivery queries; this command records the editable draft's intent and
// nothing more.
func (r *Repository) ClaimPublicPreviewUpload(
	ctx context.Context,
	req ClaimPublicPreviewUploadRequest,
	actorDescriptor string,
) (*PublicPreviewUploadClaim, error) {
	if req.CourseID == "" || req.RevisionID == "" || req.OwnerAccountID == "" {
		return nil, ErrCourseNotFound
	}
	if _, err := uuid.Parse(req.PreviewAssetVersionID); err != nil {
		return nil, ErrAssetVersionInvalid
	}

	var claim *PublicPreviewUploadClaim
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		var err error
		claim, err = r.claimPublicPreviewUpload(ctx, tx, req, actorDescriptor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

func (r *Repository) claimPublicPreviewUpload(
	ctx context.Context,
	tx pgx.Tx,
	req ClaimPublicPreviewUploadRequest,
	actorDescriptor string,
) (*PublicPreviewUploadClaim, error) {
	course, err := r.LockCourse(ctx, tx, req.CourseID)
	if err != nil {
		return nil, err
	}
	if course.OwnerAccountID != req.OwnerAccountID {
		return nil, ErrCourseNotFound
	}
	if err := r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
		return nil, err
	}
	// LockCandidate refuses a revision that is not an editable candidate, so a
	// preview cannot be swapped under a submitted or approved revision.
	candidate, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
	if err != nil {
		return nil, err
	}
	incoming, err := validateCompletedPublicPreview(ctx, tx, req, candidate.ID)
	if err != nil {
		return nil, err
	}
	if candidate.PreviewAssetVersionID != nil && *candidate.PreviewAssetVersionID == incoming.assetVersionID {
		// Already selected. A retry after a lost response converges here rather
		// than writing a second audit event.
		return r.publicPreviewClaim(ctx, tx, candidate.ID, true)
	}
	newerSelected, err := selectedPreviewUploadIsNewer(ctx, tx, candidate.PreviewAssetVersionID, incoming)
	if err != nil {
		return nil, err
	}
	if newerSelected {
		return r.publicPreviewClaim(ctx, tx, candidate.ID, false)
	}
	return r.selectPublicPreviewUpload(ctx, tx, req, candidate.ID, incoming, actorDescriptor)
}

func (r *Repository) selectPublicPreviewUpload(
	ctx context.Context,
	tx pgx.Tx,
	req ClaimPublicPreviewUploadRequest,
	candidateID string,
	incoming publicPreviewUploadIntent,
	actorDescriptor string,
) (*PublicPreviewUploadClaim, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE course_revisions
		SET preview_asset_version_id = $1::uuid, updated_at = $2
		WHERE id = $3::uuid
	`, incoming.assetVersionID, time.Now().UTC(), candidateID); err != nil {
		return nil, fmt.Errorf("selecting completed public preview upload: %w", err)
	}
	if err := writeInstructorAudit(ctx, tx, instructorAuditRequest{
		accountID: req.OwnerAccountID, actorDescriptor: actorDescriptor,
		action: "PREVIEW_UPLOAD_SELECTED", targetType: "COURSE_REVISION", targetID: candidateID,
		reason: "Completed public preview upload selected for processing", metadata: map[string]any{
			"course_id": req.CourseID, "preview_asset_version_id": incoming.assetVersionID,
			"media_state": incoming.state,
		},
	}); err != nil {
		return nil, err
	}
	return r.publicPreviewClaim(ctx, tx, candidateID, true)
}

func (r *Repository) publicPreviewClaim(
	ctx context.Context,
	tx pgx.Tx,
	candidateID string,
	selected bool,
) (*PublicPreviewUploadClaim, error) {
	revision, err := r.loadRevisionGraphByIDTx(ctx, tx, candidateID)
	if err != nil {
		return nil, err
	}
	return &PublicPreviewUploadClaim{Selected: selected, Revision: revision}, nil
}

// validateCompletedPublicPreview proves the supplied Asset Version really is a
// completed public-preview upload this Instructor made for this exact revision.
//
// It deliberately does not require READY: the point of the command is to record
// the selection while FFmpeg is still running. What it does require is that the
// upload left the UPLOADED state and that its intent is closed, so an intent
// that was issued but never completed can never be selected.
func validateCompletedPublicPreview(
	ctx context.Context,
	tx pgx.Tx,
	req ClaimPublicPreviewUploadRequest,
	candidateID string,
) (publicPreviewUploadIntent, error) {
	var createdAt time.Time
	var state string
	err := tx.QueryRow(ctx, `
		SELECT ui.created_at, mav.state::text
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN upload_intents ui ON ui.asset_version_id = mav.id
		WHERE mav.id = $1::uuid
		  AND mav.kind = 'PREVIEW' AND mav.state <> 'UPLOADED'
		  AND mav.content_type = 'video/mp4'
		  AND ma.kind = 'PREVIEW' AND ma.visibility = 'PUBLIC_PREVIEW'
		  AND ma.owner_account_id = $2::uuid AND ma.course_id = $3::uuid
		  AND ma.preview_origin_revision_id = $4::uuid
		  AND ma.retired_at IS NULL AND ui.completed_at IS NOT NULL
		FOR SHARE OF mav, ma, ui
	`, req.PreviewAssetVersionID, req.OwnerAccountID, req.CourseID, candidateID).Scan(&createdAt, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return publicPreviewUploadIntent{}, ErrAssetVersionInvalid
	}
	if err != nil {
		return publicPreviewUploadIntent{}, fmt.Errorf("validating completed public preview upload: %w", err)
	}
	return publicPreviewUploadIntent{
		assetVersionID: req.PreviewAssetVersionID,
		createdAt:      createdAt,
		state:          state,
	}, nil
}

// selectedPreviewUploadIsNewer reports whether the already-selected preview
// came from a later completed upload intent than the one arriving now. Intent
// order is reserved under an advisory lock when the intent is created, so it is
// strictly increasing per revision and a late completion cannot displace a
// newer one it lost the race to.
func selectedPreviewUploadIsNewer(
	ctx context.Context,
	tx pgx.Tx,
	selectedID *string,
	incoming publicPreviewUploadIntent,
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
		return false, fmt.Errorf("comparing public preview upload intents: %w", err)
	}
	return newer, nil
}
