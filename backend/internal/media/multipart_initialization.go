package media

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) multipartRequestVersion(ctx context.Context, req UploadRequest) (string, error) {
	var version, owner, course, kind, contentType, lesson, revision string
	var size int64
	err := s.db.QueryRow(ctx, `SELECT ui.asset_version_id::text,a.owner_account_id::text,a.course_id::text,a.kind,ui.expected_content_type,
 ui.expected_size_bytes,COALESCE(a.lesson_id::text,''),COALESCE(a.preview_origin_revision_id::text,'')
 FROM upload_intents ui JOIN media_asset_versions v ON v.id=ui.asset_version_id JOIN media_assets a ON a.id=v.logical_asset_id
 WHERE ui.multipart_client_request_id=$1::uuid`, req.ClientRequestID).Scan(&version, &owner, &course, &kind, &contentType, &size, &lesson, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if owner != req.OwnerAccountID {
		return "", ErrNotAuthorized
	}
	if course != req.CourseID || kind != string(req.Kind) || contentType != req.ContentType || size != req.SizeBytes || lesson != req.LessonID || revision != req.RevisionID {
		return "", ErrConflict
	}
	return version, nil
}

func (s *Service) prepareMultipartSession(ctx context.Context, req MultipartSessionRequest, store multipartStore) (MultipartUploadTicket, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	defer tx.Rollback(ctx)
	session, err := loadMultipartSession(ctx, tx, req)
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	if session.Status == "ABORTING" || session.Status == "ABORTED" || (session.expired && !session.completed) {
		return MultipartUploadTicket{}, ErrConflict
	}
	if session.UploadID == "" {
		id, err := store.FindMultipartUpload(ctx, session.StorageObjectKey)
		if err != nil {
			return MultipartUploadTicket{}, fmt.Errorf("%w: reconciling upload creation: %v", ErrUnavailable, err)
		}
		if id == "" {
			id, err = store.CreateMultipartUpload(ctx, session.StorageObjectKey, session.contentType)
		}
		if err != nil {
			return MultipartUploadTicket{}, fmt.Errorf("%w: creating multipart upload: %v", ErrUnavailable, err)
		}
		if id == "" {
			return MultipartUploadTicket{}, ErrUnavailable
		}
		if _, err := tx.Exec(ctx, `UPDATE upload_intents SET provider_upload_id=$2 WHERE asset_version_id=$1::uuid`, req.AssetVersionID, id); err != nil {
			return MultipartUploadTicket{}, err
		}
		session.UploadID = id
	}
	if session.Status == "ACTIVE" {
		session.Parts, err = store.ListMultipartParts(ctx, session.StorageObjectKey, session.UploadID)
		if err != nil {
			return MultipartUploadTicket{}, fmt.Errorf("%w: listing recovered upload: %v", ErrUnavailable, err)
		}
	}
	return session.MultipartUploadTicket, tx.Commit(ctx)
}

func (s *Service) MultipartRequestSession(ctx context.Context, ownerID, clientID string) (MultipartUploadTicket, error) {
	if _, err := uuid.Parse(clientID); err != nil {
		return MultipartUploadTicket{}, ErrNotFound
	}
	if _, err := uuid.Parse(ownerID); err != nil {
		return MultipartUploadTicket{}, ErrNotAuthorized
	}
	var version string
	err := s.db.QueryRow(ctx, `SELECT ui.asset_version_id::text FROM upload_intents ui JOIN media_asset_versions v ON v.id=ui.asset_version_id JOIN media_assets a ON a.id=v.logical_asset_id WHERE ui.multipart_client_request_id=$1::uuid AND a.owner_account_id=$2::uuid`, clientID, ownerID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartUploadTicket{}, ErrNotFound
	}
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	return MultipartUploadTicket{AssetVersionID: version}, nil
}
