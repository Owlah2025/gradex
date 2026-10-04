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

	if _, err := s.db.Exec(ctx, `
		UPDATE upload_intents
		SET provider_upload_id = $1, is_multipart = true
		WHERE asset_version_id = $2::uuid
	`, uploadID, record.assetVersionID); err != nil {
		// Attempt to abort the multipart upload since we failed to persist the ID.
		_ = s.store.AbortMultipartUpload(ctx, record.objectKey, uploadID)
		return MultipartUploadTicket{}, fmt.Errorf("%w: binding multipart upload state: %v", ErrUnavailable, err)
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


type MultipartCompletionResult struct {
	CompletionResult
	StorageObjectVersion string
}

func (s *Service) CompleteMultipartUpload(ctx context.Context, request CompleteMultipartRequest) (MultipartCompletionResult, error) {
	if request.AssetVersionID == "" || request.UploadID == "" || len(request.Parts) == 0 {
		return MultipartCompletionResult{}, fmt.Errorf("%w: missing required completion fields", ErrValidation)
	}

	partNumbers := make([]int32, len(request.Parts))
	eTags := make([]string, len(request.Parts))
	for i, p := range request.Parts {
		partNumbers[i] = p.PartNumber
		eTags[i] = p.ETag
	}

	versionID, err := s.store.CompleteMultipartUpload(ctx, request.StorageObjectKey, request.UploadID, partNumbers, eTags)
	if err != nil {
		return MultipartCompletionResult{}, fmt.Errorf("provider completing multipart upload: %w", err)
	}

	res, err := s.CompleteUpload(ctx, CompleteUploadRequest{
		OwnerAccountID:       request.OwnerAccountID,
		AssetVersionID:       request.AssetVersionID,
		ProviderEventID:      request.ProviderEventID,
		StorageObjectKey:     request.StorageObjectKey,
		StorageObjectVersion: versionID,
		ContentType:          request.ContentType,
		SizeBytes:            request.SizeBytes,
		SHA256Hex:            request.SHA256Hex,
	})
	
	if err != nil {
	    return MultipartCompletionResult{}, err
	}

	return MultipartCompletionResult{
		CompletionResult:     res,
		StorageObjectVersion: versionID,
	}, nil
}
