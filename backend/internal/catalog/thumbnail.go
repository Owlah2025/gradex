package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type SetThumbnailRequest struct {
	CourseID, RevisionID, OwnerAccountID   string
	AssetVersionID, ExpectedAssetVersionID *string
}

// SetThumbnail is a compare-and-set on an explicit editable revision. Replays
// converge, while a stale tab cannot replace a more recent selection.
func (r *Repository) SetThumbnail(ctx context.Context, req SetThumbnailRequest, descriptor string) (*CourseRevision, error) {
	var result *CourseRevision
	err := r.ExecTx(ctx, func(tx pgx.Tx) error {
		course, err := r.LockCourse(ctx, tx, req.CourseID)
		if err != nil {
			return err
		}
		if course.OwnerAccountID != req.OwnerAccountID {
			return ErrCourseNotFound
		}
		if err = r.checkOwnerActive(ctx, tx, req.OwnerAccountID); err != nil {
			return err
		}
		candidate, err := r.LockCandidate(ctx, tx, req.CourseID, req.RevisionID)
		if err != nil {
			return err
		}
		if course.LiveRevisionID != nil && *course.LiveRevisionID == candidate.ID {
			return ErrCourseNotFound
		}
		var current *string
		if err = tx.QueryRow(ctx, `SELECT thumbnail_asset_version_id::text FROM course_revisions WHERE id=$1::uuid`, candidate.ID).Scan(&current); err != nil {
			return err
		}
		if sameNullableRevision(current, req.AssetVersionID) {
			result, err = r.loadRevisionGraphByIDTx(ctx, tx, candidate.ID)
			return err
		}
		if !sameNullableRevision(current, req.ExpectedAssetVersionID) {
			return &LifecycleConflictError{CourseID: req.CourseID, Actual: "THUMBNAIL_CHANGED", Expected: []string{"current thumbnail"}}
		}
		if req.AssetVersionID != nil {
			if err = validateThumbnail(ctx, tx, req.CourseID, candidate.ID, *req.AssetVersionID, true); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `UPDATE course_revisions SET thumbnail_asset_version_id=$1::uuid,updated_at=now() WHERE id=$2::uuid`, req.AssetVersionID, candidate.ID); err != nil {
			return err
		}
		if err = writeInstructorAudit(ctx, tx, instructorAuditRequest{accountID: req.OwnerAccountID, actorDescriptor: descriptor, action: "COURSE_THUMBNAIL_CHANGED", targetType: "COURSE_REVISION", targetID: candidate.ID, reason: "Candidate thumbnail changed", metadata: map[string]any{"course_id": req.CourseID, "previous_asset_version_id": current, "thumbnail_asset_version_id": req.AssetVersionID}}); err != nil {
			return err
		}
		result, err = r.loadRevisionGraphByIDTx(ctx, tx, candidate.ID)
		return err
	})
	return result, err
}

func validateThumbnail(ctx context.Context, tx pgx.Tx, courseID, revisionID, assetID string, currentOrigin bool) error {
	origin := `EXISTS(SELECT 1 FROM lineage WHERE id=ma.preview_origin_revision_id)`
	if currentOrigin {
		origin = `ma.preview_origin_revision_id=$3::uuid AND ma.owner_account_id=c.owner_account_id`
	}
	var found string
	err := tx.QueryRow(ctx, `WITH RECURSIVE lineage AS (
 SELECT id,based_on_revision_id FROM course_revisions WHERE id=$3::uuid AND course_id=$2::uuid
 UNION SELECT p.id,p.based_on_revision_id FROM course_revisions p JOIN lineage l ON l.based_on_revision_id=p.id
 ) SELECT v.id::text FROM media_asset_versions v
 JOIN media_assets ma ON ma.id=v.logical_asset_id JOIN courses c ON c.id=ma.course_id
 JOIN media_thumbnail_variants t ON t.asset_version_id=v.id AND t.source_object_version=v.storage_object_version AND t.source_sha256=v.sha256_hex
 WHERE v.id=$1::uuid AND ma.course_id=$2::uuid AND ma.kind='THUMBNAIL' AND v.kind='THUMBNAIL'
 AND v.state='READY' AND ma.retired_at IS NULL AND `+origin+` FOR SHARE OF v,ma`, assetID, courseID, revisionID).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetVersionInvalid
	}
	if err != nil {
		return fmt.Errorf("validating thumbnail: %w", err)
	}
	return nil
}
