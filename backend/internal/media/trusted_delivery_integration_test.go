//go:build integration

package media

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// D-088 §7 keeps a trusted Lesson Resource protected content: it is delivered
// only through the existing authenticated, Entitlement-checked signed-download
// boundary. Skipping malware scanning must not skip entitlement.

// validatedAsset seeds a READY Asset Version whose only provenance is D-088
// trusted validation — no scan attempt exists for it anywhere.
func (f *deliveryFixture) validatedAsset(t *testing.T, kind AssetKind, contentType string) string {
	t.Helper()
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (id, kind, owner_account_id, course_id, lesson_id, visibility)
		VALUES ($1::uuid, $2::media_asset_kind, $3::uuid, $4::uuid, $5::uuid, $6::media_asset_visibility)
	`, assetID, kind, f.instructorID, f.courseID, f.lesson, visibilityForKind(kind)); err != nil {
		t.Fatalf("seeding a validated logical asset: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, $3::media_asset_kind, 'QUARANTINED', $4, 'v1', $5, 12, repeat('a', 64))
	`, versionID, assetID, kind, key, contentType); err != nil {
		t.Fatalf("seeding a validated asset version: %v", err)
	}
	// The upload intent carries the configured bound this upload was admitted
	// against. Validation provenance is bound to it, so evidence without an
	// intent describes an upload that was never admitted.
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at
		) VALUES ($1::uuid, $2, $3, 12, 52428800, now() + interval '15 minutes')
	`, versionID, key, contentType); err != nil {
		t.Fatalf("seeding the upload intent: %v", err)
	}
	var attemptID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, 1, $2, 'v1', 'PASSED', 'gradex-media-exact-version-validator',
			$3, $4, 12, 52428800, repeat('a', 64))
		RETURNING id::text
	`, versionID, "validation:"+versionID, TrustedValidationProfile, contentType).Scan(&attemptID); err != nil {
		t.Fatalf("seeding validation evidence: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("validating the asset version: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid`, versionID); err != nil {
		t.Fatalf("making the validated asset READY: %v", err)
	}
	return versionID
}

func TestD088ValidatedResourceIsDeliverableButStillEntitlementChecked(t *testing.T) {
	f := newDeliveryFixture(t)
	validated := f.validatedAsset(t, KindResource, "application/pdf")
	var lessonRow string
	if err := f.pool.QueryRow(f.ctx, `SELECT id::text FROM course_lessons WHERE lesson_identity_id = $1::uuid`, f.lesson).Scan(&lessonRow); err != nil {
		t.Fatalf("finding the lesson row: %v", err)
	}
	// Replace the scanner-provenance fixture resource with the validated one, so
	// the Lesson's current RESOURCE resolves to trusted-validation provenance.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE lesson_files SET asset_version_id = $1::uuid WHERE lesson_id = $2::uuid AND kind = 'RESOURCE'
	`, validated, lessonRow); err != nil {
		t.Fatalf("attaching the validated resource: %v", err)
	}

	t.Run("an entitled Student receives the download", func(t *testing.T) {
		issued, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			StudentID: f.student, LessonID: f.lesson, AssetVersionID: validated, Kind: KindResource,
		})
		if err != nil {
			t.Fatalf("issuing a validated resource download: %v", err)
		}
		if issued.AssetVersionID != validated || issued.URL == "" {
			t.Fatalf("download = %+v, want the validated version with a signed URL", issued)
		}
	})

	t.Run("an unentitled account is refused", func(t *testing.T) {
		stranger := uuid.NewString()
		if _, err := f.pool.Exec(f.ctx, `
			INSERT INTO accounts (id, normalized_email, email, role, status, display_name, locale, email_verified_at)
			VALUES ($1::uuid, $2, $2, 'STUDENT', 'ACTIVE', 'D-088 Stranger', 'en', now())
		`, stranger, "d088-stranger-"+stranger+"@example.test"); err != nil {
			t.Fatalf("seeding an unentitled Student: %v", err)
		}
		if _, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			StudentID: stranger, LessonID: f.lesson, AssetVersionID: validated, Kind: KindResource,
		}); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("unentitled download = %v, want ErrProtectedUnavailable", err)
		}
	})

	t.Run("an expired Entitlement is refused", func(t *testing.T) {
		if _, err := f.pool.Exec(f.ctx, `
			UPDATE entitlements SET access_ends_at = $1 WHERE student_account_id = $2::uuid
		`, f.now.Add(-time.Hour), f.student); err != nil {
			t.Fatalf("expiring the Entitlement: %v", err)
		}
		if _, err := f.delivery.IssueDownload(f.ctx, DownloadRequest{
			StudentID: f.student, LessonID: f.lesson, AssetVersionID: validated, Kind: KindResource,
		}); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("expired download = %v, want ErrProtectedUnavailable", err)
		}
	})
}

// D-096 admits the MP4 public preview to the trusted profile. What replaced the
// old scanner gate is a processing gate: a preview reaches the public only
// after real FFmpeg evidence over the exact stored object version, so these
// tests assert both halves — the trusted preview is deliverable, and the
// unprocessed one is not.

// validatedPreview seeds a PREVIEW Asset Version whose only provenance is D-096
// trusted validation. It stops at VALIDATED; the caller decides whether real
// processing evidence follows.
func (f *deliveryFixture) validatedPreview(t *testing.T, contentType string) string {
	t.Helper()
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (
			id, kind, owner_account_id, course_id, preview_origin_revision_id, visibility
		) VALUES ($1::uuid, 'PREVIEW', $2::uuid, $3::uuid, $4::uuid, 'PUBLIC_PREVIEW')
	`, assetID, f.instructorID, f.courseID, f.revision); err != nil {
		t.Fatalf("seeding a validated preview asset: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, 'PREVIEW', 'QUARANTINED', $3, 'v1', $4, 12, repeat('a', 64))
	`, versionID, assetID, key, contentType); err != nil {
		t.Fatalf("seeding a validated preview version: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at
		) VALUES ($1::uuid, $2, $3, 12, 52428800, now() + interval '15 minutes')
	`, versionID, key, contentType); err != nil {
		t.Fatalf("seeding the preview upload intent: %v", err)
	}
	var attemptID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, 1, $2, 'v1', 'PASSED', 'gradex-media-exact-version-validator',
			$3, $4, 12, 52428800, repeat('a', 64))
		RETURNING id::text
	`, versionID, "validation-preview:"+versionID, TrustedValidationProfile, contentType).Scan(&attemptID); err != nil {
		t.Fatalf("seeding preview validation evidence: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err != nil {
		t.Fatalf("validating the preview version: %v", err)
	}
	return versionID
}

// processPreview records the FFmpeg evidence a trusted preview owes and takes
// it to READY, the same shape the worker writes.
func (f *deliveryFixture) processPreview(t *testing.T, versionID string) {
	t.Helper()
	processingID := uuid.NewString()
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO processing_attempts (
			id, asset_version_id, operation_id, state, output_prefix, rendition_count, trusted_duration_ms
		) VALUES ($1::uuid, $2::uuid, $3, 'SUCCEEDED', 'preview/hls', 1, 45000)
	`, processingID, versionID, "process-preview:"+versionID); err != nil {
		t.Fatalf("seeding preview processing evidence: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid`, versionID); err != nil {
		t.Fatalf("claiming the preview for processing: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET successful_processing_attempt_id = $1::uuid, trusted_duration_ms = 45000, state = 'READY'
		WHERE id = $2::uuid
	`, processingID, versionID); err != nil {
		t.Fatalf("making the trusted preview READY: %v", err)
	}
}

func (f *deliveryFixture) publishPreview(t *testing.T, versionID string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE course_revisions SET preview_asset_version_id = $1::uuid WHERE id = $2::uuid
	`, versionID, f.revision); err != nil {
		t.Fatalf("publishing the preview: %v", err)
	}
}

func TestD096ProcessedTrustedPreviewIsPubliclyDeliverable(t *testing.T) {
	f := newDeliveryFixture(t)
	preview := f.validatedPreview(t, "video/mp4")
	f.processPreview(t, preview)
	f.publishPreview(t, preview)

	issued, err := f.delivery.IssuePreview(f.ctx, preview)
	if err != nil {
		t.Fatalf("issuing a processed trusted preview: %v", err)
	}
	if issued.URL == "" {
		t.Fatal("a processed trusted preview produced no signed URL")
	}
	byCourse, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID)
	if err != nil || byCourse.URL == "" {
		t.Fatalf("IssueCoursePreview = %+v, %v; want the live trusted preview", byCourse, err)
	}

	// Trusted validation is not scan evidence and must never be recorded as it.
	var scanProvenance *string
	var scanAttempts int
	if err := f.pool.QueryRow(f.ctx, `
		SELECT successful_scan_attempt_id::text FROM media_asset_versions WHERE id = $1::uuid
	`, preview).Scan(&scanProvenance); err != nil {
		t.Fatalf("reading preview scan provenance: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM scan_attempts WHERE asset_version_id = $1::uuid`, preview).Scan(&scanAttempts); err != nil {
		t.Fatalf("counting preview scan attempts: %v", err)
	}
	if scanProvenance != nil || scanAttempts != 0 {
		t.Fatal("the trusted preview path fabricated scan evidence")
	}
}

// The gate that replaced the scanner gate. A preview the Instructor's revision
// already selected — which now happens as soon as the upload completes — is not
// public until processing has actually succeeded.
func TestD096UnprocessedTrustedPreviewIsNotPublic(t *testing.T) {
	f := newDeliveryFixture(t)
	preview := f.validatedPreview(t, "video/mp4")
	f.publishPreview(t, preview)

	assertPreviewNotPublic := func(t *testing.T, stage string) {
		t.Helper()
		if _, err := f.delivery.IssuePreview(f.ctx, preview); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("IssuePreview on a %s preview = %v, want ErrProtectedUnavailable", stage, err)
		}
		if _, err := f.delivery.IssueCoursePreview(f.ctx, f.courseID); !errors.Is(err, ErrProtectedUnavailable) {
			t.Fatalf("a %s preview was reachable through the public Course", stage)
		}
	}

	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'READY' WHERE id = $1::uuid
	`, preview); err == nil {
		t.Fatal("a validated preview reached READY with no processing evidence")
	}
	assertPreviewNotPublic(t, "VALIDATED")

	// Mid-processing: selected on the revision, still not public.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'PROCESSING' WHERE id = $1::uuid
	`, preview); err != nil {
		t.Fatalf("claiming the preview for processing: %v", err)
	}
	assertPreviewNotPublic(t, "PROCESSING")

	// Terminal failure: still selected on the draft, permanently not public.
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions SET state = 'PROCESS_FAILED' WHERE id = $1::uuid
	`, preview); err != nil {
		t.Fatalf("failing the preview processing: %v", err)
	}
	assertPreviewNotPublic(t, "PROCESS_FAILED")

	var stillSelected string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT preview_asset_version_id::text FROM course_revisions WHERE id = $1::uuid
	`, f.revision).Scan(&stillSelected); err != nil {
		t.Fatalf("reading the selected preview: %v", err)
	}
	if stillSelected != preview {
		t.Fatalf("selected preview = %s, want the failed %s to stay visible to its author", stillSelected, preview)
	}
}

// The profile stays narrow at the database boundary: a non-MP4 preview cannot
// hold validation provenance at all, so it remains scanner-gated.
func TestD096NonMP4PreviewCannotHoldValidationProvenance(t *testing.T) {
	f := newDeliveryFixture(t)
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	assetID, versionID := uuid.NewString(), uuid.NewString()
	key := "quarantine/" + f.courseID + "/" + versionID + "/source"
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_assets (
			id, kind, owner_account_id, course_id, preview_origin_revision_id, visibility
		) VALUES ($1::uuid, 'PREVIEW', $2::uuid, $3::uuid, $4::uuid, 'PUBLIC_PREVIEW')
	`, assetID, f.instructorID, f.courseID, f.revision); err != nil {
		t.Fatalf("seeding a non-MP4 preview asset: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO media_asset_versions (
			id, logical_asset_id, kind, state, storage_object_key, storage_object_version,
			content_type, size_bytes, sha256_hex
		) VALUES ($1::uuid, $2::uuid, 'PREVIEW', 'QUARANTINED', $3, 'v1', 'application/pdf', 12, repeat('a', 64))
	`, versionID, assetID, key); err != nil {
		t.Fatalf("seeding a non-MP4 preview version: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		INSERT INTO upload_intents (
			asset_version_id, expected_object_key, expected_content_type,
			expected_size_bytes, max_size_bytes, expires_at
		) VALUES ($1::uuid, $2, 'application/pdf', 12, 52428800, now() + interval '15 minutes')
	`, versionID, key); err != nil {
		t.Fatalf("seeding the non-MP4 preview intent: %v", err)
	}
	var attemptID string
	if err := f.pool.QueryRow(f.ctx, `
		INSERT INTO validation_attempts (
			asset_version_id, attempt_number, work_id, storage_object_version, outcome,
			validator_identity, profile, declared_content_type, verified_size_bytes,
			max_size_bytes, sha256_hex
		) VALUES ($1::uuid, 1, $2, 'v1', 'PASSED', 'gradex-media-exact-version-validator',
			$3, 'application/pdf', 12, 52428800, repeat('a', 64))
		RETURNING id::text
	`, versionID, "validation-preview:"+versionID, TrustedValidationProfile).Scan(&attemptID); err != nil {
		t.Fatalf("seeding non-MP4 preview validation evidence: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE media_asset_versions
		SET state = 'VALIDATED', successful_validation_attempt_id = $1::uuid
		WHERE id = $2::uuid
	`, attemptID, versionID); err == nil {
		t.Fatal("a non-MP4 public preview accepted trusted-validation provenance")
	}
}

// The scanner path is unchanged: the fixture preview reached READY through a
// real scan with no processing evidence, and it stays public and immutable.
func TestD096ScannerPreviewDeliveryIsUnchanged(t *testing.T) {
	f := newDeliveryFixture(t)
	if _, err := f.delivery.IssuePreview(f.ctx, f.preview); err != nil {
		t.Fatalf("a scanned preview was unavailable: %v", err)
	}
	var scanned string
	if err := f.pool.QueryRow(f.ctx, `
		SELECT successful_scan_attempt_id::text FROM media_asset_versions WHERE id = $1::uuid
	`, f.preview).Scan(&scanned); err != nil {
		t.Fatalf("reading preview scan provenance: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `
		UPDATE scan_attempts SET outcome = 'FAILED' WHERE id = $1::uuid
	`, scanned); err == nil {
		t.Fatal("scan evidence was mutable")
	}
}
