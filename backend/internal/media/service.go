package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/outbox"
)

const (
	callbackUploadCompleted = "UPLOAD_COMPLETED"
	callbackTranscode       = "TRANSCODE_COMPLETED"
	mediaSourceModule       = "MEDIA_AND_ASSETS"

	maxTypeProbeBytes int64 = 4096
)

var sha256Pattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

var errCatalogueCallbackRace = errors.New("catalogue callback was recorded concurrently")

// allowedContentTypes is the BR-067 bucket allowlist, which is wider than the
// D-088 no-malware-scan profile. Membership here means the type may be uploaded
// at all; TrustedProfileAdmits decides which of those may skip scanning.
var allowedContentTypes = map[AssetKind]map[string]struct{}{
	KindVideo: {
		"video/mp4": {}, "video/quicktime": {},
	},
	KindResource: {
		"application/pdf": {}, ContentTypeDOCX: {},
		"image/jpeg": {}, "image/png": {}, "image/webp": {},
	},
	KindLabMaterial: {
		"application/pdf": {}, "image/jpeg": {}, "image/png": {}, "image/webp": {},
		"application/zip": {}, "application/x-7z-compressed": {},
	},
	KindPreview: {
		"video/mp4": {}, "video/quicktime": {}, "application/pdf": {},
		"image/jpeg": {}, "image/png": {}, "image/webp": {},
	},
}

type Service struct {
	db              *pgxpool.Pool
	store           ObjectStore
	outbox          *outbox.Writer
	scanner         *ScannerAdapter
	uploadURLExpiry time.Duration
	maxUploadBytes  int64
	limits          uploadLimits
	operatingMode   OperatingMode
	now             func() time.Time
}

type uploadRecord struct {
	assetVersionID string
	objectKey      string
	objectVersion  string
	expiresAt      time.Time
}

type uploadCompletionRecord struct {
	owner        string
	ownerRole    string
	ownerStatus  string
	kind         AssetKind
	state        AssetVersionState
	expectedKey  string
	expectedType string
	expectedSize int64
	maxSize      int64
}

type uploadCompletion struct {
	request     CompleteUploadRequest
	fingerprint string
	kind        AssetKind
	maxSize     int64
}

// NewService validates every dependency required by the D7 pipeline. A
// missing scanner may be represented by NewUnavailableScanner, but a nil
// scanner boundary itself is a construction error; there is no successful
// fallback path.
func NewService(options ServiceOptions) (*Service, error) {
	if options.DB == nil {
		return nil, errors.New("media database is required")
	}
	if options.Store == nil {
		return nil, errors.New("media object store is required")
	}
	if options.Outbox == nil {
		return nil, errors.New("media outbox writer is required")
	}
	if options.Scanner == nil {
		return nil, ErrScannerRequired
	}
	if options.UploadURLExpiry <= 0 {
		return nil, errors.New("media upload URL expiry must be positive")
	}
	if options.MaxUploadBytes <= 0 {
		return nil, errors.New("media maximum upload size must be positive")
	}
	mode := options.OperatingMode
	if mode == "" {
		mode = OperatingModeScanner
	}
	if !mode.Valid() {
		return nil, errors.New("media operating mode is invalid")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: options.DB, store: options.Store, outbox: options.Outbox,
		scanner: options.Scanner, uploadURLExpiry: options.UploadURLExpiry,
		maxUploadBytes: options.MaxUploadBytes, limits: resolveUploadLimits(options),
		operatingMode: mode, now: now,
	}, nil
}

// BeginUpload creates one immutable Asset Version and an upload intent before
// issuing a private quarantine target. A replacement is represented by a new
// version ID and never rewrites the prior version.
//
// In TRUSTED_INSTRUCTOR mode it refuses anything outside the D-088 profile
// rather than issuing an intent the deployment cannot finish. That deployment
// has no scanner, so a preview, a Lab Material, or an unapproved type would
// otherwise upload successfully and then sit quarantined and undeliverable
// forever — a silent dead end instead of an answer the Instructor can act on.
func (s *Service) BeginUpload(ctx context.Context, request UploadRequest) (UploadTicket, error) {
	if s.operatingMode == OperatingModeAdminCatalogue {
		return UploadTicket{}, ErrNotAuthorized
	}
	if s.operatingMode == OperatingModeTrustedInstructor && !TrustedProfileAdmits(request.Kind, request.ContentType) {
		return UploadTicket{}, fmt.Errorf(
			"%w: this deployment accepts only MP4 Lesson video, PDF or DOCX Lesson Resources, and an MP4 public Course preview",
			ErrValidation)
	}
	return s.beginUploadForOwner(ctx, request)
}

func (s *Service) beginUploadForOwner(ctx context.Context, request UploadRequest) (UploadTicket, error) {
	if err := validateBeginUpload(request, s.limits.perFile(request.Kind)); err != nil {
		return UploadTicket{}, err
	}
	record := newUploadRecord(request, s.now, s.uploadURLExpiry)
	if err := s.persistUpload(ctx, request, record); err != nil {
		return UploadTicket{}, err
	}
	uploadURL, err := s.store.PresignPutURL(ctx, record.objectKey, request.ContentType, s.uploadURLExpiry)
	if err != nil {
		return UploadTicket{}, fmt.Errorf("%w: presigning quarantine upload: %v", ErrUnavailable, err)
	}
	return UploadTicket{
		AssetVersionID:   record.assetVersionID,
		UploadURL:        uploadURL,
		StorageObjectKey: record.objectKey,
		ExpiresAt:        record.expiresAt,
	}, nil
}

// BeginCatalogueLoad is the explicit LG-014 operating path. It still creates
// only a private quarantine target; no mode switch can make its bytes READY.
func (s *Service) BeginCatalogueLoad(ctx context.Context, request CatalogueLoadRequest) (UploadTicket, error) {
	if s.operatingMode != OperatingModeAdminCatalogue {
		return UploadTicket{}, ErrNotAuthorized
	}
	if err := validateUploadRequest(UploadRequest{Kind: request.Kind, ContentType: request.ContentType, SizeBytes: request.SizeBytes}, s.limits.perFile(request.Kind)); err != nil {
		return UploadTicket{}, err
	}
	if _, err := uuid.Parse(request.AdminAccountID); err != nil {
		return UploadTicket{}, ErrNotAuthorized
	}
	if _, err := uuid.Parse(request.CourseID); err != nil {
		return UploadTicket{}, ErrValidation
	}
	var ownerID string
	err := s.db.QueryRow(ctx, `
		SELECT c.owner_account_id::text
		FROM courses c JOIN accounts a ON a.id = $1::uuid
		WHERE c.id = $2::uuid AND a.role = 'ADMIN' AND a.status = 'ACTIVE'
	`, request.AdminAccountID, request.CourseID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return UploadTicket{}, ErrNotAuthorized
	}
	if err != nil {
		return UploadTicket{}, fmt.Errorf("authorizing catalogue load: %w", err)
	}
	return s.beginUploadForOwner(ctx, UploadRequest{
		OwnerAccountID: ownerID, CourseID: request.CourseID, LessonID: request.LessonID,
		LogicalAssetID: request.LogicalAssetID, Kind: request.Kind,
		ContentType: request.ContentType, SizeBytes: request.SizeBytes,
	})
}

// CompleteCatalogueLoad verifies the actual private object exactly as normal
// completion does, but requires an Admin in catalogue mode and deliberately
// does not enqueue scanner work. The next required operation is immutable
// out-of-band evidence for this exact object version.
func (s *Service) CompleteCatalogueLoad(ctx context.Context, request CatalogueCompletionRequest) (CompletionResult, error) {
	if s.operatingMode != OperatingModeAdminCatalogue {
		return CompletionResult{}, ErrNotAuthorized
	}
	completion, err := catalogueCompletionRequest(request)
	if err != nil {
		return CompletionResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("%w: beginning catalogue completion: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	record, duplicate, err := s.authorizeCatalogueCompletion(ctx, tx, request.AdminAccountID, completion)
	if err != nil {
		return CompletionResult{}, err
	}
	if duplicate {
		if err := tx.Commit(ctx); err != nil {
			return CompletionResult{}, fmt.Errorf("committing duplicate catalogue callback: %w", err)
		}
		return CompletionResult{AssetVersionID: completion.AssetVersionID, State: record.state, Duplicate: true}, nil
	}
	if err := s.verifyCompletedObject(ctx, completion, record.kind, record.maxSize); err != nil {
		return CompletionResult{}, err
	}
	if err := s.quarantineCatalogueObject(ctx, tx, completion); err != nil {
		if errors.Is(err, errCatalogueCallbackRace) {
			_ = tx.Rollback(ctx)
			return s.resolveDuplicateUpload(ctx, completion, completionFingerprint(completion))
		}
		return CompletionResult{}, err
	}
	if err := appendMediaAudit(ctx, tx, request.AdminAccountID, "ADMIN", "MEDIA_CATALOGUE_LOADED", completion.AssetVersionID, "Admin catalogue load completed into quarantine", map[string]any{"state": string(StateQuarantined)}); err != nil {
		return CompletionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletionResult{}, fmt.Errorf("committing catalogue completion: %w", err)
	}
	return CompletionResult{AssetVersionID: completion.AssetVersionID, State: StateQuarantined}, nil
}

func catalogueCompletionRequest(request CatalogueCompletionRequest) (CompleteUploadRequest, error) {
	completion := CompleteUploadRequest{
		OwnerAccountID: request.AdminAccountID, AssetVersionID: request.AssetVersionID,
		ProviderEventID: request.ProviderEventID, StorageObjectKey: request.StorageObjectKey,
		StorageObjectVersion: request.StorageObjectVersion, ContentType: request.ContentType,
		SizeBytes: request.SizeBytes, SHA256Hex: request.SHA256Hex,
	}
	if err := validateCompletionRequest(completion); err != nil {
		return CompleteUploadRequest{}, err
	}
	return completion, nil
}

func (s *Service) authorizeCatalogueCompletion(ctx context.Context, tx pgx.Tx, adminID string, completion CompleteUploadRequest) (uploadCompletionRecord, bool, error) {
	if err := s.requireActiveAdmin(ctx, tx, adminID); err != nil {
		return uploadCompletionRecord{}, false, err
	}
	record, err := s.loadUploadCompletionRecord(ctx, tx, completion.AssetVersionID)
	if err != nil {
		return uploadCompletionRecord{}, false, err
	}
	if completion.StorageObjectKey != record.expectedKey || completion.ContentType != record.expectedType || completion.SizeBytes != record.expectedSize || completion.SizeBytes > record.maxSize {
		return uploadCompletionRecord{}, false, ErrConflict
	}
	duplicate, err := catalogueCallbackAlreadyRecorded(ctx, tx, completion)
	if err != nil {
		return uploadCompletionRecord{}, false, err
	}
	if !duplicate && record.state != StateUploaded {
		return uploadCompletionRecord{}, false, ErrConflict
	}
	return record, duplicate, nil
}

func catalogueCallbackAlreadyRecorded(ctx context.Context, tx pgx.Tx, completion CompleteUploadRequest) (bool, error) {
	fingerprint := completionFingerprint(completion)
	var priorAsset, priorFingerprint string
	err := tx.QueryRow(ctx, `
		SELECT asset_version_id::text, request_fingerprint
		FROM media_callback_receipts
		WHERE callback_kind = $1 AND provider_event_id = $2
	`, callbackUploadCompleted, completion.ProviderEventID).Scan(&priorAsset, &priorFingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking catalogue callback receipt: %w", err)
	}
	if priorAsset != completion.AssetVersionID || priorFingerprint != fingerprint {
		return false, ErrConflict
	}
	return true, nil
}

func (s *Service) quarantineCatalogueObject(ctx context.Context, tx pgx.Tx, completion CompleteUploadRequest) error {
	fingerprint := completionFingerprint(completion)
	recorded, err := tx.Exec(ctx, `
		INSERT INTO media_callback_receipts (provider_event_id, callback_kind, asset_version_id, object_version, request_fingerprint)
		VALUES ($1, $2, $3::uuid, $4, $5)
		ON CONFLICT (callback_kind, provider_event_id) DO NOTHING
	`, completion.ProviderEventID, callbackUploadCompleted, completion.AssetVersionID, completion.StorageObjectVersion, fingerprint)
	if err != nil {
		return fmt.Errorf("recording catalogue completion: %w", err)
	}
	if recorded.RowsAffected() != 1 {
		return errCatalogueCallbackRace
	}
	updated, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET storage_object_version = $1, size_bytes = $2, sha256_hex = $3, state = 'QUARANTINED'
		WHERE id = $4::uuid AND state = 'UPLOADED'
	`, completion.StorageObjectVersion, completion.SizeBytes, strings.ToLower(completion.SHA256Hex), completion.AssetVersionID)
	if err != nil {
		return fmt.Errorf("quarantining catalogue asset: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	updated, err = tx.Exec(ctx, `UPDATE upload_intents SET completed_at = now(), completion_fingerprint = $1 WHERE asset_version_id = $2::uuid AND completed_at IS NULL`, fingerprint, completion.AssetVersionID)
	if err != nil {
		return fmt.Errorf("completing catalogue upload intent: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return ErrConcurrentModification
	}
	return nil
}

// RecordOutOfBandScanEvidence is an Admin-only, append-only record for the
// exact quarantine object version. It cannot reuse an earlier Asset Version's
// pass, and it never operates outside explicit Admin-catalogue mode.
func (s *Service) RecordOutOfBandScanEvidence(ctx context.Context, evidence OutOfBandScanEvidence) error {
	if !validOutOfBandEvidence(s.operatingMode, evidence) {
		return ErrNotAuthorized
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: beginning out-of-band evidence: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if err := s.requireActiveAdmin(ctx, tx, evidence.AdminAccountID); err != nil {
		return err
	}
	target, err := startCatalogueEvidenceScan(ctx, tx, evidence)
	if err != nil {
		return err
	}
	attempt, err := recordCatalogueEvidenceAttempt(ctx, tx, evidence, target.objectVersion)
	if err != nil {
		return err
	}
	if err := s.applyCatalogueEvidence(ctx, tx, evidence, target.kind, attempt.id); err != nil {
		return err
	}
	if err := appendMediaAudit(ctx, tx, evidence.AdminAccountID, "ADMIN", "MEDIA_OUT_OF_BAND_SCAN_RECORDED", evidence.AssetVersionID, "Out-of-band exact-version scan evidence recorded", map[string]any{"method": evidence.Method, "provider": evidence.Provider, "reference": evidence.Reference, "object_version": target.objectVersion, "attempt_number": attempt.number}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type catalogueEvidenceTarget struct {
	kind          AssetKind
	objectVersion string
}

type catalogueEvidenceAttempt struct {
	id     string
	number int
}

func validOutOfBandEvidence(mode OperatingMode, evidence OutOfBandScanEvidence) bool {
	return mode == OperatingModeAdminCatalogue && evidence.AdminAccountID != "" && evidence.AssetVersionID != "" &&
		evidence.StorageObjectVersion != "" && strings.TrimSpace(evidence.Method) != "" &&
		strings.TrimSpace(evidence.Provider) != "" && strings.TrimSpace(evidence.Reference) != ""
}

func startCatalogueEvidenceScan(ctx context.Context, tx pgx.Tx, evidence OutOfBandScanEvidence) (catalogueEvidenceTarget, error) {
	var state AssetVersionState
	var target catalogueEvidenceTarget
	err := tx.QueryRow(ctx, `SELECT state, kind, storage_object_version FROM media_asset_versions WHERE id = $1::uuid FOR UPDATE`, evidence.AssetVersionID).Scan(&state, &target.kind, &target.objectVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return catalogueEvidenceTarget{}, ErrNotFound
	}
	if err != nil {
		return catalogueEvidenceTarget{}, fmt.Errorf("loading catalogue evidence target: %w", err)
	}
	if state != StateQuarantined || target.objectVersion != evidence.StorageObjectVersion {
		return catalogueEvidenceTarget{}, ErrConflict
	}
	if err := Transition(state, StateScanning); err != nil {
		return catalogueEvidenceTarget{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'SCANNING' WHERE id = $1::uuid`, evidence.AssetVersionID); err != nil {
		return catalogueEvidenceTarget{}, fmt.Errorf("starting recorded scan: %w", err)
	}
	return target, nil
}

func recordCatalogueEvidenceAttempt(ctx context.Context, tx pgx.Tx, evidence OutOfBandScanEvidence, objectVersion string) (catalogueEvidenceAttempt, error) {
	var attempt catalogueEvidenceAttempt
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(attempt_number), 0) + 1 FROM scan_attempts WHERE asset_version_id = $1::uuid AND storage_object_version = $2`, evidence.AssetVersionID, objectVersion).Scan(&attempt.number); err != nil {
		return catalogueEvidenceAttempt{}, fmt.Errorf("allocating evidence attempt: %w", err)
	}
	workID := "out-of-band:" + uuid.NewString()
	if err := tx.QueryRow(ctx, `
		INSERT INTO scan_attempts (asset_version_id, attempt_number, work_id, storage_object_version, outcome, scanner_identity, reason)
		VALUES ($1::uuid, $2, $3, $4, 'PASSED', $5, $6) RETURNING id::text
	`, evidence.AssetVersionID, attempt.number, workID, objectVersion, "out-of-band:"+evidence.Provider, evidence.Method+":"+evidence.Reference).Scan(&attempt.id); err != nil {
		return catalogueEvidenceAttempt{}, fmt.Errorf("recording out-of-band evidence: %w", err)
	}
	return attempt, nil
}

func (s *Service) applyCatalogueEvidence(ctx context.Context, tx pgx.Tx, evidence OutOfBandScanEvidence, kind AssetKind, attemptID string) error {
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET successful_scan_attempt_id = $1::uuid, state = 'SCAN_PASSED' WHERE id = $2::uuid AND state = 'SCANNING'`, attemptID, evidence.AssetVersionID); err != nil {
		return fmt.Errorf("applying out-of-band scan evidence: %w", err)
	}
	if kind == KindVideo {
		return appendTranscodeWork(ctx, tx, s.outbox, evidence.AssetVersionID, "out-of-band:"+evidence.Reference)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid AND state = 'SCAN_PASSED'`, evidence.AssetVersionID); err != nil {
		return fmt.Errorf("making scanned catalogue asset ready: %w", err)
	}
	return nil
}

func validateBeginUpload(request UploadRequest, maxUploadBytes int64) error {
	if err := validateUploadRequest(request, maxUploadBytes); err != nil {
		return err
	}
	if request.Kind == KindPreview {
		if request.LessonID != "" || request.RevisionID == "" {
			return fmt.Errorf("%w: public preview requires a Course revision and cannot target a Lesson", ErrValidation)
		}
		if strings.ToLower(strings.TrimSpace(request.ContentType)) != "video/mp4" {
			return fmt.Errorf("%w: public preview must be an MP4 video", ErrValidation)
		}
	} else if request.RevisionID != "" {
		return fmt.Errorf("%w: only a public preview can target a Course revision", ErrValidation)
	}
	for _, identity := range []struct {
		name  string
		value string
	}{
		{name: "owner account", value: request.OwnerAccountID},
		{name: "course", value: request.CourseID},
		{name: "Course revision", value: request.RevisionID},
		{name: "lesson", value: request.LessonID},
		{name: "logical asset", value: request.LogicalAssetID},
	} {
		if identity.value == "" && (identity.name == "Course revision" || identity.name == "lesson" || identity.name == "logical asset") {
			continue
		}
		if _, err := uuid.Parse(identity.value); err != nil {
			return fmt.Errorf("%w: %s ID is invalid", ErrValidation, identity.name)
		}
	}
	return nil
}

func newUploadRecord(request UploadRequest, now func() time.Time, expiry time.Duration) uploadRecord {
	assetVersionID := uuid.NewString()
	return uploadRecord{
		assetVersionID: assetVersionID,
		objectKey:      fmt.Sprintf("quarantine/%s/%s/source", request.CourseID, assetVersionID),
		objectVersion:  "pending:" + assetVersionID,
		expiresAt:      now().UTC().Add(expiry),
	}
}

func (s *Service) persistUpload(ctx context.Context, request UploadRequest, record uploadRecord) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: beginning media upload transaction: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if err := s.requireCourseOwner(ctx, tx, request.CourseID, request.OwnerAccountID); err != nil {
		return err
	}
	if err := requirePreviewRevision(ctx, tx, request); err != nil {
		return err
	}
	if err := s.enforceLessonAggregate(ctx, tx, request); err != nil {
		return err
	}
	logicalAssetID, err := s.ensureLogicalAsset(ctx, tx, request)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes
		) VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'UPLOADED', $4, $5, $6, $7)
	`, record.assetVersionID, logicalAssetID, request.Kind, record.objectKey, record.objectVersion, request.ContentType, request.SizeBytes); err != nil {
		return fmt.Errorf("creating media asset version: %w", err)
	}
	intentCreatedAt, err := reserveUploadIntentOrder(ctx, tx, request)
	if err != nil {
		return err
	}
	// The intent stores the bound that actually applies to this bucket, not the
	// deployment ceiling, so completion re-checks the same number the request
	// was admitted against.
	if _, err := tx.Exec(ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at, created_at
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, COALESCE($7, now()))
	`, record.assetVersionID, record.objectKey, request.ContentType, request.SizeBytes, s.limits.perFile(request.Kind), record.expiresAt, intentCreatedAt); err != nil {
		return fmt.Errorf("creating media upload intent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing media upload intent: %w", err)
	}
	return nil
}

// reserveUploadIntentOrder gives the replaceable media buckets a strictly
// increasing intent order.
//
// Selection asks "is a newer completed upload already chosen?", so the answer
// has to come from something durable and monotonic rather than from whichever
// completion callback happened to arrive first. Two intents created in the same
// clock tick would otherwise tie, and the tiebreaker — a random UUID — is not
// chronological, so a stale upload could win.
//
// Two buckets need it: Lesson video, which is one asset per Lesson, and the
// public preview, which is one asset per Course revision. Everything else is
// additive and has no selection to lose.
func reserveUploadIntentOrder(ctx context.Context, tx pgx.Tx, request UploadRequest) (*time.Time, error) {
	var scopeID string
	switch {
	case request.Kind == KindVideo && request.LessonID != "":
		scopeID = request.LessonID
	case request.Kind == KindPreview && request.RevisionID != "":
		scopeID = request.RevisionID
	default:
		return nil, nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`,
		replaceableIntentLockClass, lessonAggregateLockKey(scopeID, request.Kind)); err != nil {
		return nil, fmt.Errorf("locking %s intent order: %w", request.Kind, err)
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, `
		SELECT GREATEST(
			clock_timestamp(),
			COALESCE(MAX(ui.created_at) + interval '1 microsecond', '-infinity'::timestamptz)
		)
		FROM upload_intents ui
		JOIN media_asset_versions mav ON mav.id = ui.asset_version_id
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		WHERE ma.course_id = $1::uuid
		  AND ma.kind = $2::media_asset_kind
		  AND (
			($2 = 'VIDEO' AND ma.lesson_id = $3::uuid)
			OR ($2 = 'PREVIEW' AND ma.preview_origin_revision_id = $3::uuid)
		  )
	`, request.CourseID, request.Kind, scopeID).Scan(&createdAt); err != nil {
		return nil, fmt.Errorf("reserving %s intent order: %w", request.Kind, err)
	}
	return &createdAt, nil
}

// One advisory namespace for both replaceable buckets. The key already mixes
// the kind in, so a Lesson video and a public preview cannot collide.
const replaceableIntentLockClass int32 = 0x766964 // "vid"

func (s *Service) ensureLogicalAsset(ctx context.Context, tx pgx.Tx, request UploadRequest) (string, error) {
	if request.LogicalAssetID == "" {
		var assetID string
		err := tx.QueryRow(ctx, `
			INSERT INTO media_assets (
				kind, owner_account_id, course_id, lesson_id,
				preview_origin_revision_id, visibility
			)
			VALUES (
				$1::media_asset_kind, $2::uuid, $3::uuid, NULLIF($4, '')::uuid,
				NULLIF($5, '')::uuid, $6::media_asset_visibility
			)
			RETURNING id::text
		`, request.Kind, request.OwnerAccountID, request.CourseID, request.LessonID,
			request.RevisionID, visibilityForKind(request.Kind)).Scan(&assetID)
		if err != nil {
			return "", fmt.Errorf("creating logical media asset: %w", err)
		}
		return assetID, nil
	}
	return s.verifyLogicalAsset(ctx, tx, request)
}

func (s *Service) verifyLogicalAsset(ctx context.Context, tx pgx.Tx, request UploadRequest) (string, error) {
	var owner, course string
	var kind AssetKind
	var originRevisionID *string
	err := tx.QueryRow(ctx, `
		SELECT owner_account_id::text, course_id::text, kind, preview_origin_revision_id::text
		FROM media_assets WHERE id = $1::uuid FOR SHARE
	`, request.LogicalAssetID).Scan(&owner, &course, &kind, &originRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("loading logical media asset: %w", err)
	}
	if owner != request.OwnerAccountID || course != request.CourseID || kind != request.Kind ||
		!sameOptionalID(originRevisionID, request.RevisionID) {
		return "", ErrNotAuthorized
	}
	return request.LogicalAssetID, nil
}

// requirePreviewRevision binds a newly-uploaded public preview to the active
// candidate it was created for. The storage intent is rejected before any
// signed PUT URL is issued, so an Asset Version cannot later be substituted
// across Courses or revisions simply by guessing its immutable ID.
func requirePreviewRevision(ctx context.Context, tx pgx.Tx, request UploadRequest) error {
	if request.Kind != KindPreview {
		return nil
	}
	var state string
	err := tx.QueryRow(ctx, `
		SELECT state::text
		FROM course_revisions
		WHERE id = $1::uuid AND course_id = $2::uuid
		FOR SHARE
	`, request.RevisionID, request.CourseID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAuthorized
	}
	if err != nil {
		return fmt.Errorf("checking public-preview revision: %w", err)
	}
	if state != "DRAFT" && state != "CHANGES_REQUESTED" {
		return ErrConflict
	}
	return nil
}

func sameOptionalID(stored *string, requested string) bool {
	if requested == "" {
		return stored == nil || *stored == ""
	}
	return stored != nil && *stored == requested
}

// CompleteUpload authorizes and locks the immutable upload intent before it
// performs the bounded object inspection and full trusted hash. Holding the
// target row lock makes duplicate provider callbacks converge without letting
// an unauthorized caller force a full read of somebody else's object. The
// provider event ID is the database-level idempotency key; no process-local
// lock participates in convergence.
func (s *Service) CompleteUpload(ctx context.Context, request CompleteUploadRequest) (CompletionResult, error) {
	if err := validateCompletionRequest(request); err != nil {
		return CompletionResult{}, err
	}

	fingerprint := completionFingerprint(request)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CompletionResult{}, fmt.Errorf("%w: beginning upload completion transaction: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)

	record, err := s.loadUploadCompletionRecord(ctx, tx, request.AssetVersionID)
	if err != nil {
		return CompletionResult{}, err
	}
	if record.owner != request.OwnerAccountID || record.ownerRole != "INSTRUCTOR" || record.ownerStatus != "ACTIVE" {
		return CompletionResult{}, ErrNotAuthorized
	}
	if request.StorageObjectKey != record.expectedKey || request.ContentType != record.expectedType || request.SizeBytes != record.expectedSize || request.SizeBytes > record.maxSize {
		return CompletionResult{}, fmt.Errorf("%w: completion evidence does not match the upload intent", ErrConflict)
	}
	state := record.state

	var priorAsset, priorFingerprint string
	err = tx.QueryRow(ctx, `
		SELECT asset_version_id::text, request_fingerprint
		FROM media_callback_receipts
		WHERE callback_kind = $1 AND provider_event_id = $2
	`, callbackUploadCompleted, request.ProviderEventID).Scan(&priorAsset, &priorFingerprint)
	if err == nil {
		if priorAsset != request.AssetVersionID || priorFingerprint != fingerprint {
			return CompletionResult{}, fmt.Errorf("%w: provider event was replayed with different evidence", ErrConflict)
		}
		if err := tx.Commit(ctx); err != nil {
			return CompletionResult{}, fmt.Errorf("committing duplicate upload callback: %w", err)
		}
		return CompletionResult{AssetVersionID: request.AssetVersionID, State: AssetVersionState(state), Duplicate: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CompletionResult{}, fmt.Errorf("checking upload callback receipt: %w", err)
	}
	if state != StateUploaded {
		return CompletionResult{}, fmt.Errorf("%w: media upload has already left the upload state", ErrConflict)
	}

	// Head, format, and hash inspection are all performed against the exact
	// stored object version. HashObjectVersion deliberately reads the complete
	// object and therefore only runs after ownership, intent identity,
	// idempotency, and current-state checks have passed.
	if err := s.verifyCompletedObject(ctx, request, record.kind, record.maxSize); err != nil {
		return CompletionResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO media_callback_receipts (
			provider_event_id, callback_kind, asset_version_id, object_version, request_fingerprint
		) VALUES ($1, $2, $3::uuid, $4, $5)
	`, request.ProviderEventID, callbackUploadCompleted, request.AssetVersionID, request.StorageObjectVersion, fingerprint); err != nil {
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)
			return s.resolveDuplicateUpload(ctx, request, fingerprint)
		}
		return CompletionResult{}, fmt.Errorf("recording upload callback receipt: %w", err)
	}

	if state == StateUploaded {
		completion := uploadCompletion{
			request: request, fingerprint: fingerprint,
			kind: record.kind, maxSize: record.maxSize,
		}
		next, err := s.quarantineUpload(ctx, tx, completion)
		if err != nil {
			return CompletionResult{}, err
		}
		state = next
	}

	if err := tx.Commit(ctx); err != nil {
		return CompletionResult{}, fmt.Errorf("committing upload completion: %w", err)
	}
	return CompletionResult{AssetVersionID: request.AssetVersionID, State: state}, nil
}

func (s *Service) loadUploadCompletionRecord(ctx context.Context, tx pgx.Tx, assetVersionID string) (uploadCompletionRecord, error) {
	var record uploadCompletionRecord
	err := tx.QueryRow(ctx, `
		SELECT ma.owner_account_id::text, owner.role, owner.status, mav.kind, mav.state,
		       ui.expected_object_key, ui.expected_content_type,
		       ui.expected_size_bytes, ui.max_size_bytes
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN accounts owner ON owner.id = ma.owner_account_id
		JOIN upload_intents ui ON ui.asset_version_id = mav.id
		WHERE mav.id = $1::uuid
		FOR UPDATE OF mav, ui
	`, assetVersionID).Scan(&record.owner, &record.ownerRole, &record.ownerStatus, &record.kind, &record.state, &record.expectedKey, &record.expectedType, &record.expectedSize, &record.maxSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return uploadCompletionRecord{}, ErrNotFound
	}
	if err != nil {
		return uploadCompletionRecord{}, fmt.Errorf("loading media upload intent: %w", err)
	}
	return record, nil
}

// quarantineUpload binds the exact stored object version to the Asset Version,
// closes the intent, and then selects the one authorized safety path for these
// bytes. It returns the state the Asset Version actually reached.
//
// Every upload lands in private quarantine first. From there a D-088 upload
// carries its exact-version validation evidence forward, and everything else
// gets one committed scan-work intent. The two paths are mutually exclusive
// and neither can produce the other's evidence.
func (s *Service) quarantineUpload(ctx context.Context, tx pgx.Tx, completion uploadCompletion) (AssetVersionState, error) {
	request := completion.request
	commandTag, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET storage_object_version = $1, size_bytes = $2, sha256_hex = NULLIF($3, ''), state = 'QUARANTINED'
		WHERE id = $4::uuid AND state = 'UPLOADED'
	`, request.StorageObjectVersion, request.SizeBytes, strings.ToLower(request.SHA256Hex), request.AssetVersionID)
	if err != nil {
		return "", fmt.Errorf("quarantining media asset version: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return "", ErrConcurrentModification
	}
	if _, err := tx.Exec(ctx, `
		UPDATE upload_intents
		SET completed_at = now(), completion_fingerprint = $1
		WHERE asset_version_id = $2::uuid AND completed_at IS NULL
	`, completion.fingerprint, request.AssetVersionID); err != nil {
		return "", fmt.Errorf("completing media upload intent: %w", err)
	}

	if s.trustedPathApplies(completion.kind, request.ContentType) {
		return s.applyTrustedValidation(ctx, tx, completion, "upload:"+request.ProviderEventID)
	}

	if err := s.appendScanWork(ctx, tx, request.AssetVersionID, string(completion.kind), request.ProviderEventID); err != nil {
		return "", err
	}
	if err := appendMediaAudit(ctx, tx, request.OwnerAccountID, "INSTRUCTOR", "MEDIA_UPLOAD_COMPLETED", request.AssetVersionID, "Direct upload completed into quarantine", map[string]any{"state": string(StateQuarantined)}); err != nil {
		return "", err
	}
	return StateQuarantined, nil
}

// trustedPathApplies is the single place the D-088 no-malware-scan path is
// selected. Both the deployment's operating mode and the exact kind/type must
// admit it; either one saying no means the asset stays scanner-gated.
func (s *Service) trustedPathApplies(kind AssetKind, contentType string) bool {
	return s.operatingMode == OperatingModeTrustedInstructor && TrustedProfileAdmits(kind, contentType)
}

func (s *Service) resolveDuplicateUpload(ctx context.Context, request CompleteUploadRequest, fingerprint string) (CompletionResult, error) {
	var asset, priorFingerprint, state string
	err := s.db.QueryRow(ctx, `
		SELECT r.asset_version_id::text, r.request_fingerprint, mav.state
		FROM media_callback_receipts r
		JOIN media_asset_versions mav ON mav.id = r.asset_version_id
		WHERE r.callback_kind = $1 AND r.provider_event_id = $2
	`, callbackUploadCompleted, request.ProviderEventID).Scan(&asset, &priorFingerprint, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionResult{}, ErrConcurrentModification
	}
	if err != nil {
		return CompletionResult{}, fmt.Errorf("loading duplicate upload callback: %w", err)
	}
	if asset != request.AssetVersionID || priorFingerprint != fingerprint {
		return CompletionResult{}, fmt.Errorf("%w: provider event was replayed with different evidence", ErrConflict)
	}
	return CompletionResult{AssetVersionID: asset, State: AssetVersionState(state), Duplicate: true}, nil
}

// verifyCompletedObject proves the exact stored object version against the
// completion evidence: it exists, its actual size matches, its real format
// matches the declared type, and its SHA-256 matches. Every check reads the
// stored object; none of them trusts the caller's claim.
func (s *Service) verifyCompletedObject(ctx context.Context, request CompleteUploadRequest, kind AssetKind, maxSize int64) error {
	if err := s.verifyObjectSize(ctx, request); err != nil {
		return err
	}
	if err := s.verifyObjectContent(ctx, request, kind, maxSize); err != nil {
		return err
	}
	actualHash, err := s.store.HashObjectVersion(ctx, request.StorageObjectKey, request.StorageObjectVersion)
	if err != nil {
		return fmt.Errorf("%w: hashing uploaded object: %v", ErrUnavailable, err)
	}
	if !strings.EqualFold(actualHash, request.SHA256Hex) {
		return fmt.Errorf("%w: uploaded object checksum does not match completion evidence", ErrValidation)
	}
	return nil
}

func (s *Service) verifyObjectSize(ctx context.Context, request CompleteUploadRequest) error {
	actualSize, exists, err := s.store.HeadObjectVersion(ctx, request.StorageObjectKey, request.StorageObjectVersion)
	if err != nil {
		return fmt.Errorf("%w: verifying quarantine object: %v", ErrUnavailable, err)
	}
	if !exists {
		return ErrNotFound
	}
	if actualSize != request.SizeBytes {
		return fmt.Errorf("%w: uploaded object size does not match completion evidence", ErrValidation)
	}
	return nil
}

// verifyObjectContent proves the stored bytes really are the declared format.
//
// Most formats are decided by a bounded prefix. DOCX cannot be: an OOXML
// package's manifest lives in the ZIP central directory at the end of the
// object, so a prefix can only show a generic ZIP header and an arbitrary
// archive renamed to .docx would pass. The whole object is therefore inspected
// for DOCX, bounded by the intent's own configured size limit so an untrusted
// archive can never cost more than the upload it was admitted as.
func (s *Service) verifyObjectContent(ctx context.Context, request CompleteUploadRequest, kind AssetKind, maxSize int64) error {
	if strings.EqualFold(strings.TrimSpace(request.ContentType), ContentTypeDOCX) {
		return s.verifyDOCXObject(ctx, request, maxSize)
	}
	prefix, err := s.store.DownloadPrefixVersion(ctx, request.StorageObjectKey, request.StorageObjectVersion, maxTypeProbeBytes)
	if err != nil {
		return fmt.Errorf("%w: inspecting uploaded object: %v", ErrUnavailable, err)
	}
	if !contentMatchesDeclaredType(prefix, request.ContentType) {
		return fmt.Errorf("%w: uploaded bytes do not match the declared content type", ErrValidation)
	}
	return nil
}

// verifyDOCXObject reads the exact stored object and proves it is a macro-free
// OOXML WordprocessingML package. Nothing in the package is executed or
// written to disk; only its structure is parsed, under the bounds in
// defaultArchiveLimits.
func (s *Service) verifyDOCXObject(ctx context.Context, request CompleteUploadRequest, maxSize int64) error {
	bound := maxSize
	if bound <= 0 || bound > s.limits.perFile(KindResource) {
		bound = s.limits.perFile(KindResource)
	}
	if request.SizeBytes > bound {
		return fmt.Errorf("%w: the object is larger than the configured DOCX inspection bound", ErrValidation)
	}
	object, err := s.store.DownloadPrefixVersion(ctx, request.StorageObjectKey, request.StorageObjectVersion, bound)
	if err != nil {
		return fmt.Errorf("%w: inspecting uploaded object: %v", ErrUnavailable, err)
	}
	if int64(len(object)) != request.SizeBytes {
		return fmt.Errorf("%w: the stored object could not be read completely for inspection", ErrValidation)
	}
	if err := validateDOCXObject(object, defaultArchiveLimits()); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// GetStatus exposes only processing state and trusted metadata to the owning
// Instructor or an Admin. Object keys and signing material never enter this
// representation.
func (s *Service) GetStatus(ctx context.Context, versionID string, viewer Viewer) (AssetStatus, error) {
	if _, err := uuid.Parse(versionID); err != nil {
		return AssetStatus{}, ErrNotFound
	}
	if _, err := uuid.Parse(viewer.AccountID); err != nil || (viewer.Role != "INSTRUCTOR" && viewer.Role != "ADMIN") {
		return AssetStatus{}, ErrNotAuthorized
	}
	var status AssetStatus
	var owner, viewerRole, viewerStatus string
	err := s.db.QueryRow(ctx, `
		SELECT mav.id::text, mav.logical_asset_id::text, mav.kind, mav.state,
		       mav.size_bytes, mav.trusted_duration_ms, mav.created_at,
		       ma.owner_account_id::text, viewer.role, viewer.status
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN accounts viewer ON viewer.id = $2::uuid
		WHERE mav.id = $1::uuid
	`, versionID, viewer.AccountID).Scan(&status.AssetVersionID, &status.LogicalAssetID, &status.Kind, &status.State, &status.SizeBytes, &status.TrustedDurationMS, &status.CreatedAt, &owner, &viewerRole, &viewerStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return AssetStatus{}, ErrNotFound
	}
	if err != nil {
		return AssetStatus{}, fmt.Errorf("loading media state: %w", err)
	}
	if viewerStatus != "ACTIVE" || viewerRole != viewer.Role || (viewerRole != "ADMIN" && viewerRole != "INSTRUCTOR") {
		return AssetStatus{}, ErrNotAuthorized
	}
	if viewerRole != "ADMIN" && viewer.AccountID != owner {
		return AssetStatus{}, ErrNotAuthorized
	}
	return status, nil
}

// Retry re-enters quarantine and re-establishes the safety evidence the asset's
// own path requires. It never moves a failed version directly to PROCESSING or
// READY, and it never switches an asset from one safety path to the other.
//
// Which path is re-entered is decided by the provenance the Asset Version
// already carries, not by the current operating mode. A scanner-gated asset
// gets a fresh scan-work intent, exactly as before. A D-088 asset re-runs the
// full exact-version validation against the same immutable object version — a
// retry cannot inherit the earlier pass. If the deployment has since left
// TRUSTED_INSTRUCTOR mode, the retry is refused rather than routed into a
// malware scanner that is not there.
func (s *Service) Retry(ctx context.Context, request RetryRequest) error {
	if err := validateRetryRequest(request); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: beginning media retry transaction: %v", ErrUnavailable, err)
	}
	defer tx.Rollback(ctx)
	if err := s.requireActiveAdmin(ctx, tx, request.AdminAccountID); err != nil {
		return err
	}
	target, err := loadRetryTarget(ctx, tx, request.AssetVersionID)
	if err != nil {
		return err
	}
	if target.state != StateScanFailed && target.state != StateScanError && target.state != StateProcessFailed {
		return fmt.Errorf("%w: media version is not failed", ErrConflict)
	}
	if err := s.applyRetry(ctx, tx, request, target); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing media retry: %w", err)
	}
	return nil
}

func validateRetryRequest(request RetryRequest) error {
	if request.AdminAccountID == "" || request.ActorDescriptor == "" {
		return ErrNotAuthorized
	}
	if _, err := uuid.Parse(request.AdminAccountID); err != nil {
		return ErrNotAuthorized
	}
	if _, err := uuid.Parse(request.AssetVersionID); err != nil {
		return ErrNotFound
	}
	return nil
}

func (s *Service) requireActiveAdmin(ctx context.Context, tx pgx.Tx, accountID string) error {
	var role, status string
	err := tx.QueryRow(ctx, `SELECT role, status FROM accounts WHERE id = $1::uuid FOR SHARE`, accountID).Scan(&role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotAuthorized
	}
	if err != nil {
		return fmt.Errorf("checking media retry administrator: %w", err)
	}
	if role != "ADMIN" || status != "ACTIVE" {
		return ErrNotAuthorized
	}
	return nil
}

// retryTarget is the immutable record a retry must re-establish evidence for.
// Every field except state is fixed for the life of the Asset Version, so a
// retry is bound to the same bytes the original completion proved.
type retryTarget struct {
	state         AssetVersionState
	kind          AssetKind
	ownerID       string
	objectKey     string
	objectVersion string
	contentType   string
	sizeBytes     int64
	sha256Hex     string
	maxSize       int64
	trusted       bool
}

func loadRetryTarget(ctx context.Context, tx pgx.Tx, assetVersionID string) (retryTarget, error) {
	var target retryTarget
	err := tx.QueryRow(ctx, `
		SELECT mav.state, mav.kind, ma.owner_account_id::text, mav.storage_object_key,
		       mav.storage_object_version, mav.content_type, mav.size_bytes,
		       COALESCE(mav.sha256_hex, ''), ui.max_size_bytes,
		       mav.successful_validation_attempt_id IS NOT NULL
		FROM media_asset_versions mav
		JOIN media_assets ma ON ma.id = mav.logical_asset_id
		JOIN upload_intents ui ON ui.asset_version_id = mav.id
		WHERE mav.id = $1::uuid
		FOR UPDATE OF mav
	`, assetVersionID).Scan(&target.state, &target.kind, &target.ownerID, &target.objectKey,
		&target.objectVersion, &target.contentType, &target.sizeBytes, &target.sha256Hex,
		&target.maxSize, &target.trusted)
	if errors.Is(err, pgx.ErrNoRows) {
		return retryTarget{}, ErrNotFound
	}
	if err != nil {
		return retryTarget{}, fmt.Errorf("loading retryable media version: %w", err)
	}
	return target, nil
}

func (s *Service) applyRetry(ctx context.Context, tx pgx.Tx, request RetryRequest, target retryTarget) error {
	if err := Transition(target.state, StateQuarantined); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE media_asset_versions SET state = 'QUARANTINED' WHERE id = $1::uuid`, request.AssetVersionID); err != nil {
		return fmt.Errorf("resetting media version for retry: %w", err)
	}
	reached, err := s.reestablishRetryEvidence(ctx, tx, request, target)
	if err != nil {
		return err
	}
	metadata := map[string]any{
		"from_state": string(target.state),
		"to_state":   string(reached),
		"actor":      request.ActorDescriptor,
		"path":       retryPathLabel(target.trusted),
	}
	return appendMediaAudit(ctx, tx, request.AdminAccountID, "ADMIN", "MEDIA_PROCESSING_RETRIED", request.AssetVersionID, "Admin retried failed media processing", metadata)
}

// reestablishRetryEvidence re-enters the asset's own safety path from
// quarantine and reports the state it reached.
func (s *Service) reestablishRetryEvidence(ctx context.Context, tx pgx.Tx, request RetryRequest, target retryTarget) (AssetVersionState, error) {
	if !target.trusted {
		if err := s.appendScanWork(ctx, tx, request.AssetVersionID, string(target.kind), "admin-retry"); err != nil {
			return "", err
		}
		return StateQuarantined, nil
	}
	if !s.trustedPathApplies(target.kind, target.contentType) {
		return "", fmt.Errorf(
			"%w: this Asset Version holds D-088 trusted-validation provenance, which this deployment no longer accepts",
			ErrConflict)
	}
	completion := uploadCompletion{
		request: CompleteUploadRequest{
			OwnerAccountID:       target.ownerID,
			AssetVersionID:       request.AssetVersionID,
			ProviderEventID:      "admin-retry:" + request.AssetVersionID,
			StorageObjectKey:     target.objectKey,
			StorageObjectVersion: target.objectVersion,
			ContentType:          target.contentType,
			SizeBytes:            target.sizeBytes,
			SHA256Hex:            target.sha256Hex,
		},
		kind:    target.kind,
		maxSize: target.maxSize,
	}
	// A retry never inherits the earlier pass: the exact stored object version
	// is re-proved from scratch before any new evidence is written.
	if err := s.verifyCompletedObject(ctx, completion.request, target.kind, target.maxSize); err != nil {
		return "", err
	}
	return s.applyTrustedValidation(ctx, tx, completion, "admin-retry:"+request.AssetVersionID)
}

func retryPathLabel(trusted bool) string {
	if trusted {
		return "D-088_TRUSTED_REVALIDATION"
	}
	return "MALWARE_SCAN"
}

func (s *Service) requireCourseOwner(ctx context.Context, tx pgx.Tx, courseID, ownerID string) error {
	var owner string
	var role, status string
	err := tx.QueryRow(ctx, `
		SELECT c.owner_account_id::text, a.role, a.status
		FROM courses c JOIN accounts a ON a.id = c.owner_account_id
		WHERE c.id = $1::uuid FOR SHARE OF c, a
	`, courseID).Scan(&owner, &role, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("checking media course ownership: %w", err)
	}
	if owner != ownerID || role != "INSTRUCTOR" || status != "ACTIVE" {
		return ErrNotAuthorized
	}
	return nil
}

func (s *Service) appendScanWork(ctx context.Context, tx pgx.Tx, assetVersionID, kind, correlation string) error {
	eventID := uuid.NewString()
	_, err := s.outbox.Append(ctx, tx, outbox.Event{
		ID: eventID, Type: "media.scan_requested", SchemaVersion: 1,
		SourceModule: mediaSourceModule, AggregateType: "MEDIA_ASSET_VERSION",
		AggregateID: assetVersionID, AggregateRevision: 1, CorrelationID: correlation,
		SafePayload: map[string]any{"asset_version_id": assetVersionID, "kind": kind, "scan_work_id": eventID},
	}, ScanWork{AssetVersionID: assetVersionID, ScanWorkID: eventID})
	if err != nil {
		return fmt.Errorf("writing media scan outbox intent: %w", err)
	}
	return nil
}

func appendMediaAudit(ctx context.Context, tx pgx.Tx, actorID, actorRole, action, targetID, reason string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encoding media audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			actor_account_id, actor_role, actor_descriptor, action, module,
			target_type, target_id, reason, metadata
		) VALUES ($1::uuid, $2, $3, $4, 'MEDIA_AND_ASSETS', 'MEDIA_ASSET_VERSION', $5, $6, $7::jsonb)
	`, actorID, actorRole, actorID, action, targetID, reason, encoded); err != nil {
		return fmt.Errorf("writing media audit evidence: %w", err)
	}
	return nil
}

func validateUploadRequest(request UploadRequest, max int64) error {
	if !request.Kind.Valid() {
		return fmt.Errorf("%w: asset kind is invalid", ErrValidation)
	}
	if request.SizeBytes <= 0 || request.SizeBytes > max {
		return fmt.Errorf("%w: upload size is outside the configured limit", ErrValidation)
	}
	if !isAllowedContentType(request.Kind, request.ContentType) {
		return fmt.Errorf("%w: content type is not allowed", ErrValidation)
	}
	return nil
}

func validateCompletionRequest(request CompleteUploadRequest) error {
	if _, err := uuid.Parse(request.AssetVersionID); err != nil {
		return fmt.Errorf("%w: asset version ID is invalid", ErrValidation)
	}
	if strings.TrimSpace(request.ProviderEventID) == "" || strings.TrimSpace(request.StorageObjectKey) == "" || strings.TrimSpace(request.StorageObjectVersion) == "" {
		return fmt.Errorf("%w: completion identity is incomplete", ErrValidation)
	}
	if strings.HasPrefix(strings.ToLower(request.StorageObjectVersion), "pending:") {
		return fmt.Errorf("%w: completion requires a provider object version", ErrValidation)
	}
	if request.SizeBytes <= 0 || !sha256Pattern.MatchString(request.SHA256Hex) {
		return fmt.Errorf("%w: completion checksum and size are required", ErrValidation)
	}
	if !strings.HasPrefix(request.StorageObjectKey, "quarantine/") {
		return fmt.Errorf("%w: completion object is not in quarantine", ErrValidation)
	}
	return nil
}

func isAllowedContentType(kind AssetKind, contentType string) bool {
	_, ok := allowedContentTypes[kind][strings.ToLower(strings.TrimSpace(contentType))]
	return ok
}

func visibilityForKind(kind AssetKind) string {
	if kind == KindPreview {
		return "PUBLIC_PREVIEW"
	}
	return "PROTECTED"
}

func completionFingerprint(request CompleteUploadRequest) string {
	h := sha256.New()
	for _, value := range []string{request.AssetVersionID, request.StorageObjectKey, request.StorageObjectVersion, request.ContentType, fmt.Sprintf("%d", request.SizeBytes), strings.ToLower(request.SHA256Hex)} {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
