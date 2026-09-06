package media

import (
	"context"
	"errors"
	"fmt"
	"github.com/Owlah2025/gradex/backend/internal/catalogpublic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ThumbnailReadRequest struct {
	CourseID, RevisionID, AssetVersionID, Variant string
	Viewer                                        Viewer
	Public                                        bool
}

// ReadThumbnail releases only derivatives. The private bucket and original key
// never become a public capability, even if someone guesses a candidate UUID.
func (s *Service) ReadThumbnail(ctx context.Context, req ThumbnailReadRequest) ([]byte, error) {
	for _, id := range []string{req.CourseID, req.AssetVersionID} {
		if _, err := uuid.Parse(id); err != nil {
			return nil, ErrNotFound
		}
	}
	if req.Variant != "card" && req.Variant != "large" {
		return nil, ErrNotFound
	}
	predicate := catalogpublic.PublishedOnly("c", "r")
	args := []any{req.CourseID, req.AssetVersionID}
	if !req.Public {
		if _, err := uuid.Parse(req.RevisionID); err != nil {
			return nil, ErrNotFound
		}
		if _, err := uuid.Parse(req.Viewer.AccountID); err != nil {
			return nil, ErrNotAuthorized
		}
		// Resolve current account authority from the DB, never from client fields.
		predicate = `r.id=$3::uuid AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=$4::uuid AND a.status='ACTIVE'
    AND (a.role='ADMIN' OR (a.role='INSTRUCTOR' AND c.owner_account_id=a.id)))`
		args = append(args, req.RevisionID, req.Viewer.AccountID)
	}
	var card, large string
	err := s.db.QueryRow(ctx, `SELECT t.card_key,t.large_key FROM courses c
 JOIN course_revisions r ON r.course_id=c.id
 JOIN media_asset_versions v ON v.id=r.thumbnail_asset_version_id
 JOIN media_assets ma ON ma.id=v.logical_asset_id AND ma.course_id=c.id
 JOIN media_thumbnail_variants t ON t.asset_version_id=v.id AND t.source_object_version=v.storage_object_version AND t.source_sha256=v.sha256_hex
 WHERE c.id=$1::uuid AND v.id=$2::uuid AND v.kind='THUMBNAIL' AND ma.kind='THUMBNAIL'
 AND v.state='READY' AND ma.retired_at IS NULL AND `+predicate, args...).Scan(&card, &large)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading thumbnail authority: %w", err)
	}
	store, ok := s.store.(thumbnailStore)
	if !ok {
		return nil, ErrUnavailable
	}
	key := card
	if req.Variant == "large" {
		key = large
	}
	body, err := store.DownloadObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("%w: thumbnail delivery", ErrUnavailable)
	}
	return body, nil
}
