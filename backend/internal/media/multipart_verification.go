package media

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// Only the durable worker claim may admit a multipart object to quarantine.
type multipartVerificationKey struct{}
type multipartVerificationProofKey struct{}

func (s *Service) VerifyPendingMultipartUpload(ctx context.Context) (bool, error) {
	request, claim, err := s.claimMultipartVerification(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	verifyCtx, cancel := context.WithTimeout(context.WithValue(ctx, multipartVerificationKey{}, claim), 15*time.Minute)
	defer cancel()
	verifyErr := s.verifyClaimedMultipart(verifyCtx, request)
	if verifyErr == nil {
		proofCtx := context.WithValue(verifyCtx, multipartVerificationProofKey{}, completionFingerprint(request))
		_, verifyErr = s.CompleteUpload(proofCtx, request)
	}
	if verifyErr == nil {
		return true, nil
	}
	failure := multipartVerificationFailure(verifyErr)
	retry := s.now().UTC().Add(time.Minute)
	if failure != "UNAVAILABLE" {
		retry = s.now().UTC().Add(multipartLifetime)
	}
	// A request cancellation must still release its durable claim when the DB is reachable.
	recoveryCtx, recoveryCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer recoveryCancel()
	_, err = s.db.Exec(recoveryCtx, `UPDATE upload_intents SET multipart_verification_claim=NULL,multipart_verification_lease=NULL,
 multipart_verification_retry_at=$3, multipart_verification_error=CASE WHEN $2='UNAVAILABLE' AND multipart_verification_attempts<3 THEN '' ELSE $2 END
 WHERE asset_version_id=$1::uuid AND multipart_verification_claim=$4::uuid AND multipart_status='ASSEMBLED' AND completed_at IS NULL`, request.AssetVersionID, failure, retry, claim)
	if err != nil {
		return true, errors.Join(verifyErr, err)
	}
	return true, fmt.Errorf("multipart verification %s: %w", request.AssetVersionID, verifyErr)
}

func (s *Service) verifyClaimedMultipart(ctx context.Context, request CompleteUploadRequest) error {
	var kind AssetKind
	var maxSize int64
	if err := s.db.QueryRow(ctx, `SELECT v.kind,ui.max_size_bytes FROM upload_intents ui JOIN media_asset_versions v ON v.id=ui.asset_version_id WHERE ui.asset_version_id=$1::uuid`, request.AssetVersionID).Scan(&kind, &maxSize); err != nil {
		return err
	}
	// Exact immutable reads run without a PostgreSQL transaction or row lock.
	return s.verifyCompletedObject(ctx, request, kind, maxSize)
}

func multipartVerificationFailure(err error) string {
	switch {
	case errors.Is(err, ErrContentTypeMismatch):
		return "CONTENT_TYPE_MISMATCH"
	case errors.Is(err, ErrValidation), errors.Is(err, ErrNotFound):
		return "INVALID_OBJECT"
	case errors.Is(err, ErrUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "UNAVAILABLE"
	default:
		return "CONFLICT"
	}
}

func (s *Service) claimMultipartVerification(ctx context.Context) (CompleteUploadRequest, string, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CompleteUploadRequest{}, "", err
	}
	defer tx.Rollback(ctx)
	var request CompleteUploadRequest
	err = tx.QueryRow(ctx, `
 SELECT ui.asset_version_id::text,a.owner_account_id::text,ui.expected_object_key,ui.multipart_object_version,
 ui.expected_content_type,ui.expected_size_bytes,ui.multipart_sha256
 FROM upload_intents ui JOIN media_asset_versions v ON v.id=ui.asset_version_id JOIN media_assets a ON a.id=v.logical_asset_id
 WHERE ui.is_multipart AND ui.multipart_status='ASSEMBLED' AND ui.completed_at IS NULL
 AND ui.multipart_verification_error='' AND ui.multipart_verification_retry_at<=now()
 AND (ui.multipart_verification_claim IS NULL OR ui.multipart_verification_lease<=now())
 ORDER BY ui.created_at,ui.id LIMIT 1 FOR UPDATE OF ui SKIP LOCKED`).Scan(&request.AssetVersionID, &request.OwnerAccountID, &request.StorageObjectKey,
		&request.StorageObjectVersion, &request.ContentType, &request.SizeBytes, &request.SHA256Hex)
	if err != nil {
		return CompleteUploadRequest{}, "", err
	}
	claim := uuid.NewString()
	_, err = tx.Exec(ctx, `UPDATE upload_intents SET multipart_verification_claim=$2::uuid,multipart_verification_lease=now()+interval '16 minutes',multipart_verification_attempts=multipart_verification_attempts+1 WHERE asset_version_id=$1::uuid`, request.AssetVersionID, claim)
	if err != nil {
		return CompleteUploadRequest{}, "", err
	}
	request.ProviderEventID = "multipart:" + request.AssetVersionID
	return request, claim, tx.Commit(ctx)
}
