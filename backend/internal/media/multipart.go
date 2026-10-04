package media

import (
	"context"
	"fmt"
	"time"
)

type MultipartUploadTicket struct {
	AssetVersionID   string    `json:"asset_version_id"`
	UploadID         string    `json:"upload_id"`
	StorageObjectKey string    `json:"storage_object_key"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func (s *Service) BeginMultipartUpload(ctx context.Context, request UploadRequest) (MultipartUploadTicket, error) {
	if err := validateBeginUpload(request, s.limits.perFile(request.Kind)); err != nil {
		return MultipartUploadTicket{}, err
	}
	record := newUploadRecord(request, s.now, s.uploadURLExpiry)
	if err := s.persistUpload(ctx, request, record); err != nil {
		return MultipartUploadTicket{}, err
	}

	uploadID, err := s.store.CreateMultipartUpload(ctx, record.objectKey, request.ContentType)
	if err != nil {
		return MultipartUploadTicket{}, fmt.Errorf("%w: creating multipart upload: %v", ErrUnavailable, err)
	}

	return MultipartUploadTicket{
		AssetVersionID:   record.assetVersionID,
		UploadID:         uploadID,
		StorageObjectKey: record.objectKey,
		ExpiresAt:        record.expiresAt,
	}, nil
}

func (s *Service) PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int32) (string, error) {
	return s.store.PresignUploadPartURL(ctx, key, uploadID, partNumber, s.uploadURLExpiry)
}

func (s *Service) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	return s.store.AbortMultipartUpload(ctx, key, uploadID)
}
