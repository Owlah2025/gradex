package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Owlah2025/gradex/backend/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const MultipartPartBytes int64 = 8 * 1024 * 1024
const multipartLifetime = 7 * 24 * time.Hour

type MultipartUploadTicket struct {
	AssetVersionID   string                  `json:"asset_version_id"`
	UploadID         string                  `json:"upload_id"`
	StorageObjectKey string                  `json:"storage_object_key"`
	ExpiresAt        time.Time               `json:"expires_at"`
	PartSize         int64                   `json:"part_size_bytes"`
	Status           string                  `json:"status"`
	Parts            []storage.MultipartPart `json:"parts"`
}

type MultipartSessionRequest struct{ OwnerAccountID, AssetVersionID string }

type multipartSession struct {
	MultipartUploadTicket
	contentType, objectVersion, manifest, checksum string
	size                                           int64
	completed                                      bool
	expired                                        bool
}

func (s *Service) multipartProvider() (multipartStore, error) {
	store, ok := s.store.(multipartStore)
	if !ok {
		return nil, ErrUnavailable
	}
	return store, nil
}

func (s *Service) BeginMultipartUpload(ctx context.Context, request UploadRequest) (MultipartUploadTicket, error) {
	store, err := s.multipartProvider()
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	if err := s.authorizeUploadMode(request); err != nil {
		return MultipartUploadTicket{}, err
	}
	if request.Kind == KindThumbnail {
		return MultipartUploadTicket{}, ErrValidation
	}
	if err := validateBeginUpload(request, s.limits.perFile(request.Kind)); err != nil {
		return MultipartUploadTicket{}, err
	}
	if _, err := uuid.Parse(request.ClientRequestID); err != nil {
		return MultipartUploadTicket{}, ErrValidation
	}
	versionID, err := s.multipartRequestVersion(ctx, request)
	if err == nil {
		return s.prepareMultipartSession(ctx, MultipartSessionRequest{request.OwnerAccountID, versionID}, store)
	}
	if !errors.Is(err, ErrNotFound) {
		return MultipartUploadTicket{}, err
	}
	record := newUploadRecord(request, s.now, multipartLifetime)
	record.multipart = true
	if err := s.persistUpload(ctx, request, record); err != nil {
		if versionID, lookupErr := s.multipartRequestVersion(ctx, request); lookupErr == nil {
			return s.prepareMultipartSession(ctx, MultipartSessionRequest{request.OwnerAccountID, versionID}, store)
		}
		return MultipartUploadTicket{}, err
	}
	return s.prepareMultipartSession(ctx, MultipartSessionRequest{request.OwnerAccountID, record.assetVersionID}, store)
}

func loadMultipartSession(ctx context.Context, tx pgx.Tx, req MultipartSessionRequest) (multipartSession, error) {
	if _, err := uuid.Parse(req.AssetVersionID); err != nil {
		return multipartSession{}, ErrNotFound
	}
	if _, err := uuid.Parse(req.OwnerAccountID); err != nil {
		return multipartSession{}, ErrNotAuthorized
	}
	var session multipartSession
	var owner, courseOwner, role, status string
	err := tx.QueryRow(ctx, `
 SELECT ui.asset_version_id::text, COALESCE(ui.provider_upload_id,''), ui.expected_object_key,
 ui.expires_at, ui.multipart_status, ui.expected_content_type, ui.expected_size_bytes,
 COALESCE(ui.multipart_object_version,''), COALESCE(ui.multipart_manifest,''), COALESCE(ui.multipart_sha256,''),
 ui.completed_at IS NOT NULL, ui.expires_at <= now(), a.owner_account_id::text,
 c.owner_account_id::text, account.role, account.status
 FROM upload_intents ui JOIN media_asset_versions v ON v.id=ui.asset_version_id
 JOIN media_assets a ON a.id=v.logical_asset_id JOIN courses c ON c.id=a.course_id
 JOIN accounts account ON account.id=a.owner_account_id
 WHERE ui.asset_version_id=$1::uuid AND ui.is_multipart
 FOR UPDATE OF ui`, req.AssetVersionID).Scan(&session.AssetVersionID, &session.UploadID, &session.StorageObjectKey,
		&session.ExpiresAt, &session.Status, &session.contentType, &session.size, &session.objectVersion, &session.manifest,
		&session.checksum, &session.completed, &session.expired, &owner, &courseOwner, &role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return multipartSession{}, ErrNotFound
	}
	if err != nil {
		return multipartSession{}, err
	}
	if owner != req.OwnerAccountID || courseOwner != owner || role != "INSTRUCTOR" || status != "ACTIVE" {
		return multipartSession{}, ErrNotAuthorized
	}
	session.PartSize = MultipartPartBytes
	session.Parts = []storage.MultipartPart{}
	return session, nil
}

func activeMultipart(session multipartSession) error {
	if session.Status != "ACTIVE" || session.expired || session.UploadID == "" {
		return ErrConflict
	}
	return nil
}

func (s *Service) GetMultipartUpload(ctx context.Context, req MultipartSessionRequest) (MultipartUploadTicket, error) {
	store, err := s.multipartProvider()
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	defer tx.Rollback(ctx)
	session, err := loadMultipartSession(ctx, tx, req)
	if err != nil {
		return MultipartUploadTicket{}, err
	}
	if session.Status == "ACTIVE" {
		if err := activeMultipart(session); err != nil {
			return MultipartUploadTicket{}, err
		}
		session.Parts, err = store.ListMultipartParts(ctx, session.StorageObjectKey, session.UploadID)
		if err != nil {
			return MultipartUploadTicket{}, fmt.Errorf("%w: listing parts: %v", ErrUnavailable, err)
		}
	}
	return session.MultipartUploadTicket, tx.Commit(ctx)
}

func (s *Service) PresignUploadPart(ctx context.Context, req MultipartSessionRequest, number int32) (string, error) {
	store, err := s.multipartProvider()
	if err != nil {
		return "", err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	session, err := loadMultipartSession(ctx, tx, req)
	if err != nil {
		return "", err
	}
	if err := activeMultipart(session); err != nil {
		return "", err
	}
	if number < 1 || int64(number) > (session.size+MultipartPartBytes-1)/MultipartPartBytes {
		return "", ErrValidation
	}
	expiry := time.Until(session.ExpiresAt)
	if expiry > s.uploadURLExpiry {
		expiry = s.uploadURLExpiry
	}
	size := MultipartPartBytes
	if remaining := session.size - int64(number-1)*MultipartPartBytes; remaining < size {
		size = remaining
	}
	url, err := store.PresignUploadPartURL(ctx, storage.MultipartPartUpload{Key: session.StorageObjectKey, UploadID: session.UploadID, PartNumber: number, SizeBytes: size, Expiry: expiry})
	if err != nil {
		return "", fmt.Errorf("%w: signing part: %v", ErrUnavailable, err)
	}
	return url, tx.Commit(ctx)
}

type MultipartCompletionResult struct {
	CompletionResult
	StorageObjectVersion string
}

func multipartManifest(req CompleteMultipartRequest) (string, error) {
	if len(req.Parts) == 0 || len(req.Parts) > 10000 || !sha256Pattern.MatchString(req.SHA256Hex) {
		return "", ErrValidation
	}
	for i, p := range req.Parts {
		if p.PartNumber != int32(i+1) || !validPartETag(p.ETag) {
			return "", ErrValidation
		}
	}
	data, err := json.Marshal(req.Parts)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validPartETag(etag string) bool {
	if len(etag) < 3 || len(etag) > 128 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		return false
	}
	for _, b := range []byte(etag[1 : len(etag)-1]) {
		if b <= 32 || b == 127 || b == '"' {
			return false
		}
	}
	return true
}

func verifyMultipartParts(ctx context.Context, store multipartStore, session multipartSession, req CompleteMultipartRequest) error {
	parts, err := store.ListMultipartParts(ctx, session.StorageObjectKey, session.UploadID)
	if err != nil {
		return fmt.Errorf("%w: listing completion parts: %v", ErrUnavailable, err)
	}
	if len(parts) != len(req.Parts) || int64(len(parts)) != (session.size+MultipartPartBytes-1)/MultipartPartBytes {
		return ErrValidation
	}
	for i, part := range parts {
		size := MultipartPartBytes
		if i == len(parts)-1 {
			size = session.size - int64(i)*MultipartPartBytes
		}
		if part.PartNumber != req.Parts[i].PartNumber || part.ETag != req.Parts[i].ETag || part.SizeBytes != size {
			return ErrValidation
		}
	}
	return nil
}

func (s *Service) CompleteMultipartUpload(ctx context.Context, req CompleteMultipartRequest) (MultipartCompletionResult, error) {
	store, err := s.multipartProvider()
	if err != nil {
		return MultipartCompletionResult{}, err
	}
	manifest, err := multipartManifest(req)
	if err != nil {
		return MultipartCompletionResult{}, err
	}
	// The manifest is committed BEFORE provider completion; retries cannot replace the agreed parts.
	if err := s.reserveMultipartCompletion(ctx, req, manifest, store); err != nil {
		return MultipartCompletionResult{}, err
	}
	version, err := s.assembleMultipart(ctx, req, store)
	if err != nil {
		return MultipartCompletionResult{}, err
	}
	_ = version
	return s.GetMultipartCompletion(ctx, MultipartSessionRequest{req.OwnerAccountID, req.AssetVersionID})
}

func (s *Service) GetMultipartCompletion(ctx context.Context, req MultipartSessionRequest) (MultipartCompletionResult, error) {
	if _, err := uuid.Parse(req.AssetVersionID); err != nil {
		return MultipartCompletionResult{}, ErrNotFound
	}
	if _, err := uuid.Parse(req.OwnerAccountID); err != nil {
		return MultipartCompletionResult{}, ErrNotAuthorized
	}
	var result MultipartCompletionResult
	var failure, contentType, status string
	err := s.db.QueryRow(ctx, `SELECT v.state,ui.completed_at IS NOT NULL,ui.multipart_verification_error,COALESCE(ui.multipart_object_version,''),ui.expected_content_type,ui.multipart_status
	 FROM upload_intents ui JOIN media_asset_versions v ON v.id=ui.asset_version_id JOIN media_assets a ON a.id=v.logical_asset_id
	 JOIN courses c ON c.id=a.course_id JOIN accounts owner ON owner.id=a.owner_account_id
	 WHERE ui.asset_version_id=$1::uuid AND ui.is_multipart AND a.owner_account_id=$2::uuid AND c.owner_account_id=$2::uuid AND owner.role='INSTRUCTOR' AND owner.status='ACTIVE'`, req.AssetVersionID, req.OwnerAccountID).Scan(&result.State, &result.Duplicate, &failure, &result.StorageObjectVersion, &contentType, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartCompletionResult{}, ErrNotAuthorized
	}
	if err != nil {
		return MultipartCompletionResult{}, err
	}
	if status == "ABORTING" || status == "ABORTED" {
		return MultipartCompletionResult{}, ErrConflict
	}
	if failure == "CONTENT_TYPE_MISMATCH" {
		return MultipartCompletionResult{}, &ContentTypeMismatchError{DeclaredContentType: contentType}
	}
	if failure == "INVALID_OBJECT" {
		return MultipartCompletionResult{}, ErrValidation
	}
	if failure == "UNAVAILABLE" {
		return MultipartCompletionResult{}, ErrUnavailable
	}
	if failure != "" {
		return MultipartCompletionResult{}, ErrConflict
	}
	result.AssetVersionID = req.AssetVersionID
	return result, nil
}

func (s *Service) reserveMultipartCompletion(ctx context.Context, req CompleteMultipartRequest, manifest string, store multipartStore) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	session, err := loadMultipartSession(ctx, tx, MultipartSessionRequest{req.OwnerAccountID, req.AssetVersionID})
	if err != nil {
		return err
	}
	if req.UploadID != session.UploadID || req.StorageObjectKey != session.StorageObjectKey ||
		req.ContentType != session.contentType || req.SizeBytes != session.size {
		return ErrConflict
	}
	if session.Status == "ABORTING" || session.Status == "ABORTED" {
		return ErrConflict
	}
	if session.manifest != "" {
		if session.manifest != manifest || session.checksum != strings.ToLower(req.SHA256Hex) {
			return ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE upload_intents SET multipart_verification_error='', multipart_verification_attempts=0, multipart_verification_retry_at=now() WHERE asset_version_id=$1::uuid AND multipart_verification_error='UNAVAILABLE' AND multipart_verification_claim IS NULL`, req.AssetVersionID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err := activeMultipart(session); err != nil {
		return err
	}
	if err := verifyMultipartParts(ctx, store, session, req); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE upload_intents SET multipart_status='COMPLETING', multipart_manifest=$2, multipart_sha256=$3 WHERE asset_version_id=$1::uuid`,
		req.AssetVersionID, manifest, strings.ToLower(req.SHA256Hex))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) assembleMultipart(ctx context.Context, req CompleteMultipartRequest, store multipartStore) (string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	session, err := loadMultipartSession(ctx, tx, MultipartSessionRequest{req.OwnerAccountID, req.AssetVersionID})
	if err != nil {
		return "", err
	}
	if session.Status == "ABORTING" || session.Status == "ABORTED" {
		return "", ErrConflict
	}
	if session.objectVersion != "" {
		return session.objectVersion, tx.Commit(ctx)
	}
	// HEAD resolves a crash/timeout after storage committed but before PostgreSQL did.
	version, err := store.MultipartObjectIdentity(ctx, session.StorageObjectKey)
	if err != nil {
		return "", fmt.Errorf("%w: reconciling multipart: %v", ErrUnavailable, err)
	}
	if version == "" {
		numbers := make([]int32, len(req.Parts))
		etags := make([]string, len(req.Parts))
		for i, p := range req.Parts {
			numbers[i] = p.PartNumber
			etags[i] = p.ETag
		}
		version, err = store.CompleteMultipartUpload(ctx, session.StorageObjectKey, session.UploadID, numbers, etags)
		if err != nil {
			return "", fmt.Errorf("%w: assembling multipart: %v", ErrUnavailable, err)
		}
	}
	if version == "" {
		return "", ErrUnavailable
	}
	_, err = tx.Exec(ctx, `UPDATE upload_intents SET multipart_status='ASSEMBLED', multipart_object_version=$2 WHERE asset_version_id=$1::uuid`, req.AssetVersionID, version)
	if err != nil {
		return "", err
	}
	return version, tx.Commit(ctx)
}

func (s *Service) AbortMultipartUpload(ctx context.Context, req MultipartSessionRequest) error {
	store, err := s.multipartProvider()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	session, err := loadMultipartSession(ctx, tx, req)
	if err != nil {
		return err
	}
	if session.Status == "ABORTED" {
		return nil
	}
	if session.completed {
		return ErrConflict
	}
	// Persist ABORTING independently so completion cannot win during a cleanup retry.
	_, err = tx.Exec(ctx, `UPDATE upload_intents SET multipart_status='ABORTING' WHERE asset_version_id=$1::uuid`, req.AssetVersionID)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return cleanupMultipartSession(ctx, s.db, store, req.AssetVersionID)
}
