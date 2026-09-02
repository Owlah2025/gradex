package media

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The D-088 trusted-Instructor launch profile.
//
// D-088, as amended by D-096, defers production malware scanning for one
// deliberately narrow set of media authored by explicitly invited, vetted
// Instructors: MP4 Lesson video, PDF or DOCX Lesson Resources, and the MP4
// public Course preview. Those uploads still enter private quarantine and must
// pass exact-version validation — configured size bound, actual stored object
// size, declared type against the real file format, and SHA-256 over the exact
// stored version — before they may progress.
//
// Nothing here claims, records, or implies that malware scanning happened.
// Trusted validation is a separate honest evidence path with its own state,
// its own attempt table, and its own provenance column. Lab Materials, every
// non-MP4 preview, and every other kind or content type remain scanner-gated
// in every operating mode, and this profile is the only place that boundary is
// decided.
//
// The public preview is the one admitted kind whose bytes become publicly
// readable, which is why D-096 pairs its admission with the Lesson-video
// processing requirement below: an unscanned preview is never deliverable on
// validation evidence alone.

// trustedProfileTypes is the complete D-088/D-096 allowlist. Adding a kind or
// type here broadens the accepted-risk boundary and needs new authority; D-088
// §9 names the reconsideration triggers.
var trustedProfileTypes = map[AssetKind]map[string]struct{}{
	KindVideo:    {"video/mp4": {}},
	KindResource: {"application/pdf": {}, ContentTypeDOCX: {}},
	// D-096. MP4 only: a preview is public, and the FFmpeg pass that
	// trustedRequiresProcessing demands for it is a video pass.
	KindPreview: {"video/mp4": {}},
}

// TrustedProfileAdmits reports whether this exact kind and declared content
// type may use the D-088 no-malware-scan validation path. It fails closed: an
// unknown kind, an unlisted type, a non-MP4 public preview, and a Lab Material
// all return false.
func TrustedProfileAdmits(kind AssetKind, contentType string) bool {
	if !kind.Valid() {
		return false
	}
	_, ok := trustedProfileTypes[kind][strings.ToLower(strings.TrimSpace(contentType))]
	return ok
}

// trustedRequiresProcessing reports whether a validated asset of this kind
// still owes trusted FFmpeg evidence before READY. D-088 §6 keeps video
// processing integrity intact, and D-096 puts the MP4 public preview under the
// same requirement; only a validated PDF or DOCX Lesson Resource may become
// READY on validation evidence alone.
//
// This is the Go half of the rule. The lifecycle trigger installed by
// 0031_trusted_public_preview enforces the same thing in the database, so a
// regression here cannot make an unprocessed trusted preview deliverable.
func trustedRequiresProcessing(kind AssetKind) bool {
	return kind == KindVideo || kind == KindPreview
}

// TrustedValidationProfile labels the validation evidence Gradex records, so a
// reader of the attempt row can tell which authority admitted the object.
const TrustedValidationProfile = "D-088-TRUSTED-INSTRUCTOR"

// trustedValidatorIdentity names what performed the validation. It is
// deliberately not a scanner name: no malware inspection took place, and
// nothing downstream may read this as a scanner identity.
const trustedValidatorIdentity = "gradex-media-exact-version-validator"

// applyTrustedValidation records the D-088 evidence for one exact object
// version and moves the Asset Version onto the trusted path.
//
// The caller has already proved the bytes: configured size bound, actual stored
// object size, declared type against the real file format, and SHA-256 over the
// exact stored version. This writes that proof down as validation — never as a
// scan — and then progresses:
//
//	video    QUARANTINED -> VALIDATED, one committed transcode intent
//	preview  QUARANTINED -> VALIDATED, one committed transcode intent
//	resource QUARANTINED -> VALIDATED -> READY
//
// No scan attempt, scanner identity, or SCAN_PASSED state is created anywhere
// on this path.
func (s *Service) applyTrustedValidation(ctx context.Context, tx pgx.Tx, completion uploadCompletion, correlation string) (AssetVersionState, error) {
	request := completion.request
	attemptID, err := recordTrustedValidationAttempt(ctx, tx, completion)
	if err != nil {
		return "", err
	}
	commandTag, err := tx.Exec(ctx, `
		UPDATE media_asset_versions
		SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid AND state = 'QUARANTINED'
	`, attemptID, request.AssetVersionID)
	if err != nil {
		return "", fmt.Errorf("applying trusted validation evidence: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return "", ErrConcurrentModification
	}

	audit := map[string]any{
		"state":          string(StateValidated),
		"profile":        TrustedValidationProfile,
		"validator":      trustedValidatorIdentity,
		"object_version": request.StorageObjectVersion,
		"content_type":   request.ContentType,
		"malware_scan":   "NOT_PERFORMED",
	}
	if err := appendMediaAudit(ctx, tx, request.OwnerAccountID, "INSTRUCTOR",
		"MEDIA_UPLOAD_VALIDATED", request.AssetVersionID,
		"Exact-version validation passed under the D-088 trusted-Instructor profile; no malware scan was performed",
		audit); err != nil {
		return "", err
	}

	if trustedRequiresProcessing(completion.kind) {
		if err := appendTranscodeWork(ctx, tx, s.outbox, request.AssetVersionID, correlation); err != nil {
			return "", err
		}
		return StateValidated, nil
	}

	// The kind predicate repeats trustedRequiresProcessing as a statement-level
	// guard: a kind that owes FFmpeg evidence must never reach READY here, even
	// if this branch were entered by mistake.
	commandTag, err = tx.Exec(ctx, `
		UPDATE media_asset_versions SET state = 'READY'
		WHERE id = $1::uuid AND state = 'VALIDATED' AND kind NOT IN ('VIDEO', 'PREVIEW')
	`, request.AssetVersionID)
	if err != nil {
		return "", fmt.Errorf("making a validated Lesson Resource ready: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return "", ErrConcurrentModification
	}
	return StateReady, nil
}

// recordTrustedValidationAttempt appends immutable evidence for this exact
// object version. Attempt numbers are allocated per object version, so a retry
// adds a new attempt rather than rewriting the one that already exists.
func recordTrustedValidationAttempt(ctx context.Context, tx pgx.Tx, completion uploadCompletion) (string, error) {
	request := completion.request
	var attemptNumber int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(attempt_number), 0) + 1
		FROM validation_attempts
		WHERE asset_version_id = $1::uuid AND storage_object_version = $2
	`, request.AssetVersionID, request.StorageObjectVersion).Scan(&attemptNumber); err != nil {
		return "", fmt.Errorf("allocating a validation attempt: %w", err)
	}
	var attemptID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, $2, $3, $4, 'PASSED', $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, request.AssetVersionID, attemptNumber, "validation:"+uuid.NewString(),
		request.StorageObjectVersion, trustedValidatorIdentity, TrustedValidationProfile,
		strings.ToLower(strings.TrimSpace(request.ContentType)), request.SizeBytes,
		completion.maxSize, strings.ToLower(request.SHA256Hex)).Scan(&attemptID); err != nil {
		return "", fmt.Errorf("recording trusted validation evidence: %w", err)
	}
	return attemptID, nil
}
